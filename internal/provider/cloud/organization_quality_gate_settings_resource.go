package cloud

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
)

var (
	_ resource.Resource                = &organizationQualityGateSettingsResource{}
	_ resource.ResourceWithConfigure   = &organizationQualityGateSettingsResource{}
	_ resource.ResourceWithImportState = &organizationQualityGateSettingsResource{}
)

// NewOrganizationQualityGateSettingsResource returns the Cloud organization quality gate settings resource.
func NewOrganizationQualityGateSettingsResource() resource.Resource {
	return &organizationQualityGateSettingsResource{}
}

type organizationQualityGateSettingsResource struct {
	client *client.Client
}

type organizationQualityGateSettingsModel struct {
	ID                 types.String `tfsdk:"id"`
	Organization       types.String `tfsdk:"organization"`
	IgnoreSmallChanges types.Bool   `tfsdk:"ignore_small_changes"`
}

const settingsReadError = "Cannot read quality gate settings of the organization "

func (r *organizationQualityGateSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_organization_quality_gate_settings"
}

func (r *organizationQualityGateSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages whether a SonarQube Cloud organization ignores coverage and duplication " +
			"conditions on changes with fewer than 20 new lines. A project administrator can turn this " +
			"setting off for a project when the organization setting is on.\n\n" +
			"~> **A change resets the projects.** When the value changes, SonarQube Cloud writes the " +
			"new value to all projects of the organization, and project overrides are lost. " +
			"Terraform sends no change when the value is already correct.\n\n" +
			"~> **Destroy keeps the setting.** Terraform removes this resource from state, but " +
			"the organization keeps its current setting.",
		Attributes: organizationSettingAttributes(map[string]schema.Attribute{
			"ignore_small_changes": schema.BoolAttribute{
				Required:    true,
				Description: "Ignore new-code coverage and duplication conditions until a change has at least 20 new lines.",
			},
		}),
	}
}

func (r *organizationQualityGateSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData,
		"The sonarqube_cloud_organization_quality_gate_settings resource manages organization quality gate settings")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *organizationQualityGateSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *organizationQualityGateSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *organizationQualityGateSettingsResource) apply(ctx context.Context, planned tfsdk.Plan, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	var plan organizationQualityGateSettingsModel
	diagnostics.Append(planned.Get(ctx, &plan)...)
	if diagnostics.HasError() {
		return
	}

	organization := plan.Organization.ValueString()
	org, ok := lookupOrganization(ctx, r.client, organization, path.Root("organization"), diagnostics)
	if !ok {
		return
	}
	settings, err := r.client.GetOrganizationQualityGateSettings(ctx, org.UUIDV4)
	if err != nil {
		diagnostics.AddError(settingsReadError+organization, err.Error())
		return
	}
	// A patch of the organization also writes the value to all its projects,
	// so send it only when the value changes. This keeps project overrides.
	if settings.IgnoreSmallChanges != plan.IgnoreSmallChanges.ValueBool() {
		if err := r.client.UpdateOrganizationQualityGateSettings(ctx, settings.ID, plan.IgnoreSmallChanges.ValueBool()); err != nil {
			diagnostics.AddError("Cannot update quality gate settings of the organization "+organization, err.Error())
			return
		}
	}

	plan.ID = plan.Organization
	diagnostics.Append(state.Set(ctx, &plan)...)
}

func (r *organizationQualityGateSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationQualityGateSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organization := state.Organization.ValueString()
	org, ok := refreshOrganization(ctx, r.client, organization,
		"the quality gate settings of this organization", resp)
	if !ok {
		return
	}
	settings, err := r.client.GetOrganizationQualityGateSettings(ctx, org.UUIDV4)
	if err != nil {
		resp.Diagnostics.AddError(settingsReadError+organization, err.Error())
		return
	}

	state.ID = types.StringValue(organization)
	state.IgnoreSmallChanges = types.BoolValue(settings.IgnoreSmallChanges)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *organizationQualityGateSettingsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationQualityGateSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning("The organization quality gate setting was not changed",
		"Terraform forgot the setting of organization "+state.Organization.ValueString()+
			". The organization keeps its current value.")
}

func (r *organizationQualityGateSettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("organization"), req, resp)
}
