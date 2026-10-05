package cloud_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// The addresses in the configuration below.
const (
	qualityGateAddress     = "sonarqube_cloud_quality_gate.test"
	qualityGateDataAddress = "data.sonarqube_cloud_quality_gate.test"
)

// The test makes an organization and a gate in it. Each step after the first
// compares the condition identifiers with the API, so it shows that an apply
// changes only the conditions that the configuration changes.
func TestAccQualityGateResource(t *testing.T) {
	organization := fmt.Sprintf("%s%d", acctest.KeyPrefix, rand.Uint32())
	gateName := "Terraform test gate"
	// The first step records these. The later steps compare with them.
	var gateID string
	var firstIDs map[string]string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheckDestructive(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			_, err := acctest.Client().GetQualityGate(t.Context(), gateID)
			if errors.Is(err, client.ErrNotFound) {
				return nil
			}
			if err == nil {
				return fmt.Errorf("quality gate %s exists after the destroy", gateID)
			}
			return err
		},
		Steps: []resource.TestStep{
			{
				// resource.Test plans again after each apply and fails when
				// that plan is not empty.
				Config: testAccQualityGateConfig(organization, gateName, "80", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(qualityGateAddress, "id"),
					resource.TestCheckResourceAttr(qualityGateAddress, "ai_qualified", "false"),
					resource.TestCheckResourceAttr(qualityGateAddress, "condition.#", "2"),
					resource.TestCheckTypeSetElemNestedAttrs(qualityGateAddress, "condition.*", map[string]string{
						"metric": "new_coverage", "operator": "LT", "threshold": "80",
					}),
					resource.TestCheckResourceAttrPair(qualityGateDataAddress, "id", qualityGateAddress, "id"),
					resource.TestCheckResourceAttr(qualityGateDataAddress, "condition.#", "2"),
					func(s *terraform.State) error {
						gateID = s.RootModule().Resources[qualityGateAddress].Primary.ID
						var err error
						firstIDs, err = testAccQualityGateConditionIDs(t, gateID)
						return err
					},
				),
			},
			{
				// A new threshold changes the condition in place, and a new
				// block adds one condition. The other condition stays.
				Config: testAccQualityGateConfig(organization, gateName, "85", true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(qualityGateAddress, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(qualityGateAddress, "condition.#", "3"),
					resource.TestCheckTypeSetElemNestedAttrs(qualityGateAddress, "condition.*", map[string]string{
						"metric": "new_coverage", "threshold": "85",
					}),
					testAccCheckQualityGateConditionIDs(t, &gateID, func(ids map[string]string) error {
						return testAccSameConditionIDs(firstIDs, ids, "new_coverage", "new_duplicated_lines_density")
					}),
				),
			},
			{
				// A removed block deletes only that condition.
				Config: testAccQualityGateConfig(organization, gateName, "85", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(qualityGateAddress, "condition.#", "2"),
					testAccCheckQualityGateConditionIDs(t, &gateID, func(ids map[string]string) error {
						if _, found := ids["new_violations"]; found {
							return errors.New("the new_violations condition exists after its block was removed")
						}
						return testAccSameConditionIDs(firstIDs, ids, "new_coverage", "new_duplicated_lines_density")
					}),
				),
			},
			{
				// A change outside of Terraform shows in the plan, and the
				// apply sets the configured threshold again.
				PreConfig: func() {
					c := acctest.Client()
					if err := c.UpdateQualityGateCondition(t.Context(), firstIDs["new_coverage"], client.QualityGateConditionRequest{
						Operator: "LT", Threshold: "70",
					}); err != nil {
						t.Fatalf("cannot change the condition outside of Terraform: %v", err)
					}
				},
				Config: testAccQualityGateConfig(organization, gateName, "85", false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(qualityGateAddress, plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckTypeSetElemNestedAttrs(qualityGateAddress, "condition.*", map[string]string{
					"metric": "new_coverage", "threshold": "85",
				}),
			},
			{
				ResourceName:      qualityGateAddress,
				ImportState:       true,
				ImportStateId:     organization + "/" + gateName,
				ImportStateVerify: true,
			},
		},
	})
}

// testAccQualityGateConditionIDs reads the condition identifiers of a gate
// from the API, by metric key.
func testAccQualityGateConditionIDs(t *testing.T, gateID string) (map[string]string, error) {
	c := acctest.Client()
	conditions, err := c.ListQualityGateConditions(t.Context(), gateID)
	if err != nil {
		return nil, err
	}
	keys, err := c.ListMetrics(t.Context())
	if err != nil {
		return nil, err
	}
	ids := make(map[string]string, len(conditions))
	for _, condition := range conditions {
		ids[keys[condition.LegacyMetricID]] = condition.ID
	}
	return ids, nil
}

func testAccCheckQualityGateConditionIDs(t *testing.T, gateID *string, check func(map[string]string) error) resource.TestCheckFunc {
	return func(*terraform.State) error {
		ids, err := testAccQualityGateConditionIDs(t, *gateID)
		if err != nil {
			return err
		}
		return check(ids)
	}
}

// testAccSameConditionIDs fails when the API replaced a condition instead of
// keeping it.
func testAccSameConditionIDs(before, after map[string]string, metrics ...string) error {
	for _, metric := range metrics {
		if before[metric] == "" || before[metric] != after[metric] {
			return fmt.Errorf("condition on %s has identifier %q, before %q", metric, after[metric], before[metric])
		}
	}
	return nil
}

func testAccQualityGateConfig(organization, gateName, coverage string, extra bool) string {
	more := ""
	if extra {
		more = `
  condition {
    metric    = "new_violations"
    operator  = "GT"
    threshold = "0"
  }`
	}
	return fmt.Sprintf(`
resource "sonarqube_cloud_organization" "test" {
  key  = %[1]q
  name = "Terraform acceptance test"
}

resource "sonarqube_cloud_quality_gate" "test" {
  organization      = sonarqube_cloud_organization.test.key
  name              = %[2]q
  ai_qualified = false

  condition {
    metric    = "new_coverage"
    operator  = "LT"
    threshold = %[3]q
  }

  condition {
    metric    = "new_duplicated_lines_density"
    operator  = "GT"
    threshold = "3"
  }
  %[4]s
}

data "sonarqube_cloud_quality_gate" "test" {
  organization = sonarqube_cloud_quality_gate.test.organization
  name         = sonarqube_cloud_quality_gate.test.name
}
`, organization, gateName, coverage, more)
}
