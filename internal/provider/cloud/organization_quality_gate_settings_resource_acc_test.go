package cloud_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
)

func TestAccOrganizationQualityGateSettingsResource(t *testing.T) {
	acctest.SkipWithSOT(t, acctest.SOTNewOrganization)
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

// The test changes the shared organization, so it also runs with
// SONARQUBE_TEST_SOT_TOKEN. It sets the value back at the end.
func TestAccOrganizationQualityGateSettingsResourceSharedOrganization(t *testing.T) {
	acctest.PreCheckAcceptance(t)
	organization := acctest.PreCheckSharedOrganization(t)
	c := acctest.Client()
	org, err := c.GetOrganization(t.Context(), organization)
	if err != nil {
		t.Fatal(err)
	}
	original, err := c.GetOrganizationQualityGateSettings(t.Context(), org.UUIDV4)
	if err != nil {
		t.Fatal(err)
	}
	// A change applies to all projects of the organization, so the cleanup
	// sends one only when a step stopped before it set the value back.
	t.Cleanup(func() {
		current, err := c.GetOrganizationQualityGateSettings(context.Background(), org.UUIDV4)
		if err == nil && current.IgnoreSmallChanges == original.IgnoreSmallChanges {
			return
		}
		if err := c.UpdateOrganizationQualityGateSettings(context.Background(), original.ID, original.IgnoreSmallChanges); err != nil {
			t.Errorf("cannot set the quality gate settings back: %v", err)
		}
	})
	config := func(ignore bool) string {
		return fmt.Sprintf(`
resource "sonarqube_cloud_organization_quality_gate_settings" "test" {
  organization         = %q
  ignore_small_changes = %t
}
`, organization, ignore)
	}
	changed := !original.IgnoreSmallChanges

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// A destroy removes the settings from the state only, so the server
		// keeps the value of the last step.
		CheckDestroy: func(*terraform.State) error {
			settings, err := c.GetOrganizationQualityGateSettings(t.Context(), org.UUIDV4)
			if err != nil {
				return err
			}
			if settings.IgnoreSmallChanges != original.IgnoreSmallChanges {
				return fmt.Errorf("the destroy changed ignore_small_changes to %t", settings.IgnoreSmallChanges)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config(changed),
				Check: resource.TestCheckResourceAttr("sonarqube_cloud_organization_quality_gate_settings.test",
					"ignore_small_changes", fmt.Sprint(changed)),
			},
			{
				Config: config(original.IgnoreSmallChanges),
				Check: resource.TestCheckResourceAttr("sonarqube_cloud_organization_quality_gate_settings.test",
					"ignore_small_changes", fmt.Sprint(original.IgnoreSmallChanges)),
			},
		},
	})
}
