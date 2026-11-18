package cloud_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// TestAccGroupChangeThenMemberAndPermissions adds a member and permissions to
// a group in the apply that replaces the group with the same name, and in
// the apply that restores a name changed outside Terraform.
func TestAccGroupChangeThenMemberAndPermissions(t *testing.T) {
	acctest.SkipWithSOT(t, acctest.SOTNoUser)
	acctest.PreCheckAcceptance(t)
	organization := acctest.PreCheckSharedOrganization(t)
	api := acctest.Client()
	login, err := api.CurrentUserLogin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("%sgroup-change-%d", acctest.KeyPrefix, rand.Uint32())
	scope := client.PermissionScope{Organization: organization}
	group := client.PermissionPrincipal{Group: true, Name: name}
	want := []string{"provisioning", "scan"}
	// config replaces the group, its member and its permissions when cycle
	// changes. Terraform deletes them and then creates them again, so the
	// create uses a group name that was deleted a moment before.
	config := func(cycle int) string {
		return fmt.Sprintf(`resource "terraform_data" "cycle" {
  input = %d
}
resource "sonarqube_group" "test" {
  organization = %[2]q
  name         = %[3]q
  lifecycle {
    replace_triggered_by = [terraform_data.cycle]
  }
}
resource "sonarqube_group_member" "test" {
  organization = %[2]q
  group_name   = sonarqube_group.test.name
  login        = %[4]q
  lifecycle {
    replace_triggered_by = [terraform_data.cycle]
  }
}
resource "sonarqube_cloud_organization_permissions" "test" {
  organization = %[2]q
  group_name   = sonarqube_group.test.name
  permissions  = ["scan", "provisioning"]
  lifecycle {
    replace_triggered_by = [terraform_data.cycle]
  }
}`, cycle, organization, name, login)
	}
	check := func(*terraform.State) error {
		// The lists can show the old group for some seconds after a change.
		if err := acctest.WaitFor(func() (bool, error) {
			return checkPermissions(t, api, scope, group, want) == nil, nil
		}); err != nil {
			return checkPermissions(t, api, scope, group, want)
		}
		return acctest.WaitFor(func() (bool, error) {
			return api.IsGroupMember(t.Context(), organization, name, login)
		})
	}
	renameOutside := func() {
		found, err := api.FindGroupByName(t.Context(), organization, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := api.UpdateGroup(t.Context(), found.ID, name+"-ui", ""); err != nil {
			t.Fatal(err)
		}
		// The refresh must see the new name, so that the plan restores it.
		if err := acctest.WaitForSteady(func() (bool, error) {
			read, err := api.GetGroup(t.Context(), organization, found.ID)
			return err == nil && read.Name == name+"-ui", err
		}, 5); err != nil {
			t.Fatal(err)
		}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			return errors.Join(
				checkPermissions(t, api, scope, group, nil),
				acctest.WaitFor(func() (bool, error) {
					member, err := api.IsGroupMember(t.Context(), organization, name, login)
					if errors.Is(err, client.ErrNotFound) {
						return true, nil
					}
					return err == nil && !member, err
				}),
				acctest.WaitForGone(func() (*client.Group, error) { return api.FindGroupByName(t.Context(), organization, name) }),
				acctest.WaitForGone(func() (*client.Group, error) {
					return api.FindGroupByName(t.Context(), organization, name+"-ui")
				}),
			)
		},
		Steps: []resource.TestStep{
			{Config: config(0), Check: check},
			{Config: config(1), Check: check},
			{Config: config(2), Check: check},
			{Config: config(3), Check: check},
			{Config: config(3), PreConfig: renameOutside, Check: check},
		},
	})
}
