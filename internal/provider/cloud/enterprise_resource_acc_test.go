package cloud_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

const enterpriseAddress = "sonarqube_cloud_enterprise.test"

// The test uses up the license of SONARQUBE_TEST_ENTERPRISE_LICENSE, so it
// skips when the variable is not set.
func TestAccEnterpriseResource(t *testing.T) {
	license := acctest.PreCheckEnterprise(t)
	suffix := rand.Uint32()
	organization := fmt.Sprintf("%s%d", acctest.KeyPrefix, suffix)
	key := fmt.Sprintf("%sent-%d", acctest.KeyPrefix, suffix)
	var id string

	config := func(name, avatar, enterpriseLicense string) string {
		return fmt.Sprintf(`
resource "sonarqube_cloud_organization" "test" {
  key  = %[1]q
  name = %[1]q
}

resource "sonarqube_cloud_enterprise" "test" {
  key          = %[2]q
  name         = %[3]q
  avatar       = %[4]q
  license      = %[5]q
  organization = sonarqube_cloud_organization.test.key
}
`, organization, key, name, avatar, enterpriseLicense)
	}
	withDataSource := func(name, avatar string) string {
		return config(name, avatar, license) + `
data "sonarqube_cloud_enterprise" "test" {
  key = sonarqube_cloud_enterprise.test.key
}
`
	}
	enterpriseGone := acctest.CheckGone("enterprise "+key, func() (*client.Enterprise, error) {
		return acctest.Client().GetEnterprise(t.Context(), id)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             resource.ComposeAggregateTestCheckFunc(enterpriseGone, acctest.CheckOrganizationsDeleted(t, organization)),
		Steps: []resource.TestStep{
			{
				Config: config("First name", "", license),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(enterpriseAddress, "key", key),
					resource.TestCheckResourceAttr(enterpriseAddress, "avatar", ""),
					func(s *terraform.State) error {
						id = s.RootModule().Resources[enterpriseAddress].Primary.ID
						if id == "" {
							return errors.New("the enterprise has no id")
						}
						return nil
					},
				),
			},
			{
				Config:           config("Second name", "https://example.com/avatar.png", license),
				ConfigPlanChecks: acctest.PlanAction(enterpriseAddress, plancheck.ResourceActionUpdate),
				Check:            checkEnterprise(t, &id, "Second name", "https://example.com/avatar.png"),
			},
			{
				Config: withDataSource("Second name", "https://example.com/avatar.png"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.sonarqube_cloud_enterprise.test", "id", enterpriseAddress, "id"),
					resource.TestCheckResourceAttr("data.sonarqube_cloud_enterprise.test", "name", "Second name"),
					resource.TestCheckResourceAttr("data.sonarqube_cloud_enterprise.test", "avatar", "https://example.com/avatar.png"),
				),
			},
			{
				// A new license stops the plan, and nothing is replaced.
				Config:      config("Second name", "https://example.com/avatar.png", "test-other-license"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`license of an enterprise cannot change`),
			},
			{
				ResourceName:            enterpriseAddress,
				ImportState:             true,
				ImportStateId:           key,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"license", "organization"},
			},
			acctest.ImportError(enterpriseAddress, acctest.MissingKey, regexp.MustCompile(`Enterprise .* not found`)),
			{
				// Destroy deletes the enterprise, and the organization stays.
				Config: fmt.Sprintf(`
resource "sonarqube_cloud_organization" "test" {
  key  = %[1]q
  name = %[1]q
}
`, organization),
				Check: resource.ComposeAggregateTestCheckFunc(enterpriseGone, func(*terraform.State) error {
					_, err := acctest.Client().GetOrganization(t.Context(), organization)
					return err
				}),
			},
		},
	})
}

func checkEnterprise(t *testing.T, id *string, name, avatar string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		enterprise, err := acctest.Client().GetEnterprise(t.Context(), *id)
		if err != nil {
			return err
		}
		if enterprise.Name != name || enterprise.Avatar != avatar {
			return fmt.Errorf("enterprise has name %q and avatar %q, want %q and %q", enterprise.Name, enterprise.Avatar, name, avatar)
		}
		return nil
	}
}
