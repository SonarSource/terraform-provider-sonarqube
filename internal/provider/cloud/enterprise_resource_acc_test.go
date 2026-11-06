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
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

const (
	enterpriseAddress  = "sonarqube_cloud_enterprise.test"
	attachmentAddress  = "sonarqube_cloud_enterprise_organization.second"
	firstAttachAddress = "sonarqube_cloud_enterprise_organization.first"
)

// The test uses up the license of SONARQUBE_TEST_ENTERPRISE_LICENSE, so it
// skips when the variable is not set. It also tests
// sonarqube_cloud_enterprise_organization, so that one license is sufficient.
func TestAccEnterpriseResource(t *testing.T) {
	acctest.SkipWithSOT(t, acctest.SOTRefused)
	license := acctest.PreCheckEnterprise(t)
	suffix := rand.Uint32()
	organization := fmt.Sprintf("%s%d", acctest.KeyPrefix, suffix)
	second := fmt.Sprintf("%s%d-2", acctest.KeyPrefix, suffix)
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
	secondOrganization := fmt.Sprintf(`
resource "sonarqube_cloud_organization" "second" {
  key  = %[1]q
  name = %[1]q
}
`, second)
	attachment := func(name, organizationKey string) string {
		return fmt.Sprintf(`
resource "sonarqube_cloud_enterprise_organization" %[1]q {
  enterprise_id = sonarqube_cloud_enterprise.test.id
  organization  = %[2]s
}
`, name, organizationKey)
	}
	base := config("Second name", "https://example.com/avatar.png", license) + secondOrganization
	withAttachment := base + attachment("second", "sonarqube_cloud_organization.second.key")
	withFirstAttachment := base + attachment("first", "sonarqube_cloud_organization.test.key")
	// The legacy ID of the second organization stays known after its delete,
	// so that CheckDestroy can look for its relation.
	var secondID string
	secondDetached := acctest.CheckGone("relation of "+second, func() (*client.EnterpriseOrganization, error) {
		if secondID == "" {
			return nil, client.ErrNotFound
		}
		return relationOf(t, id, secondID)
	})

	enterpriseGone := acctest.CheckGone("enterprise "+key, func() (*client.Enterprise, error) {
		return acctest.Client().GetEnterprise(t.Context(), id)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// The removed block needs Terraform 1.7. A failed check stops the test
		// before the create uses up the license. A skip would count as a pass.
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{tfversion.RequireAbove(tfversion.Version1_7_0)},
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(enterpriseGone, secondDetached,
			acctest.CheckOrganizationsDeleted(t, organization, second)),
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
				// A free organization can be attached.
				Config:           withAttachment,
				ConfigPlanChecks: acctest.PlanAction(attachmentAddress, plancheck.ResourceActionCreate),
				Check:            checkAttached(t, attachmentAddress, &id, &secondID),
			},
			acctest.DeletedOutside(t, attachmentAddress, withAttachment, checkAttached(t, attachmentAddress, &id, &secondID),
				func() error {
					relation, err := relationOf(t, id, secondID)
					if err != nil {
						return err
					}
					return acctest.Client().DetachEnterpriseOrganization(t.Context(), relation.ID)
				},
				func() (*client.EnterpriseOrganization, error) { return relationOf(t, id, secondID) }),
			{
				ResourceName:      attachmentAddress,
				ImportState:       true,
				ImportStateId:     key + "/" + second,
				ImportStateVerify: true,
			},
			{
				// Destroy detaches the organization. The organization stays in
				// the state, so the plan after the apply finds it.
				Config: base,
				Check:  secondDetached,
			},
			{
				// The first organization is adopted, not attached again.
				Config:           withFirstAttachment,
				ConfigPlanChecks: acctest.PlanAction(firstAttachAddress, plancheck.ResourceActionCreate),
				Check:            checkAttached(t, firstAttachAddress, &id, nil),
			},
			{
				// A detach of the last organization would delete the enterprise.
				Config:      base,
				ExpectError: regexp.MustCompile(`Cannot detach the last organization`),
			},
			{
				Config: base + `
removed {
  from = sonarqube_cloud_enterprise_organization.first

  lifecycle {
    destroy = false
  }
}
`,
				Check: checkEnterprise(t, &id, "Second name", "https://example.com/avatar.png"),
			},
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

// relationOf finds the relation of an organization in the enterprise.
func relationOf(t *testing.T, enterpriseID, organizationID string) (*client.EnterpriseOrganization, error) {
	relations, err := acctest.Client().ListEnterpriseOrganizationsOfOrganization(t.Context(), organizationID)
	if err != nil {
		return nil, err
	}
	for _, relation := range relations {
		if relation.EnterpriseID == enterpriseID {
			return &relation, nil
		}
	}
	return nil, client.ErrNotFound
}

// checkAttached checks that the relation in the state exists. When
// organizationID is not nil, it gets the legacy ID of the organization.
func checkAttached(t *testing.T, address string, enterpriseID, organizationID *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s is not in the state", address)
		}
		attributes := rs.Primary.Attributes
		org, err := acctest.Client().GetOrganization(t.Context(), attributes["organization"])
		if err != nil {
			return err
		}
		if organizationID != nil {
			*organizationID = org.ID
		}
		relation, err := relationOf(t, *enterpriseID, org.ID)
		if err != nil {
			return fmt.Errorf("%s is not attached: %w", attributes["organization"], err)
		}
		if relation.ID != attributes["id"] || attributes["enterprise_id"] != *enterpriseID {
			return fmt.Errorf("relation %+v does not match the state %v", relation, attributes)
		}
		return nil
	}
}
