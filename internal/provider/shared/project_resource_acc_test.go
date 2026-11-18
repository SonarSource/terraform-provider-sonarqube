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
	acctest.SkipWithSOT(t, acctest.SOTPrivateProjects)
	key := fmt.Sprintf("%s%d", acctest.KeyPrefix, rand.Uint32())
	newKey := key + "-new"
	organization := os.Getenv(acctest.EnvTestOrganization)
	// An empty mainBranch leaves main_branch out of the configuration.
	configWithBranch := func(key, name, mainBranch string) string {
		branch := ""
		if mainBranch != "" {
			branch = fmt.Sprintf("\n  main_branch  = %q", mainBranch)
		}
		return fmt.Sprintf(`
resource "sonarqube_project" "test" {
  organization = %q
  key          = %q
  name         = %q%s
}
`, organization, key, name, branch)
	}
	config := func(key, name string) string { return configWithBranch(key, name, "") }

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
					resource.TestCheckResourceAttrSet(projectAddress, "main_branch"),
					testAccCheckProjectName(t, organization, key, "First name"),
					testAccCheckStateMainBranchMatches(t, key),
				),
			},
			{
				// A new main branch renames the branch and keeps the project.
				Config:           configWithBranch(key, "First name", "main"),
				ConfigPlanChecks: acctest.PlanAction(projectAddress, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectAddress, "main_branch", "main"),
					testAccCheckMainBranch(t, key, "main"),
				),
			},
			{
				PreConfig:        func() { renameMainBranchOutside(t, key, "outside") },
				Config:           configWithBranch(key, "First name", "main"),
				ConfigPlanChecks: acctest.PlanAction(projectAddress, plancheck.ResourceActionUpdate),
				Check:            testAccCheckMainBranch(t, key, "main"),
			},
			{
				// A configuration without the attribute keeps the main branch.
				Config:           config(key, "First name"),
				ConfigPlanChecks: acctest.PlanAction(projectAddress, plancheck.ResourceActionNoop),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectAddress, "main_branch", "main"),
					testAccCheckMainBranch(t, key, "main"),
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

func renameMainBranchOutside(t *testing.T, key, name string) {
	t.Helper()
	if err := acctest.Client().RenameMainBranch(t.Context(), key, name); err != nil {
		t.Fatal(err)
	}
}

// testAccCheckMainBranch compares the main branch of the project in the API
// with want.
func testAccCheckMainBranch(t *testing.T, key, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got, err := acctest.Client().GetMainBranch(t.Context(), key)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("main branch of project %s = %q, want %q", key, got, want)
		}
		return nil
	}
}

// testAccCheckStateMainBranchMatches compares main_branch in the state with
// the main branch of the project in the API.
func testAccCheckStateMainBranchMatches(t *testing.T, key string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		got, err := acctest.Client().GetMainBranch(t.Context(), key)
		if err != nil {
			return err
		}
		return resource.TestCheckResourceAttr(projectAddress, "main_branch", got)(state)
	}
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
