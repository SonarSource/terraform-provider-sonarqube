package cloud

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
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
)

// permissionsModel is the state of a resource that controls the full
// permission set of one group or user in one scope.
type permissionsModel interface {
	scope() client.PermissionScope
	principal() client.PermissionPrincipal
	permissions() types.Set
	setID(id string)
	setPermissions(permissions types.Set)
}

type permissionsModelPointer[M any] interface {
	*M
	permissionsModel
}

// permissionsResource has the logic of the permission resources. The resources
// add the type name and the schema.
type permissionsResource[M any, P permissionsModelPointer[M]] struct {
	client *client.Client
	// scopeName names the scope in diagnostics, for example "organization".
	scopeName string
	// scopeAttributes are the attributes that the import ID starts with, in order.
	scopeAttributes []string
	// typeSuffix follows the provider type name, for example "_cloud_project_permissions".
	typeSuffix string
	// checkApply, when set, can refuse the permissions that create or update
	// would add, before any change is sent.
	checkApply func(ctx context.Context, c *client.Client, scope client.PermissionScope, adds []string) diag.Diagnostics
}

// noun names the managed grants in diagnostics, for example "project permissions".
func (r *permissionsResource[M, P]) noun() string {
	return r.scopeName + " permissions"
}

// addReadError reports a failed read of the current grants.
func (r *permissionsResource[M, P]) addReadError(diagnostics *diag.Diagnostics, err error, model P) {
	diagnostics.AddError("Cannot read "+r.noun(), permissionReadDetail(err, model.scope(), model.principal()))
}

func (r *permissionsResource[M, P]) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + r.typeSuffix
}

func (r *permissionsResource[M, P]) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{resourcevalidator.ExactlyOneOf(path.MatchRoot("group_name"), path.MatchRoot("login"))}
}

func (r *permissionsResource[M, P]) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube"+r.typeSuffix+" resource manages "+r.scopeName+" grants")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *permissionsResource[M, P]) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *permissionsResource[M, P]) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

// apply makes the permissions of the principal equal to the plan. Create does
// the same, so it also removes the permissions that are not in the plan.
func (r *permissionsResource[M, P]) apply(ctx context.Context, planned tfsdk.Plan, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	var plan M
	diagnostics.Append(planned.Get(ctx, &plan)...)
	if diagnostics.HasError() {
		return
	}
	model := P(&plan)
	var desired []string
	diagnostics.Append(model.permissions().ElementsAs(ctx, &desired, false)...)
	if diagnostics.HasError() {
		return
	}
	current, err := r.client.GetPermissions(ctx, model.scope(), model.principal())
	if err != nil {
		r.addReadError(diagnostics, err, model)
		return
	}
	if r.checkApply != nil {
		adds, _ := permissionDiff(current, desired)
		diagnostics.Append(r.checkApply(ctx, r.client, model.scope(), adds)...)
		if diagnostics.HasError() {
			return
		}
	}
	if err := r.applyDiff(ctx, model, current, desired); err != nil {
		diagnostics.AddError("Cannot change "+r.noun(), err.Error())
		return
	}
	model.setID(r.importID(model))
	diagnostics.Append(state.Set(ctx, &plan)...)
}

// Read removes the resource from the state when the principal has no
// permission, because then there is nothing that the resource manages.
func (r *permissionsResource[M, P]) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state M
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	model := P(&state)
	current, err := r.client.GetPermissions(ctx, model.scope(), model.principal())
	var missing *client.ScopeNotFoundError
	switch {
	case errors.As(err, &missing):
		resp.Diagnostics.AddWarning(
			"The "+missing.Kind+" "+missing.Key+" was not found",
			"Terraform removes the permissions from the state. The "+missing.Kind+" was deleted, or "+
				"the token cannot read it any more.",
		)
		resp.State.RemoveResource(ctx)
		return
	case errors.Is(err, client.ErrNotFound) || (err == nil && len(current) == 0):
		resp.State.RemoveResource(ctx)
		return
	case err != nil:
		r.addReadError(&resp.Diagnostics, err, model)
		return
	}
	permissions, diagnostics := types.SetValueFrom(ctx, types.StringType, current)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	model.setID(r.importID(model))
	model.setPermissions(permissions)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *permissionsResource[M, P]) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state M
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	model := P(&state)
	current, err := r.client.GetPermissions(ctx, model.scope(), model.principal())
	var missing *client.ScopeNotFoundError
	if errors.Is(err, client.ErrNotFound) || errors.As(err, &missing) {
		return
	}
	if err != nil {
		r.addReadError(&resp.Diagnostics, err, model)
		return
	}
	if err := r.applyDiff(ctx, model, current, nil); err != nil {
		resp.Diagnostics.AddError("Cannot remove "+r.noun(), err.Error())
	}
}

