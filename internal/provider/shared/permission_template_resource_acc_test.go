package shared_test

import (
	"cmp"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

const permissionTemplateAddress = "sonarqube_permission_template.test"

// The organization of SONARQUBE_TEST_ORGANIZATION must be on the Team or the
// Enterprise plan. On the Free plan, SonarQube Cloud refuses to create a
// template.
func TestAccPermissionTemplateResource(t *testing.T) {
	acctest.PreCheckAcceptance(t)
	organization := acctest.PreCheckSharedOrganization(t)
	login, err := acctest.Client().CurrentUserLogin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	suffix := rand.Uint32()
	p := permissionTemplates{t: t, organization: organization, login: login,
		group: fmt.Sprintf("%stemplate-%d", acctest.KeyPrefix, suffix), name: fmt.Sprintf("%stemplate-%d/first", acctest.KeyPrefix, suffix)}
	renamed := p.name + "-renamed"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			return errors.Join(p.gone(p.name), p.gone(renamed), p.groupGone())
		},
		Steps: []resource.TestStep{
			{
				Config: p.config(p.name, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(permissionTemplateAddress, "id"),
					resource.TestCheckResourceAttr(permissionTemplateAddress, "description", "Made by an acceptance test."),
					p.hasGrants(p.fullGrants()),
				),
			},
			{Config: p.config(p.name, true), PlanOnly: true},
			{ResourceName: permissionTemplateAddress, ImportState: true, ImportStateId: organization + "/" + p.name, ImportStateVerify: true},
			{
				// A grant change in the UI shows in the next plan, and the
				// apply sets the grants of the configuration again.
				PreConfig: func() { p.changeOutside() },
				Config:    p.config(p.name, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(permissionTemplateAddress, plancheck.ResourceActionUpdate)},
				},
				Check: p.hasGrants(p.fullGrants()),
			},
			{
				// A rename keeps the template, and the omitted values and
				// blocks are removed.
				Config: p.config(renamed, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(permissionTemplateAddress, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(permissionTemplateAddress, "description"),
					resource.TestCheckNoResourceAttr(permissionTemplateAddress, "project_key_pattern"),
					p.hasGrants([]client.TemplateGrant{{Kind: client.GranteeGroup, Name: p.group, Permission: "user"}}),
				),
			},
			{Config: p.config(renamed, false), PlanOnly: true},
		},
	})
}

// permissionTemplates builds the configurations and the checks of the test.
type permissionTemplates struct {
	t            *testing.T
	organization string
	login        string
	group        string
	name         string
}

// config makes a group and a template. With full set, the template has a
// description, a pattern, a user and the project creator, and the group has
// two permissions.
func (p permissionTemplates) config(name string, full bool) string {
	template := fmt.Sprintf(`resource "sonarqube_group" "test" {
  organization = %q
  name         = %q
}

resource "sonarqube_permission_template" "test" {
  organization = %q
  name         = %q
`, p.organization, p.group, p.organization, name)
	if !full {
		return template + `
  group {
    name        = sonarqube_group.test.name
    permissions = ["user"]
  }
}
`
	}
	return template + fmt.Sprintf(`  description                 = "Made by an acceptance test."
  project_key_pattern         = %q
  project_creator_permissions = ["admin"]

  group {
    name        = sonarqube_group.test.name
    permissions = ["user", "codeviewer"]
  }

  user {
    login       = %q
    permissions = ["issueadmin"]
  }
}
`, p.group+"-.*", p.login)
}

// fullGrants are the grants of config with full set.
func (p permissionTemplates) fullGrants() []client.TemplateGrant {
	return []client.TemplateGrant{
		{Kind: client.GranteeProjectCreator, Permission: "admin"},
		{Kind: client.GranteeGroup, Name: p.group, Permission: "codeviewer"},
		{Kind: client.GranteeGroup, Name: p.group, Permission: "user"},
		{Kind: client.GranteeUser, Name: p.login, Permission: "issueadmin"},
	}
}

// changeOutside removes a group grant and gives a grant to the Owners group,
// as a user in the UI could do.
func (p permissionTemplates) changeOutside() {
	c := acctest.Client()
	id, err := c.FindPermissionTemplateID(p.t.Context(), p.organization, p.name)
	if err != nil {
		p.t.Fatal(err)
	}
	if err := c.RemovePermissionTemplateGrant(p.t.Context(), p.organization, id,
		client.TemplateGrant{Kind: client.GranteeGroup, Name: p.group, Permission: "codeviewer"}); err != nil {
		p.t.Fatal(err)
	}
	if err := c.AddPermissionTemplateGrant(p.t.Context(), p.organization, id,
		client.TemplateGrant{Kind: client.GranteeGroup, Name: "Owners", Permission: "scan"}); err != nil {
		p.t.Fatal(err)
	}
}

// hasGrants checks that the grants on the server are want, in any order.
func (p permissionTemplates) hasGrants(want []client.TemplateGrant) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		id := state.RootModule().Resources[permissionTemplateAddress].Primary.ID
		template, err := acctest.Client().GetPermissionTemplate(p.t.Context(), p.organization, id)
		if err != nil {
			return err
		}
		if !slices.Equal(sortedTemplateGrants(template.Grants), sortedTemplateGrants(want)) {
			return fmt.Errorf("grants = %v, want %v", template.Grants, want)
		}
		return nil
	}
}

func sortedTemplateGrants(grants []client.TemplateGrant) []client.TemplateGrant {
	return slices.SortedFunc(slices.Values(grants), func(a, b client.TemplateGrant) int {
		return cmp.Or(strings.Compare(a.Kind, b.Kind), strings.Compare(a.Name, b.Name), strings.Compare(a.Permission, b.Permission))
	})
}

func (p permissionTemplates) gone(name string) error {
	_, err := acctest.Client().FindPermissionTemplateID(p.t.Context(), p.organization, name)
	if errors.Is(err, client.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("the permission template %q still exists", name)
}

// groupGone waits until the group list does not show the group, because the
// list can show a deleted group for some seconds.
func (p permissionTemplates) groupGone() error {
	return acctest.WaitFor(func() (bool, error) {
		_, err := acctest.Client().FindGroupByName(p.t.Context(), p.organization, p.group)
		if errors.Is(err, client.ErrNotFound) {
			return true, nil
		}
		return false, err
	})
}
