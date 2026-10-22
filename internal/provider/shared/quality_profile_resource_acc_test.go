package shared_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

const (
	qualityProfileAddress = "sonarqube_quality_profile.test"
	childProfileAddress   = "sonarqube_quality_profile.child"
	coreProfileAddress    = "data.sonarqube_quality_profile.core"
)

func TestAccQualityProfileResource(t *testing.T) {
	acctest.PreCheckAcceptance(t)
	organization := acctest.PreCheckSharedOrganization(t)
	base := fmt.Sprintf("%sprofile-%d", acctest.KeyPrefix, rand.Uint32())
	p := qualityProfiles{t: t, organization: organization, name: base + "/first", child: base + "-child"}
	renamed := p.name + "-renamed"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			return errors.Join(p.gone(p.name), p.gone(p.name+"-ui"), p.gone(renamed), p.gone(renamed+"-again"), p.gone(p.child))
		},
		Steps: []resource.TestStep{
			{
				Config: p.config(p.name, true, false, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(qualityProfileAddress, "key"),
					resource.TestCheckResourceAttrPair(qualityProfileAddress, "id", qualityProfileAddress, "key"),
					resource.TestCheckResourceAttrPair(qualityProfileAddress, "parent_key", coreProfileAddress, "key"),
					resource.TestCheckResourceAttr(coreProfileAddress, "is_built_in", "true"),
					resource.TestCheckNoResourceAttr(coreProfileAddress, "parent_key"),
					p.hasParent(p.name, true),
				),
			},
			{ResourceName: qualityProfileAddress, ImportState: true, ImportStateId: organization + "/java/" + p.name, ImportStateVerify: true},
			acctest.ImportError(qualityProfileAddress, organization+"/java",
				regexp.MustCompile(`Invalid quality profile import ID`)),
			acctest.ImportError(qualityProfileAddress, organization+"/java/"+acctest.MissingKey,
				regexp.MustCompile(`Cannot import the quality profile `+acctest.MissingKey)),
			acctest.ImportError(qualityProfileAddress, organization+"/java/Sonar way core",
				regexp.MustCompile(`Cannot import the built-in quality profile`)),
			{
				// A rename and a removed parent in the UI show in the next
				// plan, and the apply sets the configuration again.
				PreConfig:        func() { p.changeOutside(p.name) },
				Config:           p.config(p.name, true, false, false),
				ConfigPlanChecks: acctest.PlanAction(qualityProfileAddress, plancheck.ResourceActionUpdate),
				Check:            p.hasParent(p.name, true),
			},
			{
				// A rename and the removal of parent_key keep the profile.
				Config:           p.config(renamed, false, false, false),
				ConfigPlanChecks: acctest.PlanAction(qualityProfileAddress, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(qualityProfileAddress, "name", renamed),
					resource.TestCheckNoResourceAttr(qualityProfileAddress, "parent_key"),
					p.hasParent(renamed, false),
				),
			},
			{
				Config: p.config(renamed, false, true, false),
				Check:  resource.TestCheckResourceAttrPair(childProfileAddress, "parent_key", qualityProfileAddress, "key"),
			},
			{
				// A delete of the parent outside Terraform also deletes the
				// child, so the next plan creates both again.
				PreConfig: func() { p.deleteOutside(renamed) },
				Config:    p.config(renamed, false, true, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(qualityProfileAddress, plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction(childProfileAddress, plancheck.ResourceActionCreate),
				}},
				Check: resource.TestCheckResourceAttrPair(childProfileAddress, "parent_key", qualityProfileAddress, "key"),
			},
			{
				// A replacement of the parent also deletes the child, but the
				// plan only updates the parent_key of the child.
				Taint:       []string{qualityProfileAddress},
				Config:      p.config(renamed, false, true, false),
				ExpectError: regexp.MustCompile(`replace_triggered_by`),
			},
			{
				// The refresh removes the child, so the apply creates it again.
				Config:           p.config(renamed, false, true, false),
				ConfigPlanChecks: acctest.PlanAction(childProfileAddress, plancheck.ResourceActionCreate),
				Check:            resource.TestCheckResourceAttrPair(childProfileAddress, "parent_key", qualityProfileAddress, "key"),
			},
			{
				// With replace_triggered_by, one apply replaces both.
				Taint:  []string{qualityProfileAddress},
				Config: p.config(renamed, false, true, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(qualityProfileAddress, plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction(childProfileAddress, plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.TestCheckResourceAttrPair(childProfileAddress, "parent_key", qualityProfileAddress, "key"),
			},
			{
				// A rename of the parent keeps its key, so the child stays.
				Config: p.config(renamed+"-again", false, true, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(qualityProfileAddress, plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction(childProfileAddress, plancheck.ResourceActionNoop),
				}},
			},
		},
	})
}

