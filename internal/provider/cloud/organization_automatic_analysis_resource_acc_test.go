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

const organizationAutomaticAnalysisAddress = "sonarqube_cloud_organization_automatic_analysis.test"

// Only an Enterprise organization can have its own value, so the test attaches
// a new organization to the enterprise of SONARQUBE_TEST_ENTERPRISE_KEY.
func TestAccOrganizationAutomaticAnalysisResource(t *testing.T) {
	acctest.SkipWithSOT(t, acctest.SOTNewOrganization)
	organization := fmt.Sprintf("%s%d", acctest.KeyPrefix, rand.Uint32())
	enterprise := os.Getenv(acctest.EnvTestEnterpriseKey)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheckDestructive(t)
			if enterprise == "" {
				t.Skipf("%s is not set. The setting needs an organization on the Enterprise plan.", acctest.EnvTestEnterpriseKey)
			}
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckOrganizationsDeleted(t, organization),
		Steps: []resource.TestStep{
			{
				Config: testAccOrganizationAutomaticAnalysisConfig(organization, enterprise, "false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(organizationAutomaticAnalysisAddress, "id", organization),
					resource.TestCheckResourceAttr(organizationAutomaticAnalysisAddress, "enabled_for_new_projects", "false"),
					testAccCheckOrganizationAutomaticAnalysis(t, organization, new(false)),
				),
			},
			{
				ResourceName:      organizationAutomaticAnalysisAddress,
				ImportState:       true,
				ImportStateId:     organization,
				ImportStateVerify: true,
			},
			{
				// A reset outside of Terraform gives the default again. The
				// refresh stores null, and the apply sets the value again.
				PreConfig: func() {
					if err := acctest.Client().ResetOrganizationAutomaticAnalysis(t.Context(), organization); err != nil {
						t.Fatalf("cannot reset the value outside of Terraform: %v", err)
					}
				},
				Config:           testAccOrganizationAutomaticAnalysisConfig(organization, enterprise, "false"),
				ConfigPlanChecks: acctest.PlanAction(organizationAutomaticAnalysisAddress, plancheck.ResourceActionUpdate),
				Check:            testAccCheckOrganizationAutomaticAnalysis(t, organization, new(false)),
			},
			{
				// The destroy of the resource removes the value of the
				// organization, and keeps the organization.
				Config:           testAccOrganizationAutomaticAnalysisConfig(organization, enterprise, ""),
				ConfigPlanChecks: acctest.PlanAction(organizationAutomaticAnalysisAddress, plancheck.ResourceActionDestroy),
				Check:            testAccCheckOrganizationAutomaticAnalysis(t, organization, nil),
			},
		},
	})
}

// A new organization is on the Free plan, which cannot have its own value.
func TestAccOrganizationAutomaticAnalysisResourceNotEnterprise(t *testing.T) {
	acctest.SkipWithSOT(t, acctest.SOTNewOrganization)
	organization := fmt.Sprintf("%s%d", acctest.KeyPrefix, rand.Uint32())

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheckDestructive(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckOrganizationsDeleted(t, organization),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "sonarqube_cloud_organization" "test" {
  key  = %q
  name = "Terraform acceptance test"
}

resource "sonarqube_cloud_organization_automatic_analysis" "test" {
  organization             = sonarqube_cloud_organization.test.key
  enabled_for_new_projects = false
}
`, organization),
				ExpectError: regexp.MustCompile(`must be on the Enterprise plan`),
			},
		},
	})
}

// testAccCheckOrganizationAutomaticAnalysis reads the value of the
// organization from the API. A nil want expects no value of its own.
func testAccCheckOrganizationAutomaticAnalysis(t *testing.T, organization string, want *bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got, err := acctest.Client().GetOrganizationAutomaticAnalysis(t.Context(), organization)
		switch {
		case err != nil:
			return err
		case want == nil && got.Own:
			return fmt.Errorf("the organization %s still has its own value %t", organization, got.Enabled)
		case want != nil && (!got.Own || got.Enabled != *want):
			return fmt.Errorf("Automatic Analysis for new projects of %s is %+v, want its own value %t", organization, got, *want)
		}
		return nil
	}
}

// testAccOrganizationAutomaticAnalysisConfig makes the organization and
// attaches it to the enterprise, and sets the value when enabled is not empty.
func testAccOrganizationAutomaticAnalysisConfig(organization, enterprise, enabled string) string {
	config := fmt.Sprintf(`
resource "sonarqube_cloud_organization" "test" {
  key  = %[1]q
  name = "Terraform acceptance test"
}

data "sonarqube_cloud_enterprise" "test" {
  key = %[2]q
}

resource "sonarqube_cloud_enterprise_organization" "test" {
  enterprise_id = data.sonarqube_cloud_enterprise.test.id
  organization  = sonarqube_cloud_organization.test.key
}
`, organization, enterprise)
	if enabled == "" {
		return config
	}
	return config + fmt.Sprintf(`
resource "sonarqube_cloud_organization_automatic_analysis" "test" {
  organization             = sonarqube_cloud_enterprise_organization.test.organization
  enabled_for_new_projects = %s
}
`, enabled)
}
