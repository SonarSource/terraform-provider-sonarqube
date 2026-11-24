package cloud_test

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
)

const projectAutomaticAnalysisAddress = "sonarqube_cloud_project_automatic_analysis.test"

// The project has no binding, so the server refuses Automatic Analysis on.
func TestAccProjectAutomaticAnalysisResource(t *testing.T) {
	acctest.SkipWithSOT(t, acctest.SOTRefused)
	organization := os.Getenv(acctest.EnvTestOrganization)
	projectKey := fmt.Sprintf("%s%d", acctest.KeyPrefix, rand.Uint32())

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheckSharedOrganization(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckProjectDeleted(t, organization, projectKey),
		Steps: []resource.TestStep{
			{
				Config: testAccProjectAutomaticAnalysisConfig(organization, projectKey, "false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectAutomaticAnalysisAddress, "id", organization+"/"+projectKey),
					resource.TestCheckResourceAttr(projectAutomaticAnalysisAddress, "enabled", "false"),
					testAccCheckAutomaticAnalysis(t, projectKey, false),
				),
			},
			{
				ResourceName:      projectAutomaticAnalysisAddress,
				ImportState:       true,
				ImportStateId:     organization + "/" + projectKey,
				ImportStateVerify: true,
			},
			acctest.ImportError(projectAutomaticAnalysisAddress, projectKey,
				regexp.MustCompile(`Cannot import the Automatic Analysis setting`)),
			acctest.ImportError(projectAutomaticAnalysisAddress, organization+"/"+acctest.MissingKey, acctest.MissingImport),
			{
				Config:      testAccProjectAutomaticAnalysisConfig(organization, projectKey, "true"),
				ExpectError: regexp.MustCompile(`the only project bound to its repository`),
			},
			{
				Config:           testAccProjectAutomaticAnalysisConfig(organization, projectKey, ""),
				ConfigPlanChecks: acctest.PlanAction(projectAutomaticAnalysisAddress, plancheck.ResourceActionDestroy),
				Check:            testAccCheckAutomaticAnalysis(t, projectKey, false),
			},
		},
	})
}

// The project is bound to a repository, so Automatic Analysis can be on.
func TestAccProjectAutomaticAnalysisResourceBound(t *testing.T) {
	acctest.SkipWithSOT(t, acctest.SOTRefused)
	organization := fmt.Sprintf("%s%d", acctest.KeyPrefix, rand.Uint32())
	projectKey := organization + "_my-project"
	installationID := os.Getenv(acctest.EnvTestInstallationID)
	repository := os.Getenv(acctest.EnvTestRepository)
	bound := func(enabled string) string {
		return testAccProjectBindingConfig(organization, "my-project", installationID, repository) + fmt.Sprintf(`
resource "sonarqube_cloud_project_automatic_analysis" "test" {
  organization = sonarqube_cloud_project_binding.test.organization
  project_key  = sonarqube_cloud_project_binding.test.project_key
  enabled      = %s
}
`, enabled)
	}

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheckBinding(t)
			if repository == "" {
				t.Skipf("%s is not set. A test that binds a project needs a public repository "+
					"that the installation can see.", acctest.EnvTestRepository)
			}
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckOrganizationsDeleted(t, organization),
		Steps: []resource.TestStep{
			{
				Config: bound("true"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectAutomaticAnalysisAddress, "enabled", "true"),
					testAccCheckAutomaticAnalysis(t, projectKey, true),
				),
			},
			{
				Config: bound("false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectAutomaticAnalysisAddress, "enabled", "false"),
					testAccCheckAutomaticAnalysis(t, projectKey, false),
				),
			},
		},
	})
}

// testAccCheckAutomaticAnalysis reads the setting of the project from the API.
func testAccCheckAutomaticAnalysis(t *testing.T, projectKey string, want bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got, err := acctest.Client().GetProjectAutomaticAnalysis(t.Context(), projectKey)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("Automatic Analysis of the project %s is %t, want %t", projectKey, got, want)
		}
		return nil
	}
}

// testAccProjectAutomaticAnalysisConfig makes the project, and the setting
// when enabled is not empty.
func testAccProjectAutomaticAnalysisConfig(organization, projectKey, enabled string) string {
	config := fmt.Sprintf(`
resource "sonarqube_project" "test" {
  organization = %[1]q
  key          = %[2]q
  name         = "Terraform acceptance test"
}
`, organization, projectKey)
	if enabled == "" {
		return config
	}
	return config + fmt.Sprintf(`
resource "sonarqube_cloud_project_automatic_analysis" "test" {
  organization = %[1]q
  project_key  = sonarqube_project.test.key
  enabled      = %[2]s
}
`, organization, enabled)
}
