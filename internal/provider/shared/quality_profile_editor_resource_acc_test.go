package shared_test

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

const (
	groupEditorAddress = "sonarqube_quality_profile_editor.group"
	userEditorAddress  = "sonarqube_quality_profile_editor.user"
)

// qualityProfileEditors is the profile, the group and the user of the test.
type qualityProfileEditors struct {
	t                         *testing.T
	organization, name, login string
}

// TestAccQualityProfileEditorResource needs a custom profile and editors, which
// need the Team or the Enterprise plan, so it uses the organization of
// SONARQUBE_TEST_ORGANIZATION. The user editor is the token user, who is a
// member of that organization.
func TestAccQualityProfileEditorResource(t *testing.T) {
	acctest.SkipWithSOT(t, acctest.SOTNoUser)
	acctest.PreCheckAcceptance(t)
	organization := acctest.PreCheckSharedOrganization(t)
	login, err := acctest.Client().CurrentUserLogin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The slash checks that the import ID keeps a group name with "/".
	e := qualityProfileEditors{t: t, organization: organization, login: login,
		name: fmt.Sprintf("%seditor-%d/team", acctest.KeyPrefix, rand.Uint32())}
	importID := func(kind, editor string) resource.ImportStateIdFunc {
		return func(s *terraform.State) (string, error) {
			key, err := profileKey(s, "test")
			return organization + "/" + key + "/" + kind + "/" + editor, err
		}
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// The last step checks that the destroy of the editors takes their
		// rights while the profile still exists. A deleted profile has no
		// editors to search, so CheckDestroy checks that the profile is gone.
		CheckDestroy: func(s *terraform.State) error {
			key, err := profileKey(s, "test")
			if err != nil {
				return err
			}
			return acctest.CheckGone("quality profile "+key, func() (*client.QualityProfile, error) {
				return acctest.Client().GetQualityProfile(t.Context(), organization, "java", key)
			})(s)
		},
		Steps: []resource.TestStep{
			{
				Config: e.config(e.name, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(groupEditorAddress, "quality_profile_key", "sonarqube_quality_profile.test", "key"),
					e.hasEditors(true),
				),
			},
			{ResourceName: groupEditorAddress, ImportState: true, ImportStateIdFunc: importID("group", e.name), ImportStateVerify: true},
			{ResourceName: userEditorAddress, ImportState: true, ImportStateIdFunc: importID("user", login), ImportStateVerify: true},
			acctest.ImportError(groupEditorAddress, organization+"/missing-profile/group",
				regexp.MustCompile(`Invalid quality profile editor import ID`)),
			{
				// A removal outside of Terraform shows in the plan.
				PreConfig: func() { e.removeOutside() },
				Config:    e.config(e.name, true), PlanOnly: true, ExpectNonEmptyPlan: true,
			},
			{Config: e.config(e.name, true), Check: e.hasEditors(true)},
			{
				// A rename of the profile does not change the editors.
				Config: e.config(e.name+"-renamed", true),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(groupEditorAddress, plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction(userEditorAddress, plancheck.ResourceActionNoop),
				}},
				Check: e.hasEditors(true),
			},
			{Config: e.config(e.name+"-renamed", false), Check: e.hasEditors(false)},
		},
	})
}

// config gives the profile with the name profileName, the group, and the two
// editors when editors is true.
func (e qualityProfileEditors) config(profileName string, editors bool) string {
	config := fmt.Sprintf(`
resource "sonarqube_group" "test" {
  organization = %[1]q
  name         = %[2]q
}

resource "sonarqube_quality_profile" "test" {
  organization = %[1]q
  language     = "java"
  name         = %[3]q
}
`, e.organization, e.name, profileName)
	if !editors {
		return config
	}
	return config + fmt.Sprintf(`
resource "sonarqube_quality_profile_editor" "group" {
  organization        = %[1]q
  quality_profile_key = sonarqube_quality_profile.test.key
  group_name          = sonarqube_group.test.name
}

resource "sonarqube_quality_profile_editor" "user" {
  organization        = %[1]q
  quality_profile_key = sonarqube_quality_profile.test.key
  login               = %[2]q
}
`, e.organization, e.login)
}

// editors gives the group and the user editor of the test.
func (e qualityProfileEditors) editors() []client.PermissionPrincipal {
	return []client.PermissionPrincipal{{Group: true, Name: e.name}, {Name: e.login}}
}

// hasEditors checks in the API that the group and the user are editors of the
// profile of the state, or are not.
func (e qualityProfileEditors) hasEditors(want bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		profile, err := e.profile(s)
		if err != nil {
			return err
		}
		var errs []error
		for _, editor := range e.editors() {
			found, err := acctest.Client().HasQualityProfileEditor(e.t.Context(), e.organization, *profile, editor)
			switch {
			case err != nil:
				errs = append(errs, err)
			case found != want:
				errs = append(errs, fmt.Errorf("editor %+v found = %v, want %v", editor, found, want))
			}
		}
		return errors.Join(errs...)
	}
}

func (e qualityProfileEditors) profile(s *terraform.State) (*client.QualityProfile, error) {
	key, err := profileKey(s, "test")
	if err != nil {
		return nil, err
	}
	return acctest.Client().GetQualityProfile(e.t.Context(), e.organization, "java", key)
}

// removeOutside takes the right of the group outside of Terraform.
func (e qualityProfileEditors) removeOutside() {
	e.t.Helper()
	c := acctest.Client()
	profile, err := c.FindQualityProfileByName(e.t.Context(), e.organization, "java", e.name)
	if err != nil {
		e.t.Fatalf("cannot find the profile: %v", err)
	}
	if err := c.RemoveQualityProfileEditor(e.t.Context(), e.organization, *profile, client.PermissionPrincipal{Group: true, Name: e.name}); err != nil {
		e.t.Fatalf("cannot remove the editor outside of Terraform: %v", err)
	}
}
