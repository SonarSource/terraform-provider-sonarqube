package cloud_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
)

// The test makes and deletes a real organization, so it needs a live instance
// and a token. resource.Test skips it when TF_ACC is unset.
func TestAccOrganizationResource(t *testing.T) {
	key := fmt.Sprintf("%s%d", acctest.KeyPrefix, rand.Uint32())
	renamedKey := key + "-renamed"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheckDestructive(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccOrganizationConfig(key, "First name", "First description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "key", key),
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "id", key),
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "name", "First name"),
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "description", "First description"),
				),
			},
			{
				// A new name and description change the organization in place.
				Config: testAccOrganizationConfig(key, "Second name", "Second description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "name", "Second name"),
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "description", "Second description"),
				),
			},
			{
				// A new key renames the organization. The id follows it.
				Config: testAccOrganizationConfig(renamedKey, "Second name", "Second description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "key", renamedKey),
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "id", renamedKey),
				),
			},
			{
				ResourceName:      "sonarqube_cloud_organization.test",
				ImportState:       true,
				ImportStateId:     renamedKey,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccOrganizationConfig(key, name, description string) string {
	return fmt.Sprintf(`
resource "sonarqube_cloud_organization" "test" {
  key         = %[1]q
  name        = %[2]q
  description = %[3]q
}
`, key, name, description)
}
