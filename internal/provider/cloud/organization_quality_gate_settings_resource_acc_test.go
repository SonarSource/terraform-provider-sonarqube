package cloud_test

import (
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
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
		Steps: []resource.TestStep{
			{
				Config: config(true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sonarqube_cloud_organization_quality_gate_settings.test", "id", key),
					resource.TestCheckResourceAttr("sonarqube_cloud_organization_quality_gate_settings.test", "ignore_small_changes", "true"),
				),
			},
			{Config: config(true)},
			{
				PreConfig: func() {
					c := client.New(client.Config{
						URL: os.Getenv(provider.EnvURL), APIURL: os.Getenv(provider.EnvAPIURL),
						Token: os.Getenv(provider.EnvToken), Product: client.ProductCloud,
					})
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
		},
	})
}
