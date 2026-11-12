package cloud_test

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
)

// The test binds a project of an organization that is bound to GitLab
// Self-Managed, first to a repository of the group, then to a repository of a
// subgroup. The repositories are private. The project is private as well,
// because an organization on the Enterprise plan makes private projects. Its
// name starts with TestAccGitLabSelfManaged, because CI runs these tests in a
// job of their own, next to the instance that the job starts.
func TestAccGitLabSelfManagedProjectBindingResource(t *testing.T) {
	acctest.SkipWithSOT(t, acctest.SOTRefused)
	// The steps below are built before resource.Test runs a PreCheck, so the
	// values are read here.
	gitlab := acctest.PreCheckGitLabSelfManaged(t)
	key := fmt.Sprintf("%s%d", acctest.KeyPrefix, rand.Uint32())
	projectKey := key + "_my-project"
	repositoryID := tfjsonpath.New("repository_id")
	// A change of case keeps the repository, and a change of path does not.
	sameRepository := statecheck.CompareValue(compare.ValuesSame())
	otherRepository := statecheck.CompareValue(compare.ValuesDiffer())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// The destroy of a binding only removes it from the state. The
		// bindings are gone when the organization is gone.
		CheckDestroy: acctest.CheckOrganizationsDeleted(t, key),
		Steps: []resource.TestStep{
			{
				// resource.Test plans again after this apply and fails when
				// that plan is not empty.
				Config: testAccGitLabProjectBindingConfig(key, gitlab, gitlab.Repository),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectBindingAddress, "repository", gitlab.Repository),
					resource.TestCheckResourceAttrSet(projectBindingAddress, "id"),
					resource.TestMatchResourceAttr(projectBindingAddress, "repository_id", regexp.MustCompile(`^\d+$`)),
					resource.TestCheckResourceAttr(projectBindingDataAddress, "repository", gitlab.Repository),
					resource.TestCheckResourceAttrPair(
						projectBindingDataAddress, "repository_id", projectBindingAddress, "repository_id"),
				),
				ConfigStateChecks: []statecheck.StateCheck{
					sameRepository.AddStateValue(projectBindingAddress, repositoryID),
				},
			},
			{
				// GitLab ignores the case of a path.
				Config: testAccGitLabProjectBindingConfig(key, gitlab, strings.ToUpper(gitlab.Repository)),
				ConfigStateChecks: []statecheck.StateCheck{
					sameRepository.AddStateValue(projectBindingAddress, repositoryID),
					otherRepository.AddStateValue(projectBindingAddress, repositoryID),
				},
			},
			{
				// A path with a subgroup. The change keeps the binding.
				Config: testAccGitLabProjectBindingConfig(key, gitlab, gitlab.SubgroupRepository),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(projectBindingAddress, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectBindingAddress, "repository", gitlab.SubgroupRepository),
					resource.TestCheckResourceAttr(projectBindingDataAddress, "repository", gitlab.SubgroupRepository),
				),
				ConfigStateChecks: []statecheck.StateCheck{
					otherRepository.AddStateValue(projectBindingAddress, repositoryID),
				},
			},
			{
				ResourceName:      projectBindingAddress,
				ImportState:       true,
				ImportStateId:     key + "/" + projectKey,
				ImportStateVerify: true,
			},
			{
				// A destroy of the binding keeps the project, which stays
				// bound to the repository.
				Config: testAccGitLabProjectConfig(key, gitlab),
				Check: func(*terraform.State) error {
					project, err := acctest.Client().GetProject(t.Context(), key, projectKey)
					if err != nil {
						return fmt.Errorf("the project after the destroy of its binding: %w", err)
					}
					binding, err := acctest.Client().FindProjectBinding(t.Context(), project)
					if err != nil {
						return fmt.Errorf("the binding after its destroy: %w", err)
					}
					if !strings.EqualFold(binding.Slug, gitlab.SubgroupRepository) {
						return fmt.Errorf("the project is bound to %s, want %s", binding.Slug, gitlab.SubgroupRepository)
					}
					return nil
				},
			},
		},
	})
}

// testAccGitLabProjectConfig adds a project to the configuration of the
// organization binding.
func testAccGitLabProjectConfig(key string, gitlab acctest.GitLabSelfManaged) string {
	return testAccGitLabBindingConfig(key, gitlab, gitlab.Token, gitlab.Group) + fmt.Sprintf(`
resource "sonarqube_project" "test" {
  # The project can bind only after its organization is bound.
  organization = sonarqube_cloud_organization_gitlab_self_managed_binding.test.organization_key
  key          = "%[1]s_my-project"
  name         = "Terraform acceptance test"
}
`, key)
}

// testAccGitLabProjectBindingConfig binds the project to the repository.
func testAccGitLabProjectBindingConfig(key string, gitlab acctest.GitLabSelfManaged, repository string) string {
	return testAccGitLabProjectConfig(key, gitlab) + fmt.Sprintf(`
resource "sonarqube_cloud_project_binding" "test" {
  organization = sonarqube_project.test.organization
  project_key  = sonarqube_project.test.key
  repository   = %[1]q
}

data "sonarqube_cloud_project_binding" "test" {
  organization = sonarqube_cloud_project_binding.test.organization
  project_key  = sonarqube_cloud_project_binding.test.project_key
}
`, repository)
}
