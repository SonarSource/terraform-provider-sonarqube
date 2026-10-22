package shared

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                = &qualityProfileResource{}
	_ resource.ResourceWithConfigure   = &qualityProfileResource{}
	_ resource.ResourceWithImportState = &qualityProfileResource{}
)

// NewQualityProfileResource returns the sonarqube_quality_profile resource.
func NewQualityProfileResource() resource.Resource { return &qualityProfileResource{} }

type qualityProfileResource struct{ client *client.Client }

type qualityProfileResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Key          types.String `tfsdk:"key"`
	Organization types.String `tfsdk:"organization"`
	Language     types.String `tfsdk:"language"`
	Name         types.String `tfsdk:"name"`
	ParentKey    types.String `tfsdk:"parent_key"`
}

func (r *qualityProfileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_quality_profile"
}

func (r *qualityProfileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	computed := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "This resource manages a custom quality profile of one language. A profile can extend one " +
			"parent profile, for example a built-in profile such as `Sonar way core`. Then it gets the rules of " +
			"the parent. A change to `name` or `parent_key` changes the profile in place. A change to " +
			"`organization` or `language` replaces it.\n\n" +
			"## Built-in profiles\n\n" +
			"The built-in profiles are read-only, and the import refuses them. Use the `sonarqube_quality_profile` " +
			"data source to get the key of a built-in profile, for example to use it as `parent_key`.\n\n" +
			"## Delete\n\n" +
			"A delete also deletes all the profiles that extend the profile. Terraform does not know about this. " +
			"When a profile of the configuration extends a deleted profile, the next plan creates that profile " +
			"again. When the configuration sets `parent_key` to the `key` of a different resource, Terraform " +
			"destroys the child profile first.\n\n" +
			"A replacement of the parent also deletes the child on the server, but the plan only changes the " +
			"`parent_key` of the child, so the apply fails. Add `lifecycle { replace_triggered_by = " +
			"[sonarqube_quality_profile.<parent>.key] }` to the child, so that Terraform replaces it together " +
			"with the parent. Refer to `key`, not to the whole resource: a reference to the whole resource also " +
			"replaces the child when the parent only gets a new name or parent.\n\n" +
			"SonarQube Cloud refuses to delete a profile when the profile or one of its descendants is the " +
			"default profile of the language. Set a different default profile first.\n\n" +
			"## Combined quality profile groups\n\n" +
			"SonarQube Cloud refuses to change the parent of a profile or to delete it, when the profile is a " +
			"member of a combined quality profile group. Manage such a profile through the group.\n\n" +
			"## Import\n\n" +
			"Import a profile with `<organization>/<language>/<name>`. The name can contain `/`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Key of the profile.", PlanModifiers: computed},
			"key": schema.StringAttribute{Computed: true, PlanModifiers: computed,
				Description: "Key of the profile. Use it as the `parent_key` of a different profile."},
			"organization": schema.StringAttribute{Required: true, Description: "Key of the organization that owns the profile.",
				Validators: validate.OrganizationKey(), PlanModifiers: replace},
			"language": schema.StringAttribute{Required: true, PlanModifiers: replace,
				Description: "Key of the language of the profile, such as `java`, `py` or `js`. SonarQube Cloud " +
					"refuses a key that is not valid, and its error gives the valid keys.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"name": schema.StringAttribute{Required: true,
				Description: "Name of the profile. The names of the profiles of a language must be different. " +
					"Names are case-sensitive.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 100)}},
			"parent_key": schema.StringAttribute{Optional: true,
				Description: "Key of the profile that this profile extends. The parent must have the same language. " +
					"When not set, the profile extends no other profile.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		},
	}
}

