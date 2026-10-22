package cloud

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/validate"
)

var (
	_ resource.Resource                     = &projectPermissionsResource{}
	_ resource.ResourceWithConfigure        = &projectPermissionsResource{}
	_ resource.ResourceWithImportState      = &projectPermissionsResource{}
	_ resource.ResourceWithConfigValidators = &projectPermissionsResource{}
)

var projectPermissionValues = []string{
	"admin", "codeviewer", "issueadmin", "securityhotspotadmin", "architectureadmin", "scan", "user",
}

// publicProjectPermissions are given to all users on a public project. The
// server answers success to a grant of them there, but does not store it.
var publicProjectPermissions = []string{"user", "codeviewer"}

// NewProjectPermissionsResource returns the Cloud project permission resource.
func NewProjectPermissionsResource() resource.Resource {
	return &projectPermissionsResource{permissionsResource[projectPermissionsModel, *projectPermissionsModel]{
		scopeName:       "project",
		scopeAttributes: []string{"organization", "project_key"},
		typeSuffix:      "_cloud_project_permissions",
		checkApply:      checkPublicProjectPermissions,
	}}
}

type projectPermissionsResource struct {
	permissionsResource[projectPermissionsModel, *projectPermissionsModel]
}

type projectPermissionsModel struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	ProjectKey   types.String `tfsdk:"project_key"`
	GroupName    types.String `tfsdk:"group_name"`
	Login        types.String `tfsdk:"login"`
	Permissions  types.Set    `tfsdk:"permissions"`
}

func (m *projectPermissionsModel) scope() client.PermissionScope {
	return client.PermissionScope{Organization: m.Organization.ValueString(), ProjectKey: m.ProjectKey.ValueString()}
}

func (m *projectPermissionsModel) principal() client.PermissionPrincipal {
	return permissionPrincipal(m.GroupName, m.Login)
}

func (m *projectPermissionsModel) permissions() types.Set { return m.Permissions }

func (m *projectPermissionsModel) setID(id string) { m.ID = types.StringValue(id) }

func (m *projectPermissionsModel) setPermissions(permissions types.Set) {
	m.Permissions = permissions
}

func (r *projectPermissionsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := permissionPrincipalAttributes()
	maps.Copy(attributes, map[string]schema.Attribute{
		"organization": schema.StringAttribute{Required: true, Description: "Organization key.",
			Validators:    validate.OrganizationKey(),
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"project_key": schema.StringAttribute{Required: true, Description: "Project key.",
			Validators:    validate.ProjectKey(),
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"permissions": schema.SetAttribute{Required: true, ElementType: types.StringType,
			Description: "Complete nonempty set of project permissions for this principal.",
			Validators: []validator.Set{setvalidator.SizeAtLeast(1),
				setvalidator.ValueStringsAre(stringvalidator.OneOf(projectPermissionValues...))}},
	})
	resp.Schema = schema.Schema{
		MarkdownDescription: "This resource manages the complete set of permissions for one group or user on one project. " +
			"Destroy removes the selected principal's grants on the project. It does not change other " +
			"principals.\n\n" +
			"The allowed permissions are `admin`, `codeviewer`, `issueadmin`, `securityhotspotadmin`, " +
			"`architectureadmin`, `scan`, and `user`. The `Anyone` group is not supported.\n\n" +
			"## Grants from the permission template\n\n" +
			"When SonarQube Cloud creates a project, it applies the default permission template, so the " +
			"project already has grants. If the principal already has grants on the project, create takes " +
			"control of them. It removes the grants that are not in `permissions`.\n\n" +
			"## Public projects\n\n" +
			"All users can browse a public project and see its source code. SonarQube Cloud does not store a " +
			"grant of `user` (Browse) or `codeviewer` (See Source Code) on a public project. If " +
			"`permissions` has one of these values and the project is public, the apply fails and sends no " +
			"change. Remove these values, or make the project private.\n\n" +
			"A change of the project visibility also changes the grants. When a project becomes public, " +
			"SonarQube Cloud removes `user` and `codeviewer`. When a project becomes private, SonarQube " +
			"Cloud gives `user` and `codeviewer` to each group that has a different permission on the " +
			"project. The next plan then shows a change.\n\n" +
			"## Administrators\n\n" +
			"SonarQube Cloud does not protect the last project administrator. An apply can remove `admin` " +
			"from all principals of the project.",
		Attributes: attributes,
	}
}

// checkPublicProjectPermissions refuses to add user and codeviewer on a public
// project. The server does not store them there, so the plan never converges.
// The permission read before it found the organization and the project.
func checkPublicProjectPermissions(ctx context.Context, c *client.Client, scope client.PermissionScope, adds []string) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	var refused []string
	for _, permission := range publicProjectPermissions {
		if slices.Contains(adds, permission) {
			refused = append(refused, permission)
		}
	}
	if len(refused) == 0 {
		return diagnostics
	}
	project, err := c.GetProject(ctx, scope.Organization, scope.ProjectKey)
	if err != nil {
		diagnostics.AddError("Cannot read the project visibility", "Read of the project "+scope.ProjectKey+
			" in the organization "+scope.Organization+" failed: "+err.Error())
		return diagnostics
	}
	if project.Visibility == "public" {
		diagnostics.AddError("Cannot grant `"+strings.Join(refused, "` and `")+"` on a public project",
			"The project "+scope.ProjectKey+" is public. All users can browse it and see its source code, "+
				"and SonarQube Cloud does not store a grant of user or codeviewer on it. "+
				"Remove these values from permissions, or make the project private.")
	}
	return diagnostics
}
