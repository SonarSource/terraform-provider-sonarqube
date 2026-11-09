package shared_test

import (
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

// TestAccQualityProfileRuleResourceInactive deactivates inheritedRule, which
// the profile gets from Sonar way core. A change of an inherited rule needs a
// custom profile, which needs the Team or the Enterprise plan, so the test
// uses the organization of SONARQUBE_TEST_ORGANIZATION.
func TestAccQualityProfileRuleResourceInactive(t *testing.T) {
	acctest.SkipWithSOT(t, acctest.SOTRefused)
	acctest.PreCheckAcceptance(t)
	organization := acctest.PreCheckSharedOrganization(t)
	p := qualityProfileRules{t: t, organization: organization,
		name: fmt.Sprintf("%sinactive-rule-%d", acctest.KeyPrefix, rand.Uint32())}
	inactive := p.inactiveConfig(`active = false`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// The last step checks that the destroy of the rule activates it again
		// while the profile still exists.
		CheckDestroy: func(*terraform.State) error { return p.profileGone() },
		Steps: []resource.TestStep{
			{
				Config: inactive,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(inheritedRuleAddress, "active", "false"),
					p.ruleNotActive(inheritedRule),
				),
			},
			// A second apply shows no changes.
			{Config: inactive, PlanOnly: true},
			{ResourceName: inheritedRuleAddress, ImportState: true, ImportStateIdFunc: p.importID(inheritedRule), ImportStateVerify: true},
			{
				// An activation outside of Terraform shows in the plan, and the
				// apply deactivates the rule again.
				PreConfig: func() { p.activateOutside(inheritedRule) },
				Config:    inactive, PlanOnly: true, ExpectNonEmptyPlan: true,
			},
			{Config: inactive, Check: p.ruleNotActive(inheritedRule)},
			{
				// active = true activates the rule again in place.
				Config:           p.inactiveConfig(`impacts = { SECURITY = "LOW" }`),
				ConfigPlanChecks: acctest.PlanAction(inheritedRuleAddress, plancheck.ResourceActionUpdate),
				Check:            p.hasRule(inheritedRule, client.RuleInheritOverrides, nil, map[string]string{"SECURITY": "LOW"}),
			},
			{Config: inactive, Check: p.ruleNotActive(inheritedRule)},
			{
				// Sonar way core does not activate ownRule, so the profile
				// cannot deactivate it.
				Config: inactive + fmt.Sprintf(`
resource "sonarqube_quality_profile_rule" "own" {
  quality_profile_key = sonarqube_quality_profile.test.key
  rule                = %q
  active              = false
}
`, ownRule),
				ExpectError: regexp.MustCompile(`The parent profile does not activate the rule`),
			},
			{
				// The destroy activates the rule again, with the values of the
				// parent.
				Config: p.config(ruleConfig{noRules: true}),
				Check:  p.hasParentValues(inheritedRule),
			},
		},
	})
}

// inactiveConfig gives the profile and inheritedRule with the attribute.
func (p qualityProfileRules) inactiveConfig(attribute string) string {
	return p.config(ruleConfig{noRules: true}) + fmt.Sprintf(`
resource "sonarqube_quality_profile_rule" "inherited" {
  quality_profile_key = sonarqube_quality_profile.test.key
  rule                = %q
  %s
}
`, inheritedRule, attribute)
}

// activateOutside activates the rule again outside of Terraform, and waits
// until the search shows it.
func (p qualityProfileRules) activateOutside(rule string) {
	p.t.Helper()
	key, err := p.profileKey()
	if err != nil {
		p.t.Fatal(err)
	}
	if err := acctest.Client().ActivateQualityProfileRule(p.t.Context(), key, rule, client.RuleValues{}); err != nil {
		p.t.Fatalf("cannot activate the rule outside of Terraform: %v", err)
	}
	p.waitForSteadyRule(key, rule, func(active *client.ActiveRule) bool { return active.Inherit == client.RuleInheritInherited })
}