func (r *permissionsResource[M, P]) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	scope, kind, name, ok := parsePermissionImportID(req.ID, len(r.scopeAttributes))
	if !ok {
		prefix := "<" + strings.Join(r.scopeAttributes, ">/<") + ">"
		resp.Diagnostics.AddError("Invalid "+r.scopeName+" permission import ID",
			"Use "+prefix+"/group/<name> or "+prefix+"/user/<login>.")
		return
	}
	if kind == "group" && strings.EqualFold(name, "Anyone") {
		resp.Diagnostics.AddError("Cannot import the Anyone group", "Use a named group.")
		return
	}
	for i, attribute := range r.scopeAttributes {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(attribute), scope[i])...)
	}
	if kind == "group" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_name"), name)...)
	} else {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("login"), name)...)
	}
	id := strings.Join(append(scope, kind, name), "/")
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// applyDiff adds the missing permissions before it removes the extra ones, so
// that a change of admin does not remove the last administrator for a moment.
func (r *permissionsResource[M, P]) applyDiff(ctx context.Context, model P, current, desired []string) error {
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

// permissionPrincipalAttributes returns the attributes that all permission
// resources have.
func permissionPrincipalAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, Description: "Resource identifier.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"group_name": schema.StringAttribute{Optional: true, Description: "Exact group name. Do not use the Anyone group.",
			Validators:    []validator.String{stringvalidator.LengthBetween(1, 255), stringvalidator.NoneOfCaseInsensitive("Anyone")},
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"login": schema.StringAttribute{Optional: true, Description: "Exact user login.",
			Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
	}
}

func permissionPrincipal(groupName, login types.String) client.PermissionPrincipal {
	if groupName.IsNull() {
		return client.PermissionPrincipal{Name: login.ValueString()}
	}
	return client.PermissionPrincipal{Group: true, Name: groupName.ValueString()}
}

// importID builds the ID in the same format that ImportState reads.
func (r *permissionsResource[M, P]) importID(model P) string {
	return permissionImportID(model.scope(), model.principal(), len(r.scopeAttributes))
}

// permissionImportID uses the first scopeParts of organization and project key.
func permissionImportID(scope client.PermissionScope, principal client.PermissionPrincipal, scopeParts int) string {
	parts := []string{scope.Organization, scope.ProjectKey}[:scopeParts]
	kind := "user"
	if principal.Group {
		kind = "group"
	}
	return strings.Join(append(parts, kind, principal.Name), "/")
}

// parsePermissionImportID reads <scope...>/<group|user>/<name>. The name is
// the rest of the ID, so a group name can hold a slash.
func parsePermissionImportID(id string, scopeParts int) (scope []string, kind, name string, ok bool) {
	parts := strings.SplitN(id, "/", scopeParts+2)
	if len(parts) != scopeParts+2 {
		return nil, "", "", false
	}
	scope, kind, name = parts[:scopeParts], parts[scopeParts], parts[scopeParts+1]
	if slices.Contains(scope, "") || name == "" || (kind != "group" && kind != "user") {
		return nil, "", "", false
	}
	return scope, kind, name, true
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

func permissionReadDetail(err error, scope client.PermissionScope, principal client.PermissionPrincipal) string {
	var missing *client.ScopeNotFoundError
	switch {
	case errors.As(err, &missing):
		return "The " + missing.Kind + " " + missing.Key + " was not found. Check the key, and check that the token can read the " + missing.Kind + "."
	case errors.Is(err, client.ErrNotFound):
		if principal.Group {
			return "No group " + principal.Name + " is in the organization " + scope.Organization + ". The name must match exactly, with the same case."
		}
		return "No member with the login " + principal.Name + " is in the organization " + scope.Organization + "."
	default:
		return err.Error()
	}
}
