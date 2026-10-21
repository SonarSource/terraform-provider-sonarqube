package cloud

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/validate"
)

var (
	_              resource.Resource                   = &scopedOrganizationTokenResource{}
	_              resource.ResourceWithConfigure      = &scopedOrganizationTokenResource{}
	_              resource.ResourceWithValidateConfig = &scopedOrganizationTokenResource{}
	_              resource.ResourceWithModifyPlan     = &scopedOrganizationTokenResource{}
	_              resource.ResourceWithImportState    = &scopedOrganizationTokenResource{}
	tokenIDPattern                                     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

type scopedOrganizationTokenResource struct{ client *client.Client }

type scopedOrganizationTokenModel struct {
	ID                      types.String `tfsdk:"id"`
	Organization            types.String `tfsdk:"organization"`
	Name                    types.String `tfsdk:"name"`
	Description             types.String `tfsdk:"description"`
	ExpiresAt               types.String `tfsdk:"expires_at"`
	OrganizationPermissions types.Set    `tfsdk:"organization_permissions"`
	ProjectPermissions      types.Set    `tfsdk:"project_permissions"`
	CreatedAt               types.String `tfsdk:"created_at"`
	Token                   types.String `tfsdk:"token"`
}

func NewScopedOrganizationTokenResource() resource.Resource {
	return &scopedOrganizationTokenResource{}
}

func (r *scopedOrganizationTokenResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_scoped_organization_token"
}

func (r *scopedOrganizationTokenResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	permissions := func(description string, roles ...string) schema.SetAttribute {
		return schema.SetAttribute{Optional: true, ElementType: types.StringType, MarkdownDescription: description,
			Validators:    []validator.Set{setvalidator.ValueStringsAre(stringvalidator.OneOf(roles...))},
			PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()}}
	}
	resp.Schema = schema.Schema{MarkdownDescription: "This resource creates a scoped organization token on SonarQube Cloud. The\n" +
		"organization must have a plan that supports these tokens. The provider must\n" +
		"use a token whose user can administer the organization.\n" +
		"\n" +
		"Each change to a configurable attribute replaces the token. Destroy revokes\n" +
		"it. The provider checks that the token is absent after revocation.\n" +
		"\n" +
		"The secret example also requires the GitHub provider and access to the\n" +
		"organization. Terraform marks `token` as sensitive, but stores its value in\n" +
		"state. Use a state backend with encryption and restricted access.\n" +
		"\n" +
		"## Permissions\n" +
		"\n" +
		"Set at least one organization or project permission. Organization permissions\n" +
		"are `admin`, `scan`, and `provisioning`. Project permissions are `user`,\n" +
		"`admin`, and `scan`.\n" +
		"\n" +
		"If both sets have values, `organization_permissions` must include\n" +
		"`provisioning`. The provider then enables automatic project permission grants.\n" +
		"A token with only project permissions needs separate project assignments\n" +
		"before it can access those projects. This resource does not manage assignments.\n" +
		"\n" +
		"## Expiration and rotation\n" +
		"\n" +
		"`expires_at` is optional. If set, it must be an RFC 3339 timestamp in the\n" +
		"future, at most one year from creation. The API checks this limit. If omitted,\n" +
		"the token has no expiration date.\n" +
		"\n" +
		"If the server removes a token, including one removed after 60 days without\n" +
		"use, the next plan creates a replacement.\n" +
		"\n" +
		"An expired token can still exist and reserve its name. The next Terraform\n" +
		"plan replaces it. If the configuration has a fixed date in the past, change\n" +
		"that date before apply. Refresh alone does not revoke an expired token.\n" +
		"\n" +
		"For scheduled rotation, use the Time provider and run Terraform regularly:\n" +
		"\n" +
		"```terraform\n" +
		"resource \"time_rotating\" \"analysis\" {\n" +
		"  rotation_days = 30\n" +
		"}\n" +
		"\n" +
		"resource \"sonarqube_cloud_scoped_organization_token\" \"analysis\" {\n" +
		"  organization             = \"my-org\"\n" +
		"  name                     = \"ci-analysis\"\n" +
		"  organization_permissions = [\"scan\"]\n" +
		"\n" +
		"  lifecycle {\n" +
		"    replace_triggered_by = [time_rotating.analysis]\n" +
		"  }\n" +
		"}\n" +
		"```\n" +
		"\n" +
		"The old token is revoked before its replacement is created. Consumers can\n" +
		"fail until they receive the new secret. Do not use `create_before_destroy`\n" +
		"with the same name: token names must be unique in the organization. To allow\n" +
		"both tokens to exist during rotation, give each replacement a unique name\n" +
		"and check how consumers receive the new secret.", Attributes: map[string]schema.Attribute{
		"organization":             schema.StringAttribute{Required: true, MarkdownDescription: "Organization key. A change replaces the token.", Validators: validate.OrganizationKey(), PlanModifiers: replace},
		"name":                     schema.StringAttribute{Required: true, MarkdownDescription: "Nonblank name, 1–100 characters. A change replaces the token.", Validators: []validator.String{stringvalidator.LengthBetween(1, 100), stringvalidator.RegexMatches(regexp.MustCompile(`\S`), "The name must contain a non-space character.")}, PlanModifiers: replace},
		"description":              schema.StringAttribute{Optional: true, MarkdownDescription: "At most 500 characters. A change replaces the token.", Validators: []validator.String{stringvalidator.LengthAtMost(500)}, PlanModifiers: replace},
		"expires_at":               schema.StringAttribute{Optional: true, MarkdownDescription: "RFC 3339 expiration time. Omit for no expiration. A change replaces the token.", PlanModifiers: replace},
		"organization_permissions": permissions("Organization permissions: `admin`, `scan`, and `provisioning`. A change replaces the token.", "admin", "scan", "provisioning"),
		"project_permissions":      permissions("Project permissions: `user`, `admin`, and `scan`. A change replaces the token.", "user", "admin", "scan"),
		"id":                       schema.StringAttribute{Computed: true, MarkdownDescription: "Token definition UUID."},
		"created_at":               schema.StringAttribute{Computed: true, MarkdownDescription: "Creation time in UTC with microsecond precision."},
		"token":                    schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Secret returned once at creation. Refresh keeps the value in state. Null after import; import cannot recover it."},
	}}
}

