package cloud_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

func TestAccOrganizationPermissionsGroupResource(t *testing.T) {
	organization := os.Getenv(acctest.EnvTestOrganization)
	name := fmt.Sprintf("%spermissions-%d", acctest.KeyPrefix, rand.Uint32())
	address := "sonarqube_cloud_organization_permissions.test"
	api := acctest.Client()
	config := func(permissions string) string {
		return fmt.Sprintf(`resource "sonarqube_group" "test" {
  organization = %q
  name         = %q
}
resource "sonarqube_cloud_organization_permissions" "test" {
  organization = %q
  group_name   = sonarqube_group.test.name
  permissions  = %s
}`, organization, name, organization, permissions)
	}
	scope := client.PermissionScope{Organization: organization}
	group := client.PermissionPrincipal{Group: true, Name: name}
	owners := client.PermissionPrincipal{Group: true, Name: "Owners"}
	check := func(want []string) resource.TestCheckFunc {
		return func(*terraform.State) error { return checkPermissions(t, api, scope, group, want) }
	}
	var ownerPermissions []string
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheckDestructive(t)
			if organization == "" {
				t.Skipf("%s is not set", acctest.EnvTestOrganization)
			}
			permissions, err := api.GetPermissions(t.Context(), scope, owners)
			if err != nil {
				t.Fatal(err)
			}
			ownerPermissions = permissions
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if err := checkPermissions(t, api, scope, group, nil); err != nil {
				return err
			}
			return checkPermissions(t, api, scope, owners, ownerPermissions)
		},
		Steps: []resource.TestStep{
			{Config: config(`["scan"]`), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "permissions.#", "1"), check([]string{"scan"}),
			)},
			{Config: config(`["scan"]`)},
			{Config: config(`["scan", "gateadmin"]`), Check: check([]string{"scan", "gateadmin"})},
			{Config: config(`["scan"]`), Check: check([]string{"scan"})},
			{Config: config(`["scan"]`), PreConfig: func() {
				if err := api.AddPermission(t.Context(), scope, group, "gateadmin"); err != nil {
					t.Fatal(err)
				}
			}, PlanOnly: true, ExpectNonEmptyPlan: true},
			{Config: config(`["scan"]`), Check: check([]string{"scan"})},
			{ResourceName: address, ImportState: true, ImportStateId: organization + "/group/" + name, ImportStateVerify: true},
		},
	})
}

func TestAccOrganizationPermissionsUserResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC is not set")
	}
	acctest.PreCheckDestructive(t)
	organization := os.Getenv(acctest.EnvTestOrganization)
	if organization == "" {
		t.Skipf("%s is not set", acctest.EnvTestOrganization)
	}
	api := acctest.Client()
	login, err := api.CurrentUserLogin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	scope := client.PermissionScope{Organization: organization}
	user := client.PermissionPrincipal{Name: login}
	permissions, err := api.GetPermissions(t.Context(), scope, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(permissions) != 0 {
		t.Skip("the token user already has direct organization permissions")
	}
	config := fmt.Sprintf(`resource "sonarqube_cloud_organization_permissions" "test" {
  organization = %q
  login        = %q
  permissions  = ["scan"]
}`, organization, login)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheckDestructive(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			return checkPermissions(t, api, scope, user, nil)
		},
		Steps: []resource.TestStep{
			{Config: config, Check: func(*terraform.State) error {
				return checkPermissions(t, api, scope, user, []string{"scan"})
			}},
			{Config: config},
			{ResourceName: "sonarqube_cloud_organization_permissions.test", ImportState: true,
				ImportStateId: organization + "/user/" + login, ImportStateVerify: true},
		},
	})
}

// checkPermissions reads once: a permission change shows in the next read.
// A missing group counts as a group without permissions.
func checkPermissions(t *testing.T, api *client.Client, scope client.PermissionScope, principal client.PermissionPrincipal, want []string) error {
	t.Helper()
	got, err := api.GetPermissions(t.Context(), scope, principal)
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		return err
	}
	left, right := slices.Clone(got), slices.Clone(want)
	slices.Sort(left)
	slices.Sort(right)
	if !slices.Equal(left, right) {
		return fmt.Errorf("permissions of %q = %v, want %v", principal.Name, got, want)
	}
	return nil
}
