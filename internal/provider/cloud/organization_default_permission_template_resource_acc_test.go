package cloud_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

const (
	defaultTemplateAddress = "sonarqube_cloud_organization_default_permission_template.test"
	defaultTemplateOfTest  = "sonarqube_permission_template.test"
)

// The organization of SONARQUBE_TEST_ORGANIZATION must be on the Team or the
// Enterprise plan, which can create a template.
//
// The destroy does not change the default, and SonarQube Cloud refuses to
// delete the default template. So the last step sets the original default
// again before the destroy of the template. After a failed step, the cleanup
// does the same, then deletes the template.
func TestAccOrganizationDefaultPermissionTemplateResource(t *testing.T) {
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("%s is not set", resource.EnvTfAcc)
	}
	acctest.PreCheck(t)
	acctest.PreCheckDestructive(t)
	organization := os.Getenv(acctest.EnvTestOrganization)
	suffix := rand.Uint32()
	d := defaultTemplates{t: t, organization: organization,
		group:    fmt.Sprintf("terraform-default-template-%d", suffix),
		template: fmt.Sprintf("terraform-default-template-%d", suffix),
		project:  fmt.Sprintf("%s_terraform-default-template-%d", organization, suffix)}
	d.original = d.prepare()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			return errors.Join(d.checkDefault(d.original), d.templateGone(), d.projectGone(), d.groupGone())
		},
		Steps: []resource.TestStep{
			{
				Config: d.config(true, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(defaultTemplateAddress, "id", organization),
					resource.TestCheckResourceAttrPair(defaultTemplateAddress, "template_id", defaultTemplateOfTest, "id"),
					d.checkDefaultIsTest(),
				),
			},
			{Config: d.config(true, false), PlanOnly: true},
			{ResourceName: defaultTemplateAddress, ImportState: true, ImportStateId: organization, ImportStateVerify: true},
			{
				// A change of the default in the UI shows in the plan, and
				// the apply sets the template of the configuration again.
				PreConfig: func() { d.setDefault(t.Context(), d.original) },
				Config:    d.config(true, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(defaultTemplateAddress, plancheck.ResourceActionUpdate)},
				},
				Check: d.checkDefaultIsTest(),
			},
			{
				// A new project gets the grants of the default template.
				Config: d.config(true, true),
				Check:  d.checkProjectGrants(),
			},
			{
				// The destroy of the default leaves the default as it is, so
				// the original default is set first, and the template can go
				// at the end.
				PreConfig: func() { d.setDefault(t.Context(), d.original) },
				Config:    d.config(false, true),
				Check:     func(*terraform.State) error { return d.checkDefault(d.original) },
			},
		},
	})
}

// defaultTemplates builds the configurations and the checks of the test.
type defaultTemplates struct {
	t            *testing.T
	organization string
	group        string
	template     string
	project      string
	original     string
}

// prepare reads the original default and registers the cleanup, which sets
// it again and then deletes what a failed step left. The cleanups run in the
// opposite order. The context of the test is canceled before they run.
//
// While the template is the default, the destroy cannot delete it, so it
// does not delete the group either, because the template uses the group.
func (d defaultTemplates) prepare() string {
	original, err := acctest.Client().GetDefaultPermissionTemplateID(d.t.Context(), d.organization)
	if err != nil {
		d.t.Fatalf("cannot read the default permission template: %v", err)
	}
	d.t.Cleanup(d.deleteLeftovers)
	d.t.Cleanup(func() { d.setDefault(context.Background(), original) })
	return original
}

// deleteLeftovers deletes the template, then the group, when a failed step
// left them.
func (d defaultTemplates) deleteLeftovers() {
	ctx, c := context.Background(), acctest.Client()
	id, err := c.FindPermissionTemplateID(ctx, d.organization, d.template)
	if err == nil {
		err = c.DeletePermissionTemplate(ctx, d.organization, id)
	}
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		d.t.Errorf("cannot delete the permission template %s: %v", d.template, err)
	}
	group, err := c.FindGroupByName(ctx, d.organization, d.group)
	if err == nil {
		err = c.DeleteGroup(ctx, group.ID)
	}
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		d.t.Errorf("cannot delete the group %s: %v", d.group, err)
	}
}

