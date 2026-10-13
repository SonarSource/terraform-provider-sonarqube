package cloud

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
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
	return &organizationPermissionsResource{}
}

type organizationPermissionsResource struct {
	client *client.Client
}

type organizationPermissionsModel struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	GroupName    types.String `tfsdk:"group_name"`
	Login        types.String `tfsdk:"login"`
	Permissions  types.Set    `tfsdk:"permissions"`
}

func (m organizationPermissionsModel) scope() client.PermissionScope {
	return client.PermissionScope{Organization: m.Organization.ValueString()}
}

func (m organizationPermissionsModel) principal() client.PermissionPrincipal {
	if m.GroupName.IsNull() {
		return client.PermissionPrincipal{Name: m.Login.ValueString()}
	}
	return client.PermissionPrincipal{Group: true, Name: m.GroupName.ValueString()}
}

func (m organizationPermissionsModel) importID() string {
	principal := m.principal()
	kind := "user"
	if principal.Group {
		kind = "group"
	}
	return m.Organization.ValueString() + "/" + kind + "/" + principal.Name
}

func (r *organizationPermissionsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_organization_permissions"
}

func (r *organizationPermissionsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages all organization permissions of one group or user in SonarQube Cloud. " +
			"Create takes control of the principal's full permission set. It removes grants that are not in `permissions`. " +
			"Destroy removes all of that principal's organization grants and does not change other principals. " +
			"A FREE organization accepts group permission changes. SonarQube Cloud refuses to remove the last " +
			"organization administrator. Import with `<organization>/group/<name>` or `<organization>/user/<login>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Resource identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"organization": schema.StringAttribute{Required: true, Description: "Organization key.",
				Validators:    validate.OrganizationKey(),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"group_name": schema.StringAttribute{Optional: true, Description: "Exact group name. Do not use the Anyone group.",
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 255), stringvalidator.NoneOfCaseInsensitive("Anyone")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"login": schema.StringAttribute{Optional: true, Description: "Exact user login.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"permissions": schema.SetAttribute{Required: true, ElementType: types.StringType,
				Description: "Complete nonempty set of organization permissions for this principal.",
				Validators: []validator.Set{setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.OneOf(organizationPermissionValues...))}},
		},
	}
}

func (r *organizationPermissionsResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{resourcevalidator.ExactlyOneOf(path.MatchRoot("group_name"), path.MatchRoot("login"))}
}

func (r *organizationPermissionsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_cloud_organization_permissions resource manages organization grants")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *organizationPermissionsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *organizationPermissionsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

// apply makes the permissions of the principal equal to the plan. Create does
// the same, so it also removes the permissions that are not in the plan.
func (r *organizationPermissionsResource) apply(ctx context.Context, planned tfsdk.Plan, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	var plan organizationPermissionsModel
	diagnostics.Append(planned.Get(ctx, &plan)...)
	if diagnostics.HasError() {
		return
	}
	var desired []string
	diagnostics.Append(plan.Permissions.ElementsAs(ctx, &desired, false)...)
	if diagnostics.HasError() {
		return
	}
	current, err := r.client.GetPermissions(ctx, plan.scope(), plan.principal())
	if err != nil {
		diagnostics.AddError("Cannot read organization permissions", permissionReadDetail(err, plan))
		return
	}
	if err := r.applyDiff(ctx, plan, current, desired); err != nil {
		diagnostics.AddError("Cannot change organization permissions", err.Error())
		return
	}
	plan.ID = types.StringValue(plan.importID())
	diagnostics.Append(state.Set(ctx, &plan)...)
}

// Read removes the resource from the state when the principal has no
// permission, because then there is nothing that the resource manages.
func (r *organizationPermissionsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationPermissionsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	current, err := r.client.GetPermissions(ctx, state.scope(), state.principal())
	switch {
	case errors.Is(err, client.ErrOrganizationNotFound):
		resp.Diagnostics.AddWarning(
			"The organization "+state.Organization.ValueString()+" was not found",
			"Terraform removes the permissions from the state. The organization was deleted, or "+
				"the token cannot read it any more.",
		)
		resp.State.RemoveResource(ctx)
		return
	case errors.Is(err, client.ErrNotFound) || (err == nil && len(current) == 0):
		resp.State.RemoveResource(ctx)
		return
	case err != nil:
		resp.Diagnostics.AddError("Cannot read organization permissions", err.Error())
		return
	}
	permissions, diagnostics := types.SetValueFrom(ctx, types.StringType, current)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.ID = types.StringValue(state.importID())
	state.Permissions = permissions
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *organizationPermissionsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationPermissionsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	current, err := r.client.GetPermissions(ctx, state.scope(), state.principal())
	if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrOrganizationNotFound) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read organization permissions before destroy", err.Error())
		return
	}
	if err := r.applyDiff(ctx, state, current, nil); err != nil {
		resp.Diagnostics.AddError("Cannot remove organization permissions", err.Error())
	}
}

func (r *organizationPermissionsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organization, kind, name, ok := parsePermissionImportID(req.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid organization permission import ID", "Use <organization>/group/<name> or <organization>/user/<login>.")
		return
	}
	if kind == "group" && strings.EqualFold(name, "Anyone") {
		resp.Diagnostics.AddError("Cannot import the Anyone group", "Use a named group.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	if kind == "group" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_name"), name)...)
	} else {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("login"), name)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func parsePermissionImportID(id string) (organization, kind, name string, ok bool) {
	parts := strings.SplitN(id, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" || (parts[1] != "group" && parts[1] != "user") {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// applyDiff adds the missing permissions before it removes the extra ones, so
// that a change of admin does not remove the last administrator for a moment.
func (r *organizationPermissionsResource) applyDiff(ctx context.Context, model organizationPermissionsModel, current, desired []string) error {
	scope, principal := model.scope(), model.principal()
	adds, removes := permissionDiff(current, desired)
	for _, permission := range adds {
		if err := r.client.AddPermission(ctx, scope, principal, permission); err != nil {
			return fmt.Errorf("add %s: %w", permission, err)
		}
	}
	for _, permission := range removes {
		if err := r.client.RemovePermission(ctx, scope, principal, permission); err != nil {
			return fmt.Errorf("remove %s: %w", permission, err)
		}
	}
	return nil
}

func permissionDiff(current, desired []string) (adds, removes []string) {
	for _, permission := range desired {
		if !slices.Contains(current, permission) {
			adds = append(adds, permission)
		}
	}
	for _, permission := range current {
		if !slices.Contains(desired, permission) {
			removes = append(removes, permission)
		}
	}
	slices.Sort(adds)
	slices.Sort(removes)
	return adds, removes
}

func permissionReadDetail(err error, model organizationPermissionsModel) string {
	organization := model.Organization.ValueString()
	switch {
	case errors.Is(err, client.ErrOrganizationNotFound):
		return "The organization " + organization + " was not found. Check the key, and check that the token can read the organization."
	case errors.Is(err, client.ErrNotFound):
		principal := model.principal()
		if principal.Group {
			return "No group " + principal.Name + " is in the organization " + organization + ". The name must match exactly, with the same case."
		}
		return "No member with the login " + principal.Name + " is in the organization " + organization + "."
	default:
		return err.Error()
	}
}
