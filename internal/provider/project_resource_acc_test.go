package provider

import (
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProjectResource(t *testing.T) {
	key := fmt.Sprintf("%s%d", acceptanceKeyPrefix, rand.Uint32())
	organization := os.Getenv(envTestOrganization)
	config := fmt.Sprintf(`
resource "sonarqube_project" "test" {
  organization = %q
  key          = %q
  name         = "Terraform acceptance test"
}
`, organization, key)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheckDestructive(t)
			if organization == "" {
				t.Skipf("%s is not set", envTestOrganization)
			}
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("sonarqube_project.test", "id", key),
				resource.TestCheckResourceAttr("sonarqube_project.test", "organization", organization),
				resource.TestCheckResourceAttr("sonarqube_project.test", "name", "Terraform acceptance test"),
			),
		}, {
			Config: config,
		}, {
			ResourceName:      "sonarqube_project.test",
			ImportState:       true,
			ImportStateId:     organization + "/" + key,
			ImportStateVerify: true,
		}},
	})
}
