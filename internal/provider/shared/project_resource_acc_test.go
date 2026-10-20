package shared_test

import (
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

func TestAccProjectResource(t *testing.T) {
	key := fmt.Sprintf("%s%d", acctest.KeyPrefix, rand.Uint32())
	organization := os.Getenv(acctest.EnvTestOrganization)
	config := fmt.Sprintf(`
resource "sonarqube_project" "test" {
  organization = %q
  key          = %q
  name         = "Terraform acceptance test"
}
`, organization, key)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheckSharedOrganization(t)
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckProjectDeleted(t, organization, key),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sonarqube_project.test", "id", key),
					resource.TestCheckResourceAttr("sonarqube_project.test", "organization", organization),
					resource.TestCheckResourceAttr("sonarqube_project.test", "name", "Terraform acceptance test"),
				),
			},
			{
				Config: config,
			},
			{
				ResourceName:      "sonarqube_project.test",
				ImportState:       true,
				ImportStateId:     organization + "/" + key,
				ImportStateVerify: true,
			},
			acctest.ImportError("sonarqube_project.test", key, regexp.MustCompile(`Cannot import the project`)),
			acctest.ImportError("sonarqube_project.test", organization+"/"+acctest.MissingKey, acctest.MissingImport),
			acctest.DeletedOutside(t, "sonarqube_project.test", config,
				resource.TestCheckResourceAttr("sonarqube_project.test", "id", key),
				func() error { return acctest.Client().DeleteProject(t.Context(), key) },
				func() (*client.Project, error) { return acctest.Client().GetProject(t.Context(), organization, key) }),
		},
	})
}
