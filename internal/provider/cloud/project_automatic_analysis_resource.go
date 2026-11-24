package cloud

import (
	"context"
	"errors"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
)

var (
	_ resource.Resource                = &projectAutomaticAnalysisResource{}
	_ resource.ResourceWithConfigure   = &projectAutomaticAnalysisResource{}
	_ resource.ResourceWithImportState = &projectAutomaticAnalysisResource{}
)

// NewProjectAutomaticAnalysisResource returns the
// sonarqube_cloud_project_automatic_analysis resource.
func NewProjectAutomaticAnalysisResource() resource.Resource {
	return &projectAutomaticAnalysisResource{}
}

type projectAutomaticAnalysisResource struct {
	client *client.Client
}

type projectAutomaticAnalysisModel struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	ProjectKey   types.String `tfsdk:"project_key"`
	Enabled      types.Bool   `tfsdk:"enabled"`
}

func (r *projectAutomaticAnalysisResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_project_automatic_analysis"
}

func (r *projectAutomaticAnalysisResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Turns Automatic Analysis on or off for a SonarQube Cloud project. When Automatic " +
			"Analysis is off, the project gets its analyses from a CI pipeline.\n\n" +
			"To turn Automatic Analysis on, the project must be bound to a repository, and it must be the " +
			"only project bound to that repository. SonarQube Cloud then checks the repository in the " +
			"background.\n\n" +
			"~> **Destroy keeps the setting.** Terraform removes this resource from the state, but the " +
			"project keeps its current setting.",
		Attributes: projectScopedAttributes(map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				Required:    true,
				Description: "Set to `true` to turn Automatic Analysis on, and to `false` to turn it off.",
			},
		}),
	}
}

func (r *projectAutomaticAnalysisResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData,
		"The sonarqube_cloud_project_automatic_analysis resource manages Automatic Analysis")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *projectAutomaticAnalysisResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *projectAutomaticAnalysisResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

// apply writes the setting of the plan to the project, then writes the plan to
// the state.
func (r *projectAutomaticAnalysisResource) apply(ctx context.Context, planned tfsdk.Plan, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	var plan projectAutomaticAnalysisModel
	diagnostics.Append(planned.Get(ctx, &plan)...)
	if diagnostics.HasError() {
		return
	}
	organization := plan.Organization.ValueString()
	key := plan.ProjectKey.ValueString()
	enabled := plan.Enabled.ValueBool()

	// The settings API does not know organizations, so this check makes sure
	// that the project belongs to the organization of the configuration.
	if _, _, ok := lookupProject(ctx, r.client, organization, key, diagnostics); !ok {
		return
	}

	current, err := r.client.GetProjectAutomaticAnalysis(ctx, key)
	if errors.Is(err, client.ErrNotFound) {
		addProjectNotFound(diagnostics, organization, key)
		return
	}
	if err != nil {
		diagnostics.AddError(automaticAnalysisReadError+key, err.Error())
		return
	}
	// A project without a stored value reads as off, but SonarQube Cloud can
	// still turn Automatic Analysis on at its first analysis. Only a stored
	// off prevents this, so off is always sent. A change to on starts a new
	// check of the repository, so on is sent only when the value is different.
	if !enabled || current != enabled {
		if err := r.client.SetProjectAutomaticAnalysis(ctx, key, enabled); err != nil {
			addAutomaticAnalysisWriteError(diagnostics, key, enabled, err)
			return
		}
	}

	plan.ID = types.StringValue(organization + "/" + key)
	diagnostics.Append(state.Set(ctx, &plan)...)
}

const automaticAnalysisReadError = "Cannot read the Automatic Analysis setting of the project "

func addAutomaticAnalysisWriteError(diagnostics *diag.Diagnostics, key string, enabled bool, err error) {
	detail := err.Error()
	switch {
	case client.IsStatus(err, http.StatusNotFound):
		// The server answers 404 also when the token does not have the
		// Administer permission on the project.
		detail += "\n\nThe token may not have the Administer permission on the project or its " +
			"organization, or the project was deleted."
	case enabled && client.IsStatus(err, http.StatusBadRequest):
		detail += "\n\nPossible causes: the project is not bound to a repository, or other projects are " +
			"bound to the same repository. Automatic Analysis needs a project that is the only project " +
			"bound to its repository."
	}
	diagnostics.AddError("Cannot change the Automatic Analysis setting of the project "+key, detail)
}

func (r *projectAutomaticAnalysisResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectAutomaticAnalysisModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization := state.Organization.ValueString()
	key := state.ProjectKey.ValueString()

	const subject = "the Automatic Analysis setting of this project"
	if _, _, ok := refreshProject(ctx, r.client, organization, key, subject, resp); !ok {
		return
	}
	enabled, err := r.client.GetProjectAutomaticAnalysis(ctx, key)
	if errors.Is(err, client.ErrNotFound) {
		// The project was deleted after the read above.
		forgetMissingProject(ctx, key, subject, resp)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(automaticAnalysisReadError+key, err.Error())
		return
	}

	state.ID = types.StringValue(organization + "/" + key)
	state.Enabled = types.BoolValue(enabled)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectAutomaticAnalysisResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectAutomaticAnalysisModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning("The Automatic Analysis setting was not changed",
		"Terraform forgot the Automatic Analysis setting of the project "+state.ProjectKey.ValueString()+
			". The project keeps its current setting.")
}

// ImportState takes "organization/project_key". Read fills in the rest.
func (r *projectAutomaticAnalysisResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importProjectScoped(ctx, "Automatic Analysis setting", req, resp)
}