func (r *qualityProfileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_quality_profile resource manages a quality profile")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

// Create creates the profile and then sets its parent: no web service does
// both. When the parent cannot be set, Create deletes the profile again, so
// that no profile without its parent stays. When that delete also fails, the
// state keeps the profile without a parent. Terraform then marks it as
// tainted, and the next apply replaces it.
func (r *qualityProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan qualityProfileResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization, name := plan.Organization.ValueString(), plan.Name.ValueString()
	profile, err := r.client.CreateQualityProfile(ctx, organization, plan.Language.ValueString(), name)
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the quality profile "+name, organizationErrorDetail(err, organization))
		return
	}
	plan.ID, plan.Key = types.StringValue(profile.Key), types.StringValue(profile.Key)
	if parentKey := plan.ParentKey.ValueString(); parentKey != "" {
		if err := r.client.ChangeQualityProfileParent(ctx, profile.Key, parentKey); err != nil {
			summary := "Cannot set the parent of the quality profile " + name
			if deleteErr := r.client.DeleteQualityProfile(ctx, profile.Key); deleteErr != nil {
				plan.ParentKey = types.StringNull()
				resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
				resp.Diagnostics.AddError(summary, err.Error()+"\n\nThe provider could not delete the new profile, so "+
					"the state keeps it without a parent. The next apply replaces it: "+deleteErr.Error())
				return
			}
			resp.Diagnostics.AddError(summary, err.Error()+"\n\nThe provider deleted the new profile.")
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *qualityProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state qualityProfileResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization := state.Organization.ValueString()
	profile, err := r.client.GetQualityProfile(ctx, organization, state.Language.ValueString(), state.Key.ValueString())
	switch {
	case errors.Is(err, client.ErrOrganizationNotFound):
		resp.Diagnostics.AddWarning(organizationNotFound(organization),
			"Terraform removes the quality profile from the state. The organization was deleted, or "+
				"the token cannot read it any more.")
		resp.State.RemoveResource(ctx)
	case errors.Is(err, client.ErrNotFound):
		resp.State.RemoveResource(ctx)
	case err != nil:
		resp.Diagnostics.AddError("Cannot read the quality profile "+state.Name.ValueString(), err.Error())
	default:
		state.Name = types.StringValue(profile.Name)
		state.ParentKey = optionalString(profile.ParentKey)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	}
}

// Update renames the profile and changes its parent, each only when it
// changes. When the parent change fails after a rename, the state keeps the
// new name.
//
// A replacement of the parent deletes this profile on the server, but the
// plan only changes its parent_key. The update then finds no profile.
func (r *qualityProfileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state qualityProfileResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key := state.Key.ValueString()
	if !plan.Name.Equal(state.Name) {
		if err := r.client.RenameQualityProfile(ctx, key, plan.Name.ValueString()); err != nil {
			resp.Diagnostics.AddError("Cannot rename the quality profile "+state.Name.ValueString(), r.updateErrorDetail(ctx, state, err))
			return
		}
		state.Name = plan.Name
	}
	if !plan.ParentKey.Equal(state.ParentKey) {
		if err := r.client.ChangeQualityProfileParent(ctx, key, plan.ParentKey.ValueString()); err != nil {
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			resp.Diagnostics.AddError("Cannot change the parent of the quality profile "+plan.Name.ValueString(), r.updateErrorDetail(ctx, state, err))
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *qualityProfileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state qualityProfileResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteQualityProfile(ctx, state.Key.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot delete the quality profile "+state.Name.ValueString(), err.Error())
	}
}

// ImportState takes "<organization>/<language>/<name>". An organization key
// and a language key have no "/", but a profile name can. Read fills in the
// name and the parent.
func (r *qualityProfileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 3)
	if len(parts) != 3 || !validate.OrganizationKeyPattern.MatchString(parts[0]) || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid quality profile import ID", "Use <organization>/<language>/<name>.")
		return
	}
	organization, language, name := parts[0], parts[1], parts[2]
	profile, err := r.client.FindQualityProfileByName(ctx, organization, language, name)
	if err != nil {
		resp.Diagnostics.AddError("Cannot import the quality profile "+name, profileLookupDetail(err, organization, language))
		return
	}
	if profile.IsBuiltIn {
		resp.Diagnostics.AddError("Cannot import the built-in quality profile "+name,
			"Built-in profiles are read-only. Use the sonarqube_quality_profile data source to read it.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), profile.Key)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), profile.Key)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("language"), language)...)
}

// updateErrorDetail explains a 404 of an update when the profile itself is
// gone. A 404 of change_parent can also name a parent that does not exist, so
// the profile is read again to tell the two apart.
func (r *qualityProfileResource) updateErrorDetail(ctx context.Context, state qualityProfileResourceModel, err error) string {
	if !errors.Is(err, client.ErrNotFound) {
		return err.Error()
	}
	_, readErr := r.client.GetQualityProfile(ctx, state.Organization.ValueString(), state.Language.ValueString(), state.Key.ValueString())
	if !errors.Is(readErr, client.ErrNotFound) {
		return err.Error()
	}
	return "The profile does not exist any more. SonarQube Cloud deletes a profile together with its parent, " +
		"for example when Terraform replaces the parent. Run the apply again: the refresh removes the profile " +
		"from the state, and the apply creates it again. To replace the profile together with its parent in " +
		"one apply, add lifecycle { replace_triggered_by = [<parent resource>.key] } to it."
}

// profileLookupDetail explains why a lookup by name found no profile, for
// import and for the data source.
func profileLookupDetail(err error, organization, language string) string {
	if errors.Is(err, client.ErrNotFound) {
		return "No " + language + " quality profile with this name is in the organization " + organization +
			". Check the name. The name must match exactly, with the same case."
	}
	return organizationErrorDetail(err, organization)
}
