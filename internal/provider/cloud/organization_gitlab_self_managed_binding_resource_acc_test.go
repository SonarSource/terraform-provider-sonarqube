package cloud_test

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

const gitlabBindingResource = "sonarqube_cloud_organization_gitlab_self_managed_binding.test"

// The test makes an organization, attaches it to the enterprise, binds it to
// the GitLab group, and deletes the organization, which is the only thing
// that removes a binding. Its name starts with TestAccGitLabSelfManaged,
// because CI runs these tests in a job of their own, next to the instance
// that the job starts.
func TestAccGitLabSelfManagedBindingResource(t *testing.T) {
	acctest.SkipWithSOT(t, acctest.SOTRefused)
	// The steps below are built before resource.Test runs a PreCheck, so the
	// values are read here.
	gitlab := acctest.PreCheckGitLabSelfManaged(t)
	key := fmt.Sprintf("%s%d", acctest.KeyPrefix, rand.Uint32())
	// The first step records the identifier of the binding for the check
	// after the destroy.
	var bindingID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// The destroy of the binding only removes it from the state. The
		// binding goes with the organization. No call can show the GitLab
		// configuration of a deleted organization: the search answers 403
		// "User is not a member of organization".
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			acctest.CheckOrganizationsDeleted(t, key),
			acctest.CheckGone("binding of "+key, func() (*client.OrganizationBinding, error) {
				return acctest.Client().GetOrganizationBinding(t.Context(), bindingID)
			}),
		),
		Steps: []resource.TestStep{
			{
				// resource.Test plans again after this apply and fails when
				// that plan is not empty. The read reports the group by its
				// number, so this checks that the state keeps the path.
				Config: testAccGitLabBindingConfig(key, gitlab, gitlab.Token, gitlab.Group),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gitlabBindingResource, "group", gitlab.Group),
					resource.TestCheckResourceAttr(gitlabBindingResource, "server_url", gitlab.URL),
					// The default of api_url, which the configuration does not set.
					resource.TestCheckResourceAttr(gitlabBindingResource, "api_url", gitlab.URL+"/api/v4"),
					resource.TestCheckResourceAttrWith(gitlabBindingResource, "id", func(value string) error {
						bindingID = value
						return nil
					}),
					resource.TestCheckResourceAttrSet(gitlabBindingResource, "configuration_id"),
					resource.TestMatchResourceAttr(gitlabBindingResource, "group_id", regexp.MustCompile(`^\d+$`)),
					resource.TestCheckResourceAttr(gitlabBindingResource, "dev_ops_platform_url",
						gitlab.URL+"/groups/"+gitlab.Group),
				),
			},
			{
				// The API never returns the token, so the import cannot know
				// it.
				ResourceName:            gitlabBindingResource,
				ImportState:             true,
				ImportStateId:           key,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"access_token"},
			},
			{
				// The group cannot change, and a replacement does not help.
				// The plan must say so.
				Config:      testAccGitLabBindingConfig(key, gitlab, gitlab.Token, gitlab.Group+"-other"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`The group of a binding cannot change`),
			},
			{
				// A new token goes to the binding, and the binding stays.
				Config: testAccGitLabBindingConfig(key, gitlab, gitlab.SecondToken, gitlab.Group),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gitlabBindingResource, plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue(gitlabBindingResource, tfjsonpath.New("id"), knownvalue.NotNull()),
					},
				},
			},
			{
				// The server checks a new token against GitLab before it
				// writes it.
				Config:      testAccGitLabBindingConfig(key, gitlab, "glpat-not-a-valid-token", gitlab.Group),
				ExpectError: regexp.MustCompile(`The GitLab access token is not valid`),
			},
			{
				// A valid token again, so that the destroy starts from a
				// clean state.
				Config: testAccGitLabBindingConfig(key, gitlab, gitlab.Token, gitlab.Group),
			},
		},
	})
}

// testAccGitLabBindingConfig makes the organization, attaches it to the
// enterprise, and binds it. The destroy of the organization is what removes
// the binding, so they go together. The attachment is destroyed before the
// organization and detaches it.
func testAccGitLabBindingConfig(key string, gitlab acctest.GitLabSelfManaged, token, group string) string {
	return fmt.Sprintf(`
resource "sonarqube_cloud_organization" "test" {
  key  = %[1]q
  name = "Terraform acceptance test"
}

data "sonarqube_cloud_enterprise" "test" {
  key = %[2]q
}

resource "sonarqube_cloud_enterprise_organization" "test" {
  enterprise_id = data.sonarqube_cloud_enterprise.test.id
  organization  = sonarqube_cloud_organization.test.key
}

resource "sonarqube_cloud_organization_gitlab_self_managed_binding" "test" {
  organization_key = sonarqube_cloud_enterprise_organization.test.organization
  server_url       = %[3]q
  group            = %[4]q
  access_token     = %[5]q
}
`, key, gitlab.EnterpriseKey, gitlab.URL, group, token)
}
