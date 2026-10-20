package cloud_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
)

func TestAccOrganizationQualityGateSettingsResource(t *testing.T) {
	key := fmt.Sprintf("%s%d", acctest.KeyPrefix, rand.Uint32())
	config := func(ignore bool) string {
		return fmt.Sprintf(`
resource "sonarqube_cloud_organization" "test" {
  key  = %[1]q
  name = "Quality gate settings test"
}

resource "sonarqube_cloud_organization_quality_gate_settings" "test" {
  organization         = sonarqube_cloud_organization.test.key
  ignore_small_changes = %[2]t
}
`, key, ignore)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheckDestructive(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// The destroy of the settings only removes them from the state. The
		// settings are gone when their organization is gone.
		CheckDestroy: acctest.CheckOrganizationsDeleted(t, key),
		Steps: []resource.TestStep{
			{
				Config: config(true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sonarqube_cloud_organization_quality_gate_settings.test", "id", key),
					resource.TestCheckResourceAttr("sonarqube_cloud_organization_quality_gate_settings.test", "ignore_small_changes", "true"),
				),
			},
			{
				PreConfig: func() {
					c := acctest.Client()
					org, err := c.GetOrganization(t.Context(), key)
					if err != nil {
						t.Fatal(err)
					}
					settings, err := c.GetOrganizationQualityGateSettings(t.Context(), org.UUIDV4)
					if err != nil {
						t.Fatal(err)
					}
					if err := c.UpdateOrganizationQualityGateSettings(t.Context(), settings.ID, false); err != nil {
						t.Fatal(err)
					}
				},
				Config:             config(true),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{Config: config(true)},
			{Config: config(false)},
			{
				ResourceName:      "sonarqube_cloud_organization_quality_gate_settings.test",
				ImportState:       true,
				ImportStateId:     key,
				ImportStateVerify: true,
			},
			acctest.ImportError("sonarqube_cloud_organization_quality_gate_settings.test", acctest.MissingKey, acctest.MissingImport),
		},
	})
}
