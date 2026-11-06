package cloud_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
)

const (
	defaultQualityProfileAddress = "sonarqube_cloud_organization_default_quality_profile.test"
	// defaultQualityProfileLanguage is not java, which the other quality
	// profile tests use.
	defaultQualityProfileLanguage = "py"
)

// A custom profile needs the Team or the Enterprise plan, so the test uses the
// organization of SONARQUBE_TEST_ORGANIZATION.
//
// The test makes its profile outside of Terraform. The destroy does not change
// the default, and SonarQube Cloud refuses to delete the default profile. The
// last step therefore sets the original default again, and the cleanup does
// it too after a failed step, before it deletes the profile.
//
// The test changes the default of the shared organization, so it must not run
// in parallel with another test: do not call t.Parallel, and keep -p 1 in the
// Makefile.
func TestAccOrganizationDefaultQualityProfileResource(t *testing.T) {
	acctest.SkipWithSOT(t, acctest.SOTRefused)
	// The configuration of each step needs the profile, so the profile is made
	// before resource.Test, which would skip the test without TF_ACC.
	acctest.PreCheckAcceptance(t)
	organization := acctest.PreCheckSharedOrganization(t)
	original, profileKey := testAccPrepareDefaultQualityProfile(t, organization)
	javaDefault, err := acctest.Client().GetDefaultQualityProfile(t.Context(), organization, "java")
	if err != nil {
		t.Fatalf("cannot read the default java profile: %v", err)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// The destroy only removes the default from the state, so the last
		// default of the configuration, the original one, must stay.
		CheckDestroy: testAccCheckDefaultQualityProfile(t, organization, original),
		Steps: []resource.TestStep{
			{
				// set_default would make a java profile the default of java,
				// so the resource refuses it for py.
				Config:      testAccDefaultQualityProfileConfig(organization, javaDefault.Key),
				ExpectError: regexp.MustCompile(`language\s+java,\s+not\s+of\s+the\s+language\s+py`),
			},
			{
				Config: testAccDefaultQualityProfileConfig(organization, profileKey),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(defaultQualityProfileAddress, "id", organization+"/"+defaultQualityProfileLanguage),
					resource.TestCheckResourceAttr(defaultQualityProfileAddress, "language", defaultQualityProfileLanguage),
					resource.TestCheckResourceAttr(defaultQualityProfileAddress, "quality_profile_key", profileKey),
					testAccCheckDefaultQualityProfile(t, organization, profileKey),
					testAccCheckDefaultQualityProfileOf(t, organization, "java", javaDefault.Key),
				),
			},
			{
				// A change of the default outside of Terraform shows in the
				// plan, and the apply sets the configured profile again.
				PreConfig: func() {
					if err := acctest.Client().SetDefaultQualityProfile(t.Context(), original); err != nil {
						t.Fatalf("cannot change the default outside of Terraform: %v", err)
					}
				},
				Config:           testAccDefaultQualityProfileConfig(organization, profileKey),
				ConfigPlanChecks: acctest.PlanAction(defaultQualityProfileAddress, plancheck.ResourceActionUpdate),
				Check:            testAccCheckDefaultQualityProfile(t, organization, profileKey),
			},
			{
				ResourceName:      defaultQualityProfileAddress,
				ImportState:       true,
				ImportStateId:     organization + "/" + defaultQualityProfileLanguage,
				ImportStateVerify: true,
			},
			acctest.ImportError(defaultQualityProfileAddress, organization, regexp.MustCompile(`<organization>/<language>`)),
			{
				// A built-in profile can be the default too. This step also
				// sets the original default again before the destroy.
				Config:           testAccDefaultQualityProfileConfig(organization, original),
				ConfigPlanChecks: acctest.PlanAction(defaultQualityProfileAddress, plancheck.ResourceActionUpdate),
				Check:            testAccCheckDefaultQualityProfile(t, organization, original),
			},
		},
	})
}

// testAccPrepareDefaultQualityProfile makes a py profile for the test and
// registers the cleanup. It returns the key of the original default and the
// key of the profile.
func testAccPrepareDefaultQualityProfile(t *testing.T, organization string) (original, key string) {
	c := acctest.Client()
	profile, err := c.GetDefaultQualityProfile(t.Context(), organization, defaultQualityProfileLanguage)
	if err != nil {
		t.Fatalf("cannot read the default profile: %v", err)
	}
	original = profile.Key
	created, err := c.CreateQualityProfile(t.Context(), organization, defaultQualityProfileLanguage,
		fmt.Sprintf("%sdefault-profile-%d", acctest.KeyPrefix, rand.Uint32()))
	if err != nil {
		t.Fatalf("cannot make the quality profile: %v", err)
	}
	// The cleanups run in the opposite order, so the default changes before
	// the delete. The context of the test is canceled before the cleanups run.
	t.Cleanup(func() {
		if err := c.DeleteQualityProfile(context.Background(), created.Key); err != nil {
			t.Errorf("cannot delete the quality profile %s: %v", created.Key, err)
		}
	})
	t.Cleanup(func() {
		if err := c.SetDefaultQualityProfile(context.Background(), original); err != nil {
			t.Errorf("cannot set the original default quality profile %s again: %v", original, err)
		}
	})
	return original, created.Key
}

func testAccCheckDefaultQualityProfile(t *testing.T, organization, want string) resource.TestCheckFunc {
	return testAccCheckDefaultQualityProfileOf(t, organization, defaultQualityProfileLanguage, want)
}

// testAccCheckDefaultQualityProfileOf compares the default profile of the
// language in the API with want.
func testAccCheckDefaultQualityProfileOf(t *testing.T, organization, language, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		profile, err := acctest.Client().GetDefaultQualityProfile(t.Context(), organization, language)
		if err != nil {
			return err
		}
		if profile.Key != want {
			return fmt.Errorf("default %s profile = %q, want %q", language, profile.Key, want)
		}
		return nil
	}
}

func testAccDefaultQualityProfileConfig(organization, key string) string {
	return fmt.Sprintf(`
resource "sonarqube_cloud_organization_default_quality_profile" "test" {
  organization        = %q
  language            = %q
  quality_profile_key = %q
}
`, organization, defaultQualityProfileLanguage, key)
}
