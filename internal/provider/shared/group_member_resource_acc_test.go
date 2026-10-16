package shared_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

func TestAccGroupMemberResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC is not set")
	}
	organization := acctest.PreCheckSharedOrganization(t)
	login, err := acctest.Client().CurrentUserLogin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The slash checks that the import ID keeps a group name with "/".
	first := fmt.Sprintf("%smember-%d/first", acctest.KeyPrefix, rand.Uint32())
	second := first + "-second"
	m := groupMembers{t: t, organization: organization, login: login}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			return errors.Join(m.goneFrom(first), m.goneFrom(second))
		},
		Steps: []resource.TestStep{
			{Config: m.config(first, second, true), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("sonarqube_group_member.first", "id", organization+"/"+first+"/"+login),
				m.isMember(first, true),
				m.isMember(second, true),
			)},
			{Config: m.config(first, second, true), PlanOnly: true},
			{ResourceName: "sonarqube_group_member.first", ImportState: true,
				ImportStateId: organization + "/" + first + "/" + login, ImportStateVerify: true},
			{Config: m.config(first, second, true), PreConfig: func() {
				if err := acctest.Client().RemoveGroupMember(t.Context(), organization, first, login); err != nil {
					t.Fatal(err)
				}
				if err := m.isMember(first, false)(nil); err != nil {
					t.Fatal(err)
				}
			}, PlanOnly: true, ExpectNonEmptyPlan: true},
			{Config: m.config(first, second, true), Check: m.isMember(first, true)},
			// A destroy of one membership leaves the other one.
			{Config: m.config(first, second, false), Check: resource.ComposeAggregateTestCheckFunc(
				m.isMember(first, false),
				m.isMember(second, true),
			)},
			// The groups stay, so that the check sees the destroy of the last
			// membership and not the destroy of its group.
			{Config: m.group("first", first) + m.group("second", second), Check: m.isMember(second, false)},
		},
	})
}

// groupMembers builds the configurations and the checks of the token user's
// memberships.
type groupMembers struct {
	t            *testing.T
	organization string
	login        string
}

// config makes two groups. It adds the user to the second group, and to the
// first one when withFirst is true.
func (m groupMembers) config(first, second string, withFirst bool) string {
	config := m.group("first", first) + m.group("second", second) + m.member("second")
	if withFirst {
		config += m.member("first")
	}
	return config
}

func (m groupMembers) group(address, name string) string {
	return fmt.Sprintf(`resource "sonarqube_group" %q {
  organization = %q
  name         = %q
}
`, address, m.organization, name)
}

func (m groupMembers) member(group string) string {
	return fmt.Sprintf(`resource "sonarqube_group_member" %q {
  organization = %q
  group_name   = sonarqube_group.%s.name
  login        = %q
}
`, group, m.organization, group, m.login)
}

// isMember waits until the member list shows the membership as want. The list
// can show the old members for some seconds after a change.
func (m groupMembers) isMember(group string, want bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		return acctest.WaitFor(func() (bool, error) {
			member, err := acctest.Client().IsGroupMember(m.t.Context(), m.organization, group, m.login)
			return err == nil && member == want, err
		})
	}
}

// goneFrom waits until the user is not in the group. A deleted group holds
// nobody.
func (m groupMembers) goneFrom(group string) error {
	return acctest.WaitFor(func() (bool, error) {
		member, err := acctest.Client().IsGroupMember(m.t.Context(), m.organization, group, m.login)
		if errors.Is(err, client.ErrNotFound) {
			return true, nil
		}
		return err == nil && !member, err
	})
}