func (r *scopedOrganizationTokenResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_cloud_scoped_organization_token resource manages an organization token")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *scopedOrganizationTokenResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model scopedOrganizationTokenModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if !resp.Diagnostics.HasError() {
		validateTokenConfiguration(ctx, model, &resp.Diagnostics)
	}
}

func validateTokenConfiguration(ctx context.Context, model scopedOrganizationTokenModel, diagnostics *diag.Diagnostics) {
	if !model.ExpiresAt.IsNull() && !model.ExpiresAt.IsUnknown() {
		if _, err := time.Parse(time.RFC3339, model.ExpiresAt.ValueString()); err != nil {
			diagnostics.AddAttributeError(path.Root("expires_at"), "Invalid token expiration", "Use an RFC 3339 timestamp, such as 2030-01-01T00:00:00Z.")
		}
	}
	org, orgKnown := tokenRoles(ctx, model.OrganizationPermissions, diagnostics)
	project, projectKnown := tokenRoles(ctx, model.ProjectPermissions, diagnostics)
	if !orgKnown || !projectKnown {
		return
	}
	if len(org) == 0 && len(project) == 0 {
		diagnostics.AddError("Missing token permissions", "Set at least one organization or project permission.")
	}
	if len(org) > 0 && len(project) > 0 && !slices.Contains(org, "provisioning") {
		diagnostics.AddAttributeError(path.Root("project_permissions"), "Invalid token permissions", "Project permissions on an organization-wide token require the provisioning organization permission.")
	}
}

func tokenRoles(ctx context.Context, value types.Set, diagnostics *diag.Diagnostics) ([]string, bool) {
	if value.IsUnknown() {
		return nil, false
	}
	if value.IsNull() {
		return nil, true
	}
	for _, element := range value.Elements() {
		if element.IsUnknown() {
			return nil, false
		}
	}
	var roles []string
	diagnostics.Append(value.ElementsAs(ctx, &roles, false)...)
	return roles, !diagnostics.HasError()
}

func tokenExpired(expiresAt types.String, now time.Time) bool {
	if expiresAt.IsNull() || expiresAt.IsUnknown() {
		return false
	}
	expires, err := time.Parse(time.RFC3339, expiresAt.ValueString())
	return err == nil && !expires.After(now)
}

func (r *scopedOrganizationTokenResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var state, plan scopedOrganizationTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if tokenExpired(state.ExpiresAt, time.Now()) {
		// The expired token still reserves its name until Terraform deletes it.
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("id"))
		plan.ID = types.StringUnknown()
		plan.CreatedAt = types.StringUnknown()
		plan.Token = types.StringUnknown()
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
	}
}

