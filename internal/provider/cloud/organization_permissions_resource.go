package cloud

import (
	"context"
	"maps"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                     = &organizationPermissionsResource{}
	_ resource.ResourceWithConfigure        = &organizationPermissionsResource{}
	_ resource.ResourceWithImportState      = &organizationPermissionsResource{}
	_ resource.ResourceWithConfigValidators = &organizationPermissionsResource{}
)

var organizationPermissionValues = []string{
	"admin", "gateadmin", "profileadmin", "architectureadmin", "provisioning", "scan",
}

// NewOrganizationPermissionsResource returns the Cloud organization permission resource.
func NewOrganizationPermissionsResource() resource.Resource {
	return &organizationPermissionsResource{permissionsResource[organizationPermissionsModel, *organizationPermissionsModel]{
		scopeName:       "organization",
		scopeAttributes: []string{"organization"},
		typeSuffix:      "_cloud_organization_permissions",
	}}
}

type organizationPermissionsResource struct {
	permissionsResource[organizationPermissionsModel, *organizationPermissionsModel]
}

type organizationPermissionsModel struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	GroupName    types.String `tfsdk:"group_name"`
	Login        types.String `tfsdk:"login"`
	Permissions  types.Set    `tfsdk:"permissions"`
}

func (m *organizationPermissionsModel) scope() client.PermissionScope {
	return client.PermissionScope{Organization: m.Organization.ValueString()}
}

func (m *organizationPermissionsModel) principal() client.PermissionPrincipal {
	return permissionPrincipal(m.GroupName, m.Login)
}

func (m *organizationPermissionsModel) permissions() types.Set { return m.Permissions }

func (m *organizationPermissionsModel) setID(id string) { m.ID = types.StringValue(id) }

func (m *organizationPermissionsModel) setPermissions(permissions types.Set) {
	m.Permissions = permissions
}

func (r *organizationPermissionsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := permissionPrincipalAttributes()
	maps.Copy(attributes, map[string]schema.Attribute{
		"organization": schema.StringAttribute{Required: true, Description: "Organization key.",
			Validators:    validate.OrganizationKey(),
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"permissions": schema.SetAttribute{Required: true, ElementType: types.StringType,
			Description: "Complete nonempty set of organization permissions for this principal.",
			Validators: []validator.Set{setvalidator.SizeAtLeast(1),
				setvalidator.ValueStringsAre(stringvalidator.OneOf(organizationPermissionValues...))}},
	})
	resp.Schema = schema.Schema{
		Description: "Manages all organization permissions of one group or user in SonarQube Cloud. " +
			"Create takes control of the principal's full permission set. It removes grants that are not in `permissions`. " +
			"Destroy removes all of that principal's organization grants and does not change other principals. " +
			"A FREE organization accepts group permission changes. SonarQube Cloud refuses to remove the last " +
			"organization administrator. Import with `<organization>/group/<name>` or `<organization>/user/<login>`.",
		Attributes: attributes,
	}
}
