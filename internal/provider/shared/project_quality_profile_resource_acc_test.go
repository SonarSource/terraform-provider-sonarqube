package shared_test

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

const projectQualityProfileAddress = "sonarqube_project_quality_profile.test"

// projectQualityProfiles is the project and the profiles of the test.
type projectQualityProfiles struct {
	t                  *testing.T
	organization, name string
}

// TestAccProjectQualityProfileResource needs a custom profile and add_project,
// which need the Team or the Enterprise plan, so it uses the organization of
// SONARQUBE_TEST_ORGANIZATION.
func TestAccProjectQualityProfileResource(t *testing.T) {
	acctest.PreCheckAcceptance(t)
	organization := acctest.PreCheckSharedOrganization(t)
	p := projectQualityProfiles{t: t, organization: organization,
		name: fmt.Sprintf("%sproject-profile-%d", acctest.KeyPrefix, rand.Uint32())}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// The step before the destroy checks that the project uses the
		// default profile again after the assignment is removed. Here the
		// project and the profiles are gone.
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			acctest.CheckProjectDeleted(t, organization, p.name),
			p.profilesGone,
		),
		Steps: []resource.TestStep{
			{
				Config: p.config("java_a"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectQualityProfileAddress, "id", organization+"/"+p.name+"/java"),
					resource.TestCheckResourceAttr(projectQualityProfileAddress, "language", "java"),
					resource.TestCheckResourceAttrPair(projectQualityProfileAddress, "quality_profile_key",
						"sonarqube_quality_profile.java_a", "key"),
					p.usesProfile("java", "java_a"),
				),
			},
			{
				ResourceName:      projectQualityProfileAddress,
				ImportState:       true,
				ImportStateId:     organization + "/" + p.name + "/java",
				ImportStateVerify: true,
			},
			acctest.ImportError(projectQualityProfileAddress, organization+"/"+p.name,
				regexp.MustCompile(`<organization>/<project_key>/<language>`)),
			{
				// A change in the UI shows in the plan, and the apply sets the
				// configured profile again.
				PreConfig:        func() { p.assignOutside("java_b") },
				Config:           p.config("java_a"),
				ConfigPlanChecks: acctest.PlanAction(projectQualityProfileAddress, plancheck.ResourceActionUpdate),
				Check:            p.usesProfile("java", "java_a"),
			},
			{
				// A profile of the same language changes the assignment in
				// place.
				Config:           p.config("java_b"),
				ConfigPlanChecks: acctest.PlanAction(projectQualityProfileAddress, plancheck.ResourceActionUpdate),
				Check:            p.usesProfile("java", "java_b"),
			},
			{
				// A profile of a different language replaces the assignment,
				// and the project uses the default java profile again.
				Config:           p.config("py"),
				ConfigPlanChecks: acctest.PlanAction(projectQualityProfileAddress, plancheck.ResourceActionReplace),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectQualityProfileAddress, "id", organization+"/"+p.name+"/py"),
					p.usesProfile("py", "py"),
					p.usesDefault("java"),
				),
			},
			{
				// The destroy of the assignment makes the project use the
				// default profile again.
				Config: p.config(""),
				Check:  p.usesDefault("py"),
			},
		},
	})
}

// testProfiles maps the name of each profile resource of the test to its
// language. The name of the profile is the name of the test, a dash and the
// name of the resource.
var testProfiles = map[string]string{"java_a": "java", "java_b": "java", "py": "py"}

func (p projectQualityProfiles) profileName(profile string) string { return p.name + "-" + profile }

// config gives the project, the profiles and the assignment of the profile
// resource named profile. An empty profile gives no assignment.
func (p projectQualityProfiles) config(profile string) string {
	var config strings.Builder
	fmt.Fprintf(&config, `
resource "sonarqube_project" "test" {
  organization = %[1]q
  key          = %[2]q
  name         = %[2]q
}
`, p.organization, p.name)
	for _, name := range slices.Sorted(maps.Keys(testProfiles)) {
		fmt.Fprintf(&config, `
resource "sonarqube_quality_profile" %q {
  organization = %q
  language     = %q
  name         = %q
}
`, name, p.organization, testProfiles[name], p.profileName(name))
	}
	if profile != "" {
		fmt.Fprintf(&config, `
resource "sonarqube_project_quality_profile" "test" {
  organization        = %q
  project_key         = sonarqube_project.test.key
  quality_profile_key = sonarqube_quality_profile.%s.key
}
`, p.organization, profile)
	}
	return config.String()
}

// profileKey gives the key of the profile resource named profile.
func profileKey(s *terraform.State, profile string) (string, error) {
	r, ok := s.RootModule().Resources["sonarqube_quality_profile."+profile]
	if !ok {
		return "", fmt.Errorf("the state has no profile %s", profile)
	}
	return r.Primary.Attributes["key"], nil
}

// usesProfile compares the profile that the API gives for the language of
// the project with the profile resource named profile.
func (p projectQualityProfiles) usesProfile(language, profile string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		want, err := profileKey(s, profile)
		if err != nil {
			return err
		}
		return p.checkProfile(language, want)
	}
}

// usesDefault checks that the project uses the default profile of the
// language.
func (p projectQualityProfiles) usesDefault(language string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		profile, err := acctest.Client().GetDefaultQualityProfile(p.t.Context(), p.organization, language)
		if err != nil {
			return err
		}
		return p.checkProfile(language, profile.Key)
	}
}

func (p projectQualityProfiles) checkProfile(language, want string) error {
	profile, err := acctest.Client().GetProjectQualityProfile(p.t.Context(), p.organization, p.name, language)
	if err != nil {
		return err
	}
	if profile.Key != want {
		return fmt.Errorf("the %s profile of the project is %q, want %q", language, profile.Key, want)
	}
	return nil
}

// profilesGone reports a profile of the state that the API still shows.
func (p projectQualityProfiles) profilesGone(s *terraform.State) error {
	for profile, language := range testProfiles {
		key, err := profileKey(s, profile)
		if err != nil {
			return err
		}
		err = acctest.CheckGone("quality profile "+key, func() (*client.QualityProfile, error) {
			return acctest.Client().GetQualityProfile(p.t.Context(), p.organization, language, key)
		})(s)
		if err != nil {
			return err
		}
	}
	return nil
}

// assignOutside assigns the profile resource named profile outside of
// Terraform.
func (p projectQualityProfiles) assignOutside(profile string) {
	p.t.Helper()
	c := acctest.Client()
	found, err := c.FindQualityProfileByName(p.t.Context(), p.organization, testProfiles[profile], p.profileName(profile))
	if err != nil {
		p.t.Fatalf("cannot find the profile %s: %v", profile, err)
	}
	if err := c.AddQualityProfileProject(p.t.Context(), found.Key, p.name); err != nil {
		p.t.Fatalf("cannot assign the profile outside of Terraform: %v", err)
	}
}
