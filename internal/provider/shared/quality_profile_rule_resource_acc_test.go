package shared_test

import (
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

const (
	// ownRule is not active in Sonar way core, and has the parameters max and
	// constructorMax, with the default value 7, and a MAINTAINABILITY impact.
	ownRule        = "java:S107"
	ownRuleAddress = "sonarqube_quality_profile_rule.own"
	// inheritedRule is active in Sonar way core, with a SECURITY impact.
	inheritedRule        = "java:S3329"
	inheritedRuleAddress = "sonarqube_quality_profile_rule.inherited"
)

// ruleConfig is the configuration of the two rules of the test. Each field is
// the HCL value of its attribute, or empty for no attribute.
type ruleConfig struct {
	ownParams, ownImpacts, inheritedImpacts string
	// noRules leaves only the profile.
	noRules bool
}

func TestAccQualityProfileRuleResource(t *testing.T) {
	acctest.PreCheckAcceptance(t)
	organization := acctest.PreCheckSharedOrganization(t)
	p := qualityProfileRules{t: t, organization: organization,
		name: fmt.Sprintf("%srules-%d", acctest.KeyPrefix, rand.Uint32())}
	first := ruleConfig{ownParams: `{ max = "5" }`, inheritedImpacts: `{ SECURITY = "LOW" }`}
	changed := ruleConfig{ownParams: `{ max = "6", constructorMax = "8" }`, ownImpacts: `{ MAINTAINABILITY = "HIGH" }`,
		inheritedImpacts: `{ SECURITY = "LOW" }`}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// The last step checks the destroy of the rules while the profile
		// still exists. After the profile is deleted, the search finds no rule
		// of it, so CheckDestroy can only check the profile.
		CheckDestroy: func(*terraform.State) error { return p.profileGone() },
		Steps: []resource.TestStep{
			{
				Config: p.config(first),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(ownRuleAddress, "quality_profile_key", "sonarqube_quality_profile.test", "key"),
					p.hasRule(ownRule, client.RuleInheritNone, map[string]string{"max": "5", "constructorMax": "7"},
						map[string]string{"MAINTAINABILITY": "MEDIUM"}),
					p.hasRule(inheritedRule, client.RuleInheritOverrides, nil, map[string]string{"SECURITY": "LOW"}),
				),
			},
			// The import keeps the values that differ from the defaults, which
			// are the configured ones.
			{ResourceName: ownRuleAddress, ImportState: true, ImportStateIdFunc: p.importID(ownRule), ImportStateVerify: true},
			{ResourceName: inheritedRuleAddress, ImportState: true, ImportStateIdFunc: p.importID(inheritedRule), ImportStateVerify: true},
			acctest.ImportError(ownRuleAddress, "missing-profile",
				regexp.MustCompile(`Invalid quality profile rule import ID`)),
			{
				// A change in the UI to a configured value shows in the plan.
				PreConfig:        func() { p.changeOutside(ownRule, map[string]string{"max": "9"}) },
				Config:           p.config(first),
				ConfigPlanChecks: acctest.PlanAction(ownRuleAddress, plancheck.ResourceActionUpdate),
				Check: p.hasRule(ownRule, client.RuleInheritNone, map[string]string{"max": "5", "constructorMax": "7"},
					map[string]string{"MAINTAINABILITY": "MEDIUM"}),
			},
			{
				// A change of a parameter and an impact changes only that rule.
				Config: p.config(changed),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(ownRuleAddress, plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction(inheritedRuleAddress, plancheck.ResourceActionNoop),
				}},
				Check: p.hasRule(ownRule, client.RuleInheritNone, map[string]string{"max": "6", "constructorMax": "8"},
					map[string]string{"MAINTAINABILITY": "HIGH"}),
			},
			{
				// A removed parameter and a removed impact get the defaults
				// of the rule again.
				Config: p.config(first),
				Check: p.hasRule(ownRule, client.RuleInheritNone, map[string]string{"max": "5", "constructorMax": "7"},
					map[string]string{"MAINTAINABILITY": "MEDIUM"}),
			},
			{
				Config:      p.config(ruleConfig{ownParams: first.ownParams, ownImpacts: `{ SECURITY = "LOW" }`, inheritedImpacts: first.inheritedImpacts}),
				ExpectError: regexp.MustCompile(`The rule has no SECURITY impact`),
			},
			{
				Config:      p.config(first) + p.builtInRuleConfig(),
				ExpectError: regexp.MustCompile(`a built-in quality profile is read-only`),
			},
			{
				// A destroy deactivates the rule that the profile activates
				// itself, and gives the inherited rule the values of the
				// parent again.
				Config: p.config(ruleConfig{noRules: true}),
				Check: resource.ComposeAggregateTestCheckFunc(
					p.ruleNotActive(ownRule),
					p.hasParentValues(inheritedRule),
				),
			},
		},
	})
}