func (r *scopedOrganizationTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan scopedOrganizationTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateTokenConfiguration(ctx, plan, &resp.Diagnostics)
	orgRoles, orgKnown := tokenRoles(ctx, plan.OrganizationPermissions, &resp.Diagnostics)
	projectRoles, projectKnown := tokenRoles(ctx, plan.ProjectPermissions, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !orgKnown || !projectKnown {
		resp.Diagnostics.AddError("Unknown token permissions", "Token permissions must be known before creation.")
		return
	}
	org, ok := lookupOrganization(ctx, r.client, plan.Organization.ValueString(), path.Root("organization"), &resp.Diagnostics)
	if !ok {
		return
	}
	assignments := []client.TokenRoleAssignment{}
	if len(orgRoles) > 0 {
		assignments = append(assignments, client.TokenRoleAssignment{ResourceType: "organization", Roles: orgRoles})
	}
	if len(projectRoles) > 0 {
		assignments = append(assignments, client.TokenRoleAssignment{ResourceType: "project", Roles: projectRoles})
	}
	result, err := r.client.CreateOrganizationToken(ctx, client.OrganizationTokenRequest{
		OrganizationID: org.UUIDV4, Name: plan.Name.ValueString(), Description: plan.Description.ValueStringPointer(), ExpiresAt: plan.ExpiresAt.ValueStringPointer(),
		RoleAssignments: assignments, AutoGrantProjectPermissions: len(orgRoles) > 0 && len(projectRoles) > 0,
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the organization token", err.Error())
		return
	}
	if result.ID == "" {
		resp.Diagnostics.AddError("Incomplete token creation response", "The API returned no token ID. Check for a token with this name before retrying; its secret cannot be recovered.")
		return
	}
	plan.ID = types.StringValue(result.ID)
	plan.Token = types.StringValue(result.Token)
	plan.CreatedAt = tokenCreationTime(result.CreatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if result.Token == "" || result.CreatedAt == "" {
		resp.Diagnostics.AddError("Incomplete token creation response", "The API returned no secret or creation time. Revoke and recreate the token. The returned ID was recorded for cleanup.")
	}
}

func (r *scopedOrganizationTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state scopedOrganizationTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	org, ok := lookupOrganization(ctx, r.client, state.Organization.ValueString(), path.Root("organization"), &resp.Diagnostics)
	if !ok {
		return
	}
	token, err := r.client.FindOrganizationToken(ctx, org.UUIDV4, state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the organization token", err.Error())
		return
	}
	state.Name = types.StringValue(token.Name)
	state.Description = types.StringPointerValue(token.Description)
	state.CreatedAt = tokenCreationTime(token.CreatedAt)
	state.ExpiresAt = equivalentTokenExpiration(state.ExpiresAt, token.ExpiresAt)
	var orgRoles, projectRoles []string
	for _, assignment := range token.RoleAssignments {
		switch assignment.ResourceType {
		case "organization":
			orgRoles = append(orgRoles, assignment.Roles...)
		case "project":
			projectRoles = append(projectRoles, assignment.Roles...)
		default:
			resp.Diagnostics.AddError("Unsupported token scope", "The API returned an unknown token resource type.")
			return
		}
	}
	if token.AutoGrantProjectPermissions != (len(orgRoles) > 0 && len(projectRoles) > 0) {
		resp.Diagnostics.AddError("Unsupported automatic project grants", "The token's automatic project grants do not match its permission sets. This resource requires automatic grants only when both organization and project permissions are set. Recreate the token with this resource to use that behavior.")
		return
	}
	state.OrganizationPermissions = tokenRoleState(ctx, orgRoles, state.OrganizationPermissions, &resp.Diagnostics)
	state.ProjectPermissions = tokenRoleState(ctx, projectRoles, state.ProjectPermissions, &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	}
}

// The create response has nanoseconds, but persisted metadata has microseconds.
func tokenCreationTime(value string) types.String {
	timestamp, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return types.StringValue(value)
	}
	return types.StringValue(timestamp.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano))
}

func equivalentTokenExpiration(prior types.String, value *string) types.String {
	if value != nil && !prior.IsNull() && !prior.IsUnknown() {
		old, oldErr := time.Parse(time.RFC3339, prior.ValueString())
		current, currentErr := time.Parse(time.RFC3339, *value)
		if oldErr == nil && currentErr == nil && old.Equal(current) {
			return prior
		}
	}
	return types.StringPointerValue(value)
}

func tokenRoleState(ctx context.Context, roles []string, prior types.Set, diagnostics *diag.Diagnostics) types.Set {
	if len(roles) == 0 && prior.IsNull() {
		return types.SetNull(types.StringType)
	}
	if roles == nil {
		roles = []string{}
	}
	result, diags := types.SetValueFrom(ctx, types.StringType, roles)
	diagnostics.Append(diags...)
	return result
}

func (r *scopedOrganizationTokenResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Token replacement required", "Token settings cannot be updated. Report this unexpected update plan to the provider maintainers.")
}

func (r *scopedOrganizationTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state scopedOrganizationTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	org, ok := lookupOrganization(ctx, r.client, state.Organization.ValueString(), path.Root("organization"), &resp.Diagnostics)
	if !ok {
		return
	}
	if err := r.client.DeleteOrganizationToken(ctx, org.UUIDV4, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot revoke the organization token", err.Error())
	}
}

func (r *scopedOrganizationTokenResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organization, id, ok := strings.Cut(req.ID, "/")
	if !ok || organization == "" || !tokenIDPattern.MatchString(id) {
		resp.Diagnostics.AddError("Invalid token import ID", "Use <organization>/<token_id>, where token_id is a UUID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
