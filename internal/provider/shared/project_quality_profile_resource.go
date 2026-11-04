package shared

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/validate"
)

var (
	_ resource.Resource                = &projectQualityProfileResource{}
	_ resource.ResourceWithConfigure   = &projectQualityProfileResource{}
	_ resource.ResourceWithModifyPlan  = &projectQualityProfileResource{}
	_ resource.ResourceWithImportState = &projectQualityProfileResource{}
)

// NewProjectQualityProfileResource returns the
// sonarqube_project_quality_profile resource.
func NewProjectQualityProfileResource() resource.Resource { return &projectQualityProfileResource{} }

type projectQualityProfileResource struct{ client *client.Client }

type projectQualityProfileResourceModel struct {
	ID                types.String `tfsdk:"id"`
	Organization      types.String `tfsdk:"organization"`
	ProjectKey        types.String `tfsdk:"project_key"`
	QualityProfileKey types.String `tfsdk:"quality_profile_key"`
	Language          types.String `tfsdk:"language"`
}

func projectQualityProfileID(organization, project, language string) types.String {
	return types.StringValue(organization + "/" + project + "/" + language)
}

func (r *projectQualityProfileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_quality_profile"
}

func (r *projectQualityProfileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "This resource sets the quality profile that a project uses for the language of the " +
			"profile. A project uses the default profile of the organization for each language that has no such " +
			"assignment. Use one resource for each language of the project. Do not use two resources for the " +
			"same project and language: each apply then changes the assignment of the other.\n\n" +
			"A change of `quality_profile_key` to a different profile of the same language changes the assignment " +
			"in place. A change to a profile of a different language replaces the resource: the project then uses " +
			"the default profile again for the earlier language. When the plan does not know the key yet, for " +
			"example for a profile that the same apply creates, the plan shows an update. The apply then also " +
			"removes the assignment of the earlier language.\n\n" +
			"When someone changes the profile of the project outside of Terraform, the next plan shows the change. " +
			"When the profile is deleted, the project uses the default profile again. If the configuration still " +
			"names the key of the deleted profile, the next plan fails, because the profile does not exist.\n\n" +
			"A destroy makes the project use the default profile of the organization for the language again.\n\n" +
			"## Permissions\n\n" +
			"The token needs one of these permissions: Administer Quality Profiles on the organization, the " +
			"permission to edit the profile, or Administer on the project. SonarQube Cloud permits this resource " +
			"only on the Team and the Enterprise plans. When the organization moves to a plan without custom " +
			"profiles, SonarQube Cloud refuses to remove the assignment. A destroy then only removes the resource " +
			"from the state, with a warning.\n\n" +
			"## Import\n\n" +
			"Import an assignment with `<organization>/<project_key>/<language>`. When the project uses the default " +
			"profile for the language, the import gets the key of the default profile.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "`<organization>/<project_key>/<language>`."},
			"organization": schema.StringAttribute{Required: true, PlanModifiers: replace,
				Description: "Key of the organization that owns the project and the profile.",
				Validators:  validate.OrganizationKey()},
			"project_key": schema.StringAttribute{Required: true, PlanModifiers: replace,
				Description: "Key of the project.", Validators: validate.ProjectKey()},
			"quality_profile_key": schema.StringAttribute{Required: true,
				Description: "Key of the quality profile, for example the `key` of a `sonarqube_quality_profile` " +
					"resource or data source. A built-in profile is permitted.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"language": schema.StringAttribute{Computed: true,
				Description: "Key of the language of the profile, such as `java`."},
		},
	}
}