// qualityProfileRules builds the configurations and the checks of the test.
type qualityProfileRules struct {
	t            *testing.T
	organization string
	name         string
}

func (p qualityProfileRules) config(c ruleConfig) string {
	config := fmt.Sprintf(`data "sonarqube_quality_profile" "core" {
  organization = %q
  language     = "java"
  name         = "Sonar way core"
}

resource "sonarqube_quality_profile" "test" {
  organization = %q
  language     = "java"
  name         = %q
  parent_key   = data.sonarqube_quality_profile.core.key
}
`, p.organization, p.organization, p.name)
	if c.noRules {
		return config
	}
	config += fmt.Sprintf("\nresource \"sonarqube_quality_profile_rule\" \"own\" {\n"+
		"  quality_profile_key = sonarqube_quality_profile.test.key\n  rule = %q\n", ownRule)
	config += optionalAttribute("params", c.ownParams) + optionalAttribute("impacts", c.ownImpacts) + "}\n"
	config += fmt.Sprintf("\nresource \"sonarqube_quality_profile_rule\" \"inherited\" {\n"+
		"  quality_profile_key = sonarqube_quality_profile.test.key\n  rule = %q\n", inheritedRule)
	return config + optionalAttribute("impacts", c.inheritedImpacts) + "}\n"
}

func (p qualityProfileRules) builtInRuleConfig() string {
	return fmt.Sprintf(`
resource "sonarqube_quality_profile_rule" "built_in" {
  quality_profile_key = data.sonarqube_quality_profile.core.key
  rule                = %q
}
`, ownRule)
}

func optionalAttribute(name, value string) string {
	if value == "" {
		return ""
	}
	return "  " + name + " = " + value + "\n"
}

func (p qualityProfileRules) profileKey() (string, error) {
	profile, err := acctest.Client().FindQualityProfileByName(p.t.Context(), p.organization, "java", p.name)
	if err != nil {
		return "", err
	}
	return profile.Key, nil
}

func (p qualityProfileRules) importID(rule string) resource.ImportStateIdFunc {
	return func(*terraform.State) (string, error) {
		key, err := p.profileKey()
		return key + "/" + rule, err
	}
}

// hasRule waits until the search shows the rule in the profile with the
// inheritance and the values. A nil map is not checked.
func (p qualityProfileRules) hasRule(rule, inherit string, params, impacts map[string]string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		key, err := p.profileKey()
		if err != nil {
			return err
		}
		var last *client.ActiveRule
		err = acctest.WaitFor(func() (bool, error) {
			active, err := acctest.Client().GetActiveRule(p.t.Context(), key, rule)
			if errors.Is(err, client.ErrNotFound) {
				return false, nil
			}
			last = active
			return err == nil && active.Inherit == inherit &&
				(params == nil || maps.Equal(active.Values.Params, params)) &&
				(impacts == nil || maps.Equal(active.Values.Impacts, impacts)), err
		})
		if err != nil && last != nil {
			return fmt.Errorf("%w: the rule %s is %+v", err, rule, *last)
		}
		return err
	}
}

func (p qualityProfileRules) ruleNotActive(rule string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		key, err := p.profileKey()
		if err != nil {
			return err
		}
		return acctest.WaitForGone(func() (*client.ActiveRule, error) {
			return acctest.Client().GetActiveRule(p.t.Context(), key, rule)
		})
	}
}

// hasParentValues checks that the rule has the values of Sonar way core.
func (p qualityProfileRules) hasParentValues(rule string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		core, err := acctest.Client().FindQualityProfileByName(p.t.Context(), p.organization, "java", "Sonar way core")
		if err != nil {
			return err
		}
		parent, err := acctest.Client().GetActiveRule(p.t.Context(), core.Key, rule)
		if err != nil {
			return err
		}
		return p.hasRule(rule, client.RuleInheritInherited, parent.Values.Params, parent.Values.Impacts)(s)
	}
}

// changeOutside changes parameters of the rule, as a user in the UI could do.
func (p qualityProfileRules) changeOutside(rule string, params map[string]string) {
	key, err := p.profileKey()
	if err != nil {
		p.t.Fatal(err)
	}
	if err := acctest.Client().ActivateQualityProfileRule(p.t.Context(), key, rule, client.RuleValues{Params: params}); err != nil {
		p.t.Fatal(err)
	}
	err = acctest.WaitFor(func() (bool, error) {
		active, err := acctest.Client().GetActiveRule(p.t.Context(), key, rule)
		if errors.Is(err, client.ErrNotFound) {
			return false, nil
		}
		return err == nil && hasAll(active.Values.Params, params), err
	})
	if err != nil {
		p.t.Fatal(err)
	}
}

func hasAll(values, want map[string]string) bool {
	for key, value := range want {
		if values[key] != value {
			return false
		}
	}
	return true
}

func (p qualityProfileRules) profileGone() error {
	_, err := p.profileKey()
	if errors.Is(err, client.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("the quality profile %q still exists", p.name)
}
