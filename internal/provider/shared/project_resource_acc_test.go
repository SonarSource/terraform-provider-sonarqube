package shared_test

import (
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

const projectAddress = "sonarqube_project.test"

func TestAccProjectResource(t *testing.T) {
	key := fmt.Sprintf("%s%d", acctest.KeyPrefix, rand.Uint32())
	newKey := key + "-new"
	organization := os.Getenv(acctest.EnvTestOrganization)
	config := func(key, name string) string {
		return fmt.Sprintf(`
resource "sonarqube_project" "test" {
  organization = %q
  key          = %q
  name         = %q
}
`, organization, key, name)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheckSharedOrganization(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			acctest.CheckProjectDeleted(t, organization, key),
			acctest.CheckProjectDeleted(t, organization, newKey),
		),
		Steps: []resource.TestStep{
			{
				Config: config(key, "First name"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectAddress, "id", key),
					resource.TestCheckResourceAttr(projectAddress, "organization", organization),
					resource.TestCheckResourceAttr(projectAddress, "name", "First name"),
					testAccCheckProjectName(t, organization, key, "First name"),
				),
			},
			{
				// SonarQube Cloud cannot rename a project, so a new name
				// replaces it.
				Config:           config(key, "Second name"),
				ConfigPlanChecks: acctest.PlanAction(projectAddress, plancheck.ResourceActionReplace),
				Check:            testAccCheckProjectName(t, organization, key, "Second name"),
			},
			{
				// A new key replaces the project.
				Config:           config(newKey, "Second name"),
				ConfigPlanChecks: acctest.PlanAction(projectAddress, plancheck.ResourceActionReplace),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectAddress, "id", newKey),
					testAccCheckProjectName(t, organization, newKey, "Second name"),
					acctest.CheckProjectDeleted(t, organization, key),
				),
			},
			{
				ResourceName:      projectAddress,
				ImportState:       true,
				ImportStateId:     organization + "/" + newKey,
				ImportStateVerify: true,
			},
			acctest.ImportError(projectAddress, newKey, regexp.MustCompile(`Cannot import the project`)),
			acctest.ImportError(projectAddress, organization+"/"+acctest.MissingKey, acctest.MissingImport),
			acctest.DeletedOutside(t, projectAddress, config(newKey, "Second name"),
				testAccCheckProjectName(t, organization, newKey, "Second name"),
				func() error { return acctest.Client().DeleteProject(t.Context(), newKey) },
				func() (*client.Project, error) { return acctest.Client().GetProject(t.Context(), organization, newKey) }),
		},
	})
}

// testAccCheckProjectName compares the name of the project in the API with
// want.
func testAccCheckProjectName(t *testing.T, organization, key, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		project, err := acctest.Client().GetProject(t.Context(), organization, key)
		if err != nil {
			return err
		}
		if project.Name != want {
			return fmt.Errorf("name of project %s = %q, want %q", key, project.Name, want)
		}
		return nil
	}
}
