package cloud_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// The test makes and deletes a real organization, so it needs a live instance
// and a token. resource.Test skips it when TF_ACC is unset.
func TestAccOrganizationResource(t *testing.T) {
	key := fmt.Sprintf("%s%d", acctest.KeyPrefix, rand.Uint32())
	renamedKey := key + "-renamed"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheckDestructive(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckOrganizationsDeleted(t, key, renamedKey),
		Steps: []resource.TestStep{
			{
				Config: testAccOrganizationConfig(key, "First name", "First description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "key", key),
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "id", key),
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "name", "First name"),
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "description", "First description"),
					testAccCheckOrganization(t, key, "First name", "First description"),
				),
			},
			{
				// A new name and description change the organization in place.
				Config: testAccOrganizationConfig(key, "Second name", "Second description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "name", "Second name"),
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "description", "Second description"),
					testAccCheckOrganization(t, key, "Second name", "Second description"),
				),
			},
			{
				// A new key renames the organization. The id follows it.
				Config: testAccOrganizationConfig(renamedKey, "Second name", "Second description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "key", renamedKey),
					resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "id", renamedKey),
					testAccCheckOrganization(t, renamedKey, "Second name", "Second description"),
				),
			},
			{
				ResourceName:      "sonarqube_cloud_organization.test",
				ImportState:       true,
				ImportStateId:     renamedKey,
				ImportStateVerify: true,
			},
			acctest.ImportError("sonarqube_cloud_organization.test", acctest.MissingKey, acctest.MissingImport),
			acctest.DeletedOutside(t, "sonarqube_cloud_organization.test",
				testAccOrganizationConfig(renamedKey, "Second name", "Second description"),
				resource.TestCheckResourceAttr("sonarqube_cloud_organization.test", "key", renamedKey),
				func() error { return acctest.Client().DeleteOrganization(t.Context(), renamedKey) },
				func() (*client.Organization, error) { return acctest.Client().GetOrganization(t.Context(), renamedKey) }),
		},
	})
}

// testAccCheckOrganization compares the organization in the API with the
// name and the description that the configuration gives.
func testAccCheckOrganization(t *testing.T, key, name, description string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		org, err := acctest.Client().GetOrganization(t.Context(), key)
		if err != nil {
			return err
		}
		if org.Name != name || org.Description != description {
			return fmt.Errorf("organization %s has name %q and description %q, want %q and %q",
				key, org.Name, org.Description, name, description)
		}
		return nil
	}
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
