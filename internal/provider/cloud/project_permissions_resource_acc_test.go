package cloud_test

import (
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

func testAccProjectPermissionsBase(organization, projectKey, group string) string {
	return fmt.Sprintf(`resource "sonarqube_project" "test" {
  organization = %[1]q
  key          = %[2]q
  name         = "Terraform acceptance test"
}
resource "sonarqube_group" "test" {
  organization = %[1]q
  name         = %[3]q
}
`, organization, projectKey, group)
}

func TestAccProjectPermissionsGroupResource(t *testing.T) {
	organization := os.Getenv(acctest.EnvTestOrganization)
	suffix := rand.Uint32()
	projectKey := fmt.Sprintf("%spermissions-%d", acctest.KeyPrefix, suffix)
	name := fmt.Sprintf("%sproject-permissions-%d", acctest.KeyPrefix, suffix)
	address := "sonarqube_cloud_project_permissions.test"
	api := acctest.Client()
	base := testAccProjectPermissionsBase(organization, projectKey, name)
	config := func(permissions string) string {
		return base + fmt.Sprintf(`resource "sonarqube_cloud_project_permissions" "test" {
  organization = %q
  project_key  = sonarqube_project.test.key
  group_name   = sonarqube_group.test.name
  permissions  = %s
}`, organization, permissions)
	}
	scope := client.PermissionScope{Organization: organization, ProjectKey: projectKey}
	group := client.PermissionPrincipal{Group: true, Name: name}
	owners := client.PermissionPrincipal{Group: true, Name: "Owners"}
	check := func(want []string) resource.TestCheckFunc {
		return func(*terraform.State) error { return checkPermissions(t, api, scope, group, want) }
	}
	var ownerPermissions []string
	readOwners := func(*terraform.State) error {
		permissions, err := api.GetPermissions(t.Context(), scope, owners)
		ownerPermissions = permissions
		return err
	}
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheckSharedOrganization(t)
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckProjectDeleted(t, organization, projectKey),
		Steps: []resource.TestStep{
			{Config: config(`["issueadmin", "user"]`), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "id", organization+"/"+projectKey+"/group/"+name),
				resource.TestCheckResourceAttr(address, "permissions.#", "2"),
				check([]string{"issueadmin", "user"}), readOwners,
			)},
			{Config: config(`["issueadmin", "user"]`)},
			{Config: config(`["issueadmin", "user", "codeviewer"]`), Check: check([]string{"issueadmin", "user", "codeviewer"})},
			{Config: config(`["issueadmin", "user", "codeviewer"]`), PreConfig: func() {
				if err := api.RemovePermission(t.Context(), scope, group, "codeviewer"); err != nil {
					t.Fatal(err)
				}
			}, PlanOnly: true, ExpectNonEmptyPlan: true},
			{Config: config(`["issueadmin"]`), Check: check([]string{"issueadmin"})},
			{ResourceName: address, ImportState: true, ImportStateId: organization + "/" + projectKey + "/group/" + name, ImportStateVerify: true},
			acctest.ImportError(address, organization+"/group/"+name,
				regexp.MustCompile(`Invalid project permission import ID`)),
			acctest.ImportError(address, organization+"/"+acctest.MissingKey+"/group/"+name, acctest.MissingImport),
			{Config: base, Check: resource.ComposeAggregateTestCheckFunc(
				check(nil),
				func(*terraform.State) error { return checkPermissions(t, api, scope, owners, ownerPermissions) },
			)},
		},
	})
}

func TestAccProjectPermissionsUserResource(t *testing.T) {
	acctest.PreCheckAcceptance(t)
	organization := acctest.PreCheckSharedOrganization(t)
	api := acctest.Client()
	login, err := api.CurrentUserLogin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	projectKey := fmt.Sprintf("%spermissions-user-%d", acctest.KeyPrefix, rand.Uint32())
	address := "sonarqube_cloud_project_permissions.test"
	scope := client.PermissionScope{Organization: organization, ProjectKey: projectKey}
	user := client.PermissionPrincipal{Name: login}
	project := fmt.Sprintf(`resource "sonarqube_project" "test" {
  organization = %q
  key          = %q
  name         = "Terraform acceptance test"
}
`, organization, projectKey)
	config := project + fmt.Sprintf(`resource "sonarqube_cloud_project_permissions" "test" {
  organization = %q
  project_key  = sonarqube_project.test.key
  login        = %q
  permissions  = ["issueadmin", "securityhotspotadmin"]
}`, organization, login)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheckDestructive(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckProjectDeleted(t, organization, projectKey),
		Steps: []resource.TestStep{
			{Config: config, Check: func(*terraform.State) error {
				return checkPermissions(t, api, scope, user, []string{"issueadmin", "securityhotspotadmin"})
			}},
			{ResourceName: address, ImportState: true, ImportStateId: organization + "/" + projectKey + "/user/" + login, ImportStateVerify: true},
			{Config: project, Check: func(*terraform.State) error {
				return checkPermissions(t, api, scope, user, nil)
			}},
		},
	})
}
