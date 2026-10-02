package provider

import (
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// envTestRepository names a public GitHub repository that the installation of
// envTestInstallationID can see. The repository must be public, because the
// project that the test makes is public, and the server refuses to bind a
// public project to a private repository.
const envTestRepository = "SONARQUBE_TEST_GITHUB_REPOSITORY"

// The addresses in the configuration below.
const (
	projectBindingAddress     = "sonarqube_cloud_project_binding.test"
	projectBindingDataAddress = "data.sonarqube_cloud_project_binding.test"
)

// The test makes an organization, binds it, makes a project and binds the
// project to the repository. The destroy deletes the organization, which is
// the only thing that removes the two bindings, so the test can run again.
func TestAccCloudProjectBindingResource(t *testing.T) {
	organization := fmt.Sprintf("%s%d", acceptanceKeyPrefix, rand.Uint32())
	installationID := os.Getenv(envTestInstallationID)
	repository := os.Getenv(envTestRepository)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheckBinding(t)
			if repository == "" {
				t.Skipf("%s is not set. A test that binds a project needs a public repository "+
					"that the installation can see.", envTestRepository)
			}
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// resource.Test plans again after this apply and fails when
				// that plan is not empty.
				Config: testAccProjectBindingConfig(organization, "my-project", installationID, repository),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectBindingAddress, "repository", repository),
					resource.TestCheckResourceAttrSet(projectBindingAddress, "id"),
					resource.TestCheckResourceAttrSet(projectBindingAddress, "repository_id"),
					resource.TestCheckResourceAttrPair(
						projectBindingDataAddress, "id", projectBindingAddress, "id"),
					resource.TestCheckResourceAttrPair(
						projectBindingDataAddress, "repository_id", projectBindingAddress, "repository_id"),
				),
			},
			{
				// The slug in SONARQUBE_TEST_GITHUB_REPOSITORY must have the
				// case that GitHub gives it, or this step reports the
				// difference.
				ResourceName:      projectBindingAddress,
				ImportState:       true,
				ImportStateId:     organization + "/" + organization + "_my-project",
				ImportStateVerify: true,
			},
			{
				// A new key replaces the project, which removes its binding,
				// and replaces the binding, which binds the new project. The
				// repository is free again, so the server accepts the bind.
				Config: testAccProjectBindingConfig(organization, "other-project", installationID, repository),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectBindingAddress, "project_key", organization+"_other-project"),
					resource.TestCheckResourceAttr(projectBindingAddress, "repository", repository),
					resource.TestCheckResourceAttrPair(
						projectBindingDataAddress, "id", projectBindingAddress, "id"),
				),
			},
		},
	})
}

func testAccProjectBindingConfig(organization, projectKey, installationID, repository string) string {
	return fmt.Sprintf(`
resource "sonarqube_cloud_organization" "test" {
  key  = %[1]q
  name = "Terraform acceptance test"
}

resource "sonarqube_cloud_organization_binding" "test" {
  organization_key = sonarqube_cloud_organization.test.key
  installation_id  = %[3]q
}

resource "sonarqube_project" "test" {
  organization = sonarqube_cloud_organization.test.key
  key          = "%[1]s_%[2]s"
  name         = "Terraform acceptance test"
}

resource "sonarqube_cloud_project_binding" "test" {
  # The project can bind only after its organization is bound.
  organization = sonarqube_cloud_organization_binding.test.organization_key
  project_key  = sonarqube_project.test.key
  repository   = %[4]q
}

data "sonarqube_cloud_project_binding" "test" {
  organization = sonarqube_cloud_project_binding.test.organization
  project_key  = sonarqube_cloud_project_binding.test.project_key
}
`, organization, projectKey, installationID, repository)
}