func (d defaultTemplates) setDefault(ctx context.Context, id string) {
	if err := acctest.Client().SetDefaultPermissionTemplate(ctx, d.organization, id); err != nil {
		d.t.Errorf("cannot set the default permission template %s: %v", id, err)
	}
}

// config makes a group and a template. The template gives the group browse
// and the project creator admin. withDefault adds the default, and
// withProject adds a project that is made after the default.
func (d defaultTemplates) config(withDefault, withProject bool) string {
	config := fmt.Sprintf(`resource "sonarqube_group" "test" {
  organization = %[1]q
  name         = %[2]q
}

resource "sonarqube_permission_template" "test" {
  organization                = %[1]q
  name                        = %[3]q
  project_creator_permissions = ["admin"]

  group {
    name        = sonarqube_group.test.name
    permissions = ["user", "codeviewer"]
  }
}
`, d.organization, d.group, d.template)
	if withDefault {
		config += fmt.Sprintf(`
resource "sonarqube_cloud_organization_default_permission_template" "test" {
  organization = %q
  template_id  = sonarqube_permission_template.test.id
}
`, d.organization)
	}
	if withProject {
		dependsOn := ""
		if withDefault {
			dependsOn = "\n  depends_on   = [sonarqube_cloud_organization_default_permission_template.test]"
		}
		config += fmt.Sprintf(`
resource "sonarqube_project" "test" {
  organization = %q
  key          = %q
  name         = "Terraform default template"%s
}
`, d.organization, d.project, dependsOn)
	}
	return config
}

func (d defaultTemplates) checkDefault(want string) error {
	got, err := acctest.Client().GetDefaultPermissionTemplateID(d.t.Context(), d.organization)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("default permission template = %q, want %q", got, want)
	}
	return nil
}

func (d defaultTemplates) checkDefaultIsTest() resource.TestCheckFunc {
	return func(state *terraform.State) error {
		return d.checkDefault(state.RootModule().Resources[defaultTemplateOfTest].Primary.ID)
	}
}

// checkProjectGrants checks that the new project has the grants of the
// template, for the group and for the token user, who made the project.
func (d defaultTemplates) checkProjectGrants() resource.TestCheckFunc {
	return func(*terraform.State) error {
		c := acctest.Client()
		scope := client.PermissionScope{Organization: d.organization, ProjectKey: d.project}
		groupPermissions, err := c.GetPermissions(d.t.Context(), scope, client.PermissionPrincipal{Group: true, Name: d.group})
		if err != nil {
			return err
		}
		login, err := c.CurrentUserLogin(d.t.Context())
		if err != nil {
			return err
		}
		userPermissions, err := c.GetPermissions(d.t.Context(), scope, client.PermissionPrincipal{Name: login})
		if err != nil {
			return err
		}
		slices.Sort(groupPermissions)
		if !slices.Equal(groupPermissions, []string{"codeviewer", "user"}) || !slices.Contains(userPermissions, "admin") {
			return fmt.Errorf("project grants: group %v, creator %v", groupPermissions, userPermissions)
		}
		return nil
	}
}

func (d defaultTemplates) templateGone() error {
	_, err := acctest.Client().FindPermissionTemplateID(d.t.Context(), d.organization, d.template)
	if errors.Is(err, client.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("the permission template %q still exists", d.template)
}

func (d defaultTemplates) projectGone() error {
	_, err := acctest.Client().GetProject(d.t.Context(), d.organization, d.project)
	if errors.Is(err, client.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("the project %q still exists", d.project)
}

// groupGone waits until the group list does not show the group, because the
// list can show a deleted group for some seconds.
func (d defaultTemplates) groupGone() error {
	return acctest.WaitFor(func() (bool, error) {
		_, err := acctest.Client().FindGroupByName(d.t.Context(), d.organization, d.group)
		if errors.Is(err, client.ErrNotFound) {
			return true, nil
		}
		return false, err
	})
}