func (r *projectQualityProfileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_project_quality_profile resource assigns a quality profile")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

// ModifyPlan computes the language of the profile, so that a profile of a
// different language replaces the resource. add_project would keep the
// assignment of the earlier language. When the key is not known yet, for
// example for a profile of the same apply, the language stays unknown, and
// Update removes the earlier assignment.
func (r *projectQualityProfileResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.client == nil {
		return
	}
	var plan, state projectQualityProfileResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.QualityProfileKey.IsUnknown() || plan.Organization.IsUnknown() {
		plan.Language, plan.ID = types.StringUnknown(), types.StringUnknown()
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}
	if !req.State.Raw.IsNull() && plan.QualityProfileKey.Equal(state.QualityProfileKey) &&
		plan.Organization.Equal(state.Organization) && plan.ProjectKey.Equal(state.ProjectKey) {
		plan.Language, plan.ID = state.Language, state.ID
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}
	language, ok := r.profileLanguage(ctx, plan, &resp.Diagnostics)
	if !ok {
		return
	}
	plan.Language = types.StringValue(language)
	plan.ID = types.StringUnknown()
	if !plan.ProjectKey.IsUnknown() {
		plan.ID = projectQualityProfileID(plan.Organization.ValueString(), plan.ProjectKey.ValueString(), language)
	}
	if !req.State.Raw.IsNull() && !state.Language.Equal(plan.Language) {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("quality_profile_key"))
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

// profileLanguage finds the profile in the organization. add_project would
// accept a profile of a different organization only to refuse it, so this
// gives a clearer error.
func (r *projectQualityProfileResource) profileLanguage(ctx context.Context, plan projectQualityProfileResourceModel, diagnostics *diag.Diagnostics) (string, bool) {
	organization, key := plan.Organization.ValueString(), plan.QualityProfileKey.ValueString()
	profile, err := r.client.GetQualityProfile(ctx, organization, "", key)
	switch {
	case errors.Is(err, client.ErrNotFound):
		diagnostics.AddAttributeError(path.Root("quality_profile_key"), "Quality profile "+key+" not found",
			"No quality profile with this key is in the organization "+organization+". Check the key.")
		return "", false
	case err != nil:
		diagnostics.AddError("Cannot read the quality profile "+key, organizationErrorDetail(err, organization))
		return "", false
	}
	return profile.Language, true
}

func (r *projectQualityProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectQualityProfileResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !r.assign(ctx, &plan, &resp.Diagnostics) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// assign sets the profile of the plan and fills in the language and the id.
// ModifyPlan gives the language when it knows the key, so only an unknown
// language needs a lookup.
func (r *projectQualityProfileResource) assign(ctx context.Context, plan *projectQualityProfileResourceModel, diagnostics *diag.Diagnostics) bool {
	language := plan.Language.ValueString()
	if plan.Language.IsUnknown() || plan.Language.IsNull() {
		var ok bool
		if language, ok = r.profileLanguage(ctx, *plan, diagnostics); !ok {
			return false
		}
	}
	organization, project, key := plan.Organization.ValueString(), plan.ProjectKey.ValueString(), plan.QualityProfileKey.ValueString()
	if err := r.client.AddQualityProfileProject(ctx, key, project); err != nil {
		diagnostics.AddError("Cannot assign the quality profile "+key+" to the project "+project, projectErrorDetail(err, organization, project))
		return false
	}
	plan.Language = types.StringValue(language)
	plan.ID = projectQualityProfileID(organization, project, language)
	return true
}

// Read gets the profile that the project uses for the language. It can be a
// different profile than the state, also the default profile, and then the
// plan shows the change.
func (r *projectQualityProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectQualityProfileResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization, project := state.Organization.ValueString(), state.ProjectKey.ValueString()
	profile, err := r.client.GetProjectQualityProfile(ctx, organization, project, state.Language.ValueString())
	switch {
	case errors.Is(err, client.ErrOrganizationNotFound):
		resp.Diagnostics.AddWarning(organizationNotFound(organization),
			"Terraform removes the quality profile assignment from the state. The organization was deleted, or "+
				"the token cannot read it any more.")
		resp.State.RemoveResource(ctx)
	case errors.Is(err, client.ErrProjectNotFound), errors.Is(err, client.ErrNotFound):
		resp.State.RemoveResource(ctx)
	case err != nil:
		resp.Diagnostics.AddError("Cannot read the quality profile of the project "+project, err.Error())
	default:
		state.QualityProfileKey = types.StringValue(profile.Key)
		state.ID = projectQualityProfileID(organization, project, state.Language.ValueString())
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	}
}

// Update assigns the new profile. When the plan did not know the language of
// the profile, the language can change here. add_project keeps the
// assignment of the earlier language, so Update removes it.
func (r *projectQualityProfileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectQualityProfileResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !r.assign(ctx, &plan, &resp.Diagnostics) {
		return
	}
	if !plan.Language.Equal(state.Language) {
		project, key := state.ProjectKey.ValueString(), state.QualityProfileKey.ValueString()
		if err := r.client.RemoveQualityProfileProject(ctx, key, project); err != nil {
			// The state keeps the earlier assignment, so that the next apply
			// replaces it and removes it again.
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			resp.Diagnostics.AddError("Cannot remove the earlier quality profile "+key+" from the project "+project,
				err.Error()+"\n\nThe project still uses this profile for the language "+state.Language.ValueString()+
					". The next apply tries again.")
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete makes the project use the default profile again. When the plan of
// the organization no longer permits custom profiles, remove_project refuses
// the call. Then Delete only removes the resource from the state, so that
// the plan does not block the destroy.
func (r *projectQualityProfileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectQualityProfileResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, key := state.ProjectKey.ValueString(), state.QualityProfileKey.ValueString()
	err := r.client.RemoveQualityProfileProject(ctx, key, project)
	switch {
	case errors.Is(err, client.ErrQualityProfilePlanRefused):
		resp.Diagnostics.AddWarning("The quality profile "+key+" can stay assigned to the project "+project,
			err.Error()+"\n\nTerraform removes the resource from the state.")
	case err != nil:
		resp.Diagnostics.AddError("Cannot remove the quality profile "+key+" from the project "+project, err.Error())
	}
}

// ImportState takes "<organization>/<project_key>/<language>". The three keys
// have no "/". Read fills in the profile.
func (r *projectQualityProfileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || !validate.OrganizationKeyPattern.MatchString(parts[0]) ||
		!validate.ProjectKeyPattern.MatchString(parts[1]) || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid project quality profile import ID", "Use <organization>/<project_key>/<language>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_key"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("language"), parts[2])...)
}

// projectErrorDetail explains a missing project. The client already explains
// the other refusals.
func projectErrorDetail(err error, organization, project string) string {
	if errors.Is(err, client.ErrProjectNotFound) {
		return "No project " + project + " is in the organization " + organization + ". Check the key, and check " +
			"that the token can read the project."
	}
	return err.Error()
}
