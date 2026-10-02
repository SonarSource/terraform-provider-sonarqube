package cloud_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// The data source only reads, so a token is all it needs. It does not write,
// so it may run against any instance.
func TestAccDopApplicationsDataSource(t *testing.T) {
	const address = "data.sonarqube_cloud_dop_applications.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheckToken(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
data "sonarqube_cloud_dop_applications" "test" {
  dev_ops_platform = "` + client.PlatformGitHub + `"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "dev_ops_platform", client.PlatformGitHub),
					// An instance owns at least one application of the
					// platform that it binds organizations to.
					resource.TestCheckResourceAttrSet(address, "applications.#"),
					resource.TestCheckResourceAttrSet(address, "applications.0.application_key"),
				),
			},
		},
	})
}
