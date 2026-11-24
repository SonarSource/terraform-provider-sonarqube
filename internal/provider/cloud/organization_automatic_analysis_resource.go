package cloud

import (
	"context"
	"net/http"

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
	_ resource.Resource                = &organizationAutomaticAnalysisResource{}
	_ resource.ResourceWithConfigure   = &organizationAutomaticAnalysisResource{}
	_ resource.ResourceWithImportState = &organizationAutomaticAnalysisResource{}
)

// NewOrganizationAutomaticAnalysisResource returns the
// sonarqube_cloud_organization_automatic_analysis resource.
func NewOrganizationAutomaticAnalysisResource() resource.Resource {
	return &organizationAutomaticAnalysisResource{}
}

type organizationAutomaticAnalysisResource struct {
	client *client.Client
}

type organizationAutomaticAnalysisModel struct {
	ID                    types.String `tfsdk:"id"`
	Organization          types.String `tfsdk:"organization"`
	EnabledForNewProjects types.Bool   `tfsdk:"enabled_for_new_projects"`
}

func (r *organizationAutomaticAnalysisResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_organization_automatic_analysis"
}

func (r *organizationAutomaticAnalysisResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Sets whether new projects of a SonarQube Cloud organization get Automatic Analysis. " +
			"The setting does not change the existing projects. To set one project, use " +
			"`sonarqube_cloud_project_automatic_analysis`.\n\n" +
			"The organization must be on the Enterprise plan. On the other plans, SonarQube Cloud " +
			"turns Automatic Analysis on for each eligible new project, and refuses a change.\n\n" +
			"`terraform destroy` removes the value of the organization. New projects then get the " +
			"default of SonarQube Cloud again.",
		Attributes: organizationSettingAttributes(map[string]schema.Attribute{
			"enabled_for_new_projects": schema.BoolAttribute{
				Required:    true,
				Description: "Set to `true` to turn Automatic Analysis on for new projects, and to `false` to turn it off.",
			},
		}),
	}
}

func (r *organizationAutomaticAnalysisResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClientAcceptingScopedToken(req.ProviderData,
		"The sonarqube_cloud_organization_automatic_analysis resource manages Automatic Analysis")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *organizationAutomaticAnalysisResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *organizationAutomaticAnalysisResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

// apply writes the value of the plan to the organization, then writes the plan
// to the state. The write is sent also when the value is the default, so that
// the organization keeps it when the default changes.
func (r *organizationAutomaticAnalysisResource) apply(ctx context.Context, planned tfsdk.Plan, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	var plan organizationAutomaticAnalysisModel
	diagnostics.Append(planned.Get(ctx, &plan)...)
	if diagnostics.HasError() {
		return
	}
	organization := plan.Organization.ValueString()

	// The settings API answers with the default for an organization that does
	// not exist, so the organization is read first.
	if _, ok := lookupOrganization(ctx, r.client, organization, path.Root("organization"), diagnostics); !ok {
		return
	}
	err := r.client.SetOrganizationAutomaticAnalysis(ctx, organization, plan.EnabledForNewProjects.ValueBool())
	if err != nil {
		diagnostics.AddError("Cannot set Automatic Analysis for new projects of the organization "+organization,
			organizationAutomaticAnalysisDetail(err))
		return
	}

	plan.ID = plan.Organization
	diagnostics.Append(state.Set(ctx, &plan)...)
}

func (r *organizationAutomaticAnalysisResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationAutomaticAnalysisModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization := state.Organization.ValueString()

	if _, ok := refreshOrganization(ctx, r.client, organization,
		"the Automatic Analysis setting for new projects of this organization", resp); !ok {
		return
	}
	value, err := r.client.GetOrganizationAutomaticAnalysis(ctx, organization)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read Automatic Analysis for new projects of the organization "+organization, err.Error())
		return
	}
	state.ID = types.StringValue(organization)
	// Without its own value, the organization follows the default, which can
	// change. A null value makes the next plan set the value of the
	// configuration.
	state.EnabledForNewProjects = types.BoolNull()
	if value.Own {
		state.EnabledForNewProjects = types.BoolValue(value.Enabled)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete removes the value of the organization, so that new projects get the
// default of SonarQube Cloud again.
func (r *organizationAutomaticAnalysisResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationAutomaticAnalysisModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization := state.Organization.ValueString()
	if err := r.client.ResetOrganizationAutomaticAnalysis(ctx, organization); err != nil {
		detail := err.Error()
		// The reset checks only the permission, not the plan of the organization.
		if client.IsStatus(err, http.StatusForbidden) {
			detail += "\n\nThe token must have the Administer permission on the organization."
		}
		resp.Diagnostics.AddError("Cannot reset Automatic Analysis for new projects of the organization "+organization, detail)
	}
}

// organizationAutomaticAnalysisDetail explains a refusal of a write.
func organizationAutomaticAnalysisDetail(err error) string {
	if !client.IsStatus(err, http.StatusForbidden) {
		return err.Error()
	}
	return err.Error() + "\n\nThe organization must be on the Enterprise plan, and the token must have the " +
		"Administer permission on the organization."
}

func (r *organizationAutomaticAnalysisResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("organization"), req, resp)
}