func TestAccQualityProfileDataSourceMissingProfile(t *testing.T) {
	config := fmt.Sprintf(`data "sonarqube_quality_profile" "missing" {
  organization = %q
  language     = "java"
  name         = %q
}`, os.Getenv(acctest.EnvTestOrganization), acctest.MissingKey)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config,
			ExpectError: regexp.MustCompile(`Cannot find the quality profile ` + acctest.MissingKey),
		}},
	})
}

// qualityProfiles builds the configurations and the checks of the test.
type qualityProfiles struct {
	t            *testing.T
	organization string
	name         string
	child        string
}

// config makes a Java profile with the name. With parent set, it extends
// Sonar way core. With child set, a second profile extends it, and with
// replaceChild also set, a replacement of the first profile replaces it.
func (p qualityProfiles) config(name string, parent, child, replaceChild bool) string {
	config := fmt.Sprintf(`data "sonarqube_quality_profile" "core" {
  organization = %q
  language     = "java"
  name         = "Sonar way core"
}

resource "sonarqube_quality_profile" "test" {
  organization = %q
  language     = "java"
  name         = %q
`, p.organization, p.organization, name)
	if parent {
		config += "  parent_key   = data.sonarqube_quality_profile.core.key\n"
	}
	config += "}\n"
	if child {
		config += fmt.Sprintf(`
resource "sonarqube_quality_profile" "child" {
  organization = %q
  language     = "java"
  name         = %q
  parent_key   = sonarqube_quality_profile.test.key
`, p.organization, p.child)
		if replaceChild {
			config += "\n  lifecycle {\n    replace_triggered_by = [sonarqube_quality_profile.test.key]\n  }\n"
		}
		config += "}\n"
	}
	return config
}

func (p qualityProfiles) find(name string) (*client.QualityProfile, error) {
	return acctest.Client().FindQualityProfileByName(p.t.Context(), p.organization, "java", name)
}

// hasParent checks on the server that the profile with the name extends Sonar
// way core, or extends no profile.
func (p qualityProfiles) hasParent(name string, parent bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		profile, err := p.find(name)
		if err != nil {
			return err
		}
		core, err := p.find("Sonar way core")
		if err != nil {
			return err
		}
		want := ""
		if parent {
			want = core.Key
		}
		if profile.ParentKey != want {
			return fmt.Errorf("the parent of %q is %q, want %q", name, profile.ParentKey, want)
		}
		return nil
	}
}

// changeOutside renames the profile and removes its parent, as a user in the
// UI could do.
func (p qualityProfiles) changeOutside(name string) {
	c := acctest.Client()
	profile, err := p.find(name)
	if err != nil {
		p.t.Fatal(err)
	}
	if err := c.RenameQualityProfile(p.t.Context(), profile.Key, name+"-ui"); err != nil {
		p.t.Fatal(err)
	}
	if err := c.ChangeQualityProfileParent(p.t.Context(), profile.Key, ""); err != nil {
		p.t.Fatal(err)
	}
}

func (p qualityProfiles) deleteOutside(name string) {
	profile, err := p.find(name)
	if err != nil {
		p.t.Fatal(err)
	}
	if err := acctest.Client().DeleteQualityProfile(p.t.Context(), profile.Key); err != nil {
		p.t.Fatal(err)
	}
	if err := p.gone(p.child); err != nil {
		p.t.Fatal(err)
	}
}

func (p qualityProfiles) gone(name string) error {
	_, err := p.find(name)
	if errors.Is(err, client.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("the quality profile %q still exists", name)
}
