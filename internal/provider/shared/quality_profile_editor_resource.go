package shared

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
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
	_ resource.Resource                     = &qualityProfileEditorResource{}
	_ resource.ResourceWithConfigure        = &qualityProfileEditorResource{}
	_ resource.ResourceWithImportState      = &qualityProfileEditorResource{}
	_ resource.ResourceWithConfigValidators = &qualityProfileEditorResource{}
)

// NewQualityProfileEditorResource returns the sonarqube_quality_profile_editor
// resource.
func NewQualityProfileEditorResource() resource.Resource { return &qualityProfileEditorResource{} }

type qualityProfileEditorResource struct{ client *client.Client }

type qualityProfileEditorResourceModel struct {
	ID                types.String `tfsdk:"id"`
	Organization      types.String `tfsdk:"organization"`
	QualityProfileKey types.String `tfsdk:"quality_profile_key"`
	GroupName         types.String `tfsdk:"group_name"`
	Login             types.String `tfsdk:"login"`
}

func (m qualityProfileEditorResourceModel) editor() client.PermissionPrincipal {
	if !m.GroupName.IsNull() {
		return client.PermissionPrincipal{Group: true, Name: m.GroupName.ValueString()}
	}
	return client.PermissionPrincipal{Name: m.Login.ValueString()}
}

// editorName names the editor in diagnostics.
func (m qualityProfileEditorResourceModel) editorName() string {
	editor := m.editor()
	return "the " + editor.Kind() + " " + editor.Name
}

func (m qualityProfileEditorResourceModel) importID() string {
	editor := m.editor()
	return m.Organization.ValueString() + "/" + m.QualityProfileKey.ValueString() + "/" + editor.Kind() + "/" + editor.Name
}

func (r *qualityProfileEditorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_quality_profile_editor"
}

func (r *qualityProfileEditorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "This resource lets one group or one user edit one custom quality profile. The editor can " +
			"then change the rules, the projects, the name and the parent of the profile, and let other groups and " +
			"users edit it too. The editor does not need the Administer Quality Profiles permission. Destroy takes " +
			"the right from the editor. A change to any attribute replaces the resource.\n\n" +
			"A rename of the profile does not change the resource. When someone takes the right from the editor " +
			"outside of Terraform, or deletes the profile, the next plan shows that the resource must be created " +
			"again.\n\n" +
			"## Requirements\n\n" +
			"Built-in profiles cannot have editors. A user must be a member of the organization. The token needs " +
			"the Administer Quality Profiles permission, or the right to edit the profile.\n\n" +
			"SonarQube Cloud permits editors only on the Team and the Enterprise plans. On a different plan, a " +
			"refresh does not find the editor, and the next apply fails.\n\n" +
			"This resource uses web services that SonarQube Cloud marks as internal.\n\n" +
			"## Import\n\n" +
			"Import a group with `<organization>/<quality_profile_key>/group/<group_name>`, and a user with " +
			"`<organization>/<quality_profile_key>/user/<login>`. The group name can contain `/`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "The import ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"organization": schema.StringAttribute{Required: true, PlanModifiers: replace,
				Description: "Key of the organization that owns the profile.", Validators: validate.OrganizationKey()},
			"quality_profile_key": schema.StringAttribute{Required: true, PlanModifiers: replace,
				Description: "Key of a custom quality profile, for example the `key` of a `sonarqube_quality_profile`.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1), stringvalidator.RegexMatches(
					regexp.MustCompile(`^[^/]+$`), "must not contain /")}},
			"group_name": schema.StringAttribute{Optional: true, PlanModifiers: replace,
				Description: "Name of the group. Use the same case as the group, so that the configuration and SonarQube " +
					"Cloud show the same name. Set exactly one of `group_name` and `login`.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 255)}},
			"login": schema.StringAttribute{Optional: true, PlanModifiers: replace,
				Description: "Login of the user, with the same case. Set exactly one of `group_name` and `login`.",
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)}},
		},
	}
}

func (r *qualityProfileEditorResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{resourcevalidator.ExactlyOneOf(path.MatchRoot("group_name"), path.MatchRoot("login"))}
}

func (r *qualityProfileEditorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_quality_profile_editor resource manages an editor")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

// errBuiltInProfile reports a built-in profile, which cannot have editors.
var errBuiltInProfile = errors.New("the profile is built-in, and a built-in profile cannot have editors. Use a custom profile")

// withProfile reads the name and the language of the profile from its key,
// and runs call with them. The editor web services name the profile, so a
// rename between the lookup and the call gives a 404. Then withProfile reads
// the profile again and runs call once more. A missing profile from the
// lookup matches client.ErrNotFound. A 404 of the second call does not, so
// that a caller does not take it for a deleted profile.
func (r *qualityProfileEditorResource) withProfile(ctx context.Context, m qualityProfileEditorResourceModel,
	call func(client.QualityProfile) error) error {
	for attempt := 1; ; attempt++ {
		profile, err := r.client.GetQualityProfile(ctx, m.Organization.ValueString(), "", m.QualityProfileKey.ValueString())
		if err != nil {
			return err
		}
		err = call(*profile)
		if !errors.Is(err, client.ErrNotFound) {
			return err
		}
		if attempt == 2 {
			return fmt.Errorf("the profile changed its name during the call: %s", err.Error())
		}
	}
}

func (r *qualityProfileEditorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan qualityProfileEditorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization, key := plan.Organization.ValueString(), plan.QualityProfileKey.ValueString()
	err := r.withProfile(ctx, plan, func(profile client.QualityProfile) error {
		if profile.IsBuiltIn {
			return errBuiltInProfile
		}
		return r.client.AddQualityProfileEditor(ctx, organization, profile, plan.editor())
	})
	summary := "Cannot let " + plan.editorName() + " edit the quality profile " + key
	switch {
	case errors.Is(err, client.ErrNotFound):
		resp.Diagnostics.AddAttributeError(path.Root("quality_profile_key"), summary,
			"No quality profile with this key is in the organization "+organization+". Check the key.")
	case errors.Is(err, errBuiltInProfile):
		resp.Diagnostics.AddAttributeError(path.Root("quality_profile_key"), summary, err.Error()+".")
	case err != nil:
		resp.Diagnostics.AddError(summary, organizationErrorDetail(err, organization))
	default:
		plan.ID = types.StringValue(plan.importID())
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

func (r *qualityProfileEditorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state qualityProfileEditorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization := state.Organization.ValueString()
	found := false
	err := r.withProfile(ctx, state, func(profile client.QualityProfile) (err error) {
		found, err = r.client.HasQualityProfileEditor(ctx, organization, profile, state.editor())
		return err
	})
	switch {
	case errors.Is(err, client.ErrOrganizationNotFound):
		resp.Diagnostics.AddWarning(organizationNotFound(organization),
			"Terraform removes the quality profile editor from the state. The organization was deleted, or "+
				"the token cannot read it any more.")
		resp.State.RemoveResource(ctx)
	case errors.Is(err, client.ErrNotFound), err == nil && !found:
		resp.State.RemoveResource(ctx)
	case err != nil:
		resp.Diagnostics.AddError("Cannot read the editors of the quality profile "+state.QualityProfileKey.ValueString(), err.Error())
	default:
		state.ID = types.StringValue(state.importID())
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	}
}

// Update is never called with a change, because each attribute replaces the
// resource.
func (r *qualityProfileEditorResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Cannot update a quality profile editor",
		"Each attribute of sonarqube_quality_profile_editor replaces the resource. This is a fault in the provider.")
}

// Delete takes the right from the editor. A missing organization or profile
// has no editors to remove.
func (r *qualityProfileEditorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state qualityProfileEditorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.withProfile(ctx, state, func(profile client.QualityProfile) error {
		return r.client.RemoveQualityProfileEditor(ctx, state.Organization.ValueString(), profile, state.editor())
	})
	if err == nil || errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrOrganizationNotFound) {
		return
	}
	resp.Diagnostics.AddError("Cannot take the right to edit the quality profile "+state.QualityProfileKey.ValueString()+
		" from "+state.editorName(), err.Error())
}

// ImportState takes "<organization>/<quality_profile_key>/group/<group_name>"
// or "<organization>/<quality_profile_key>/user/<login>". An organization key
// and a profile key have no "/", but a group name can.
func (r *qualityProfileEditorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 4)
	if len(parts) != 4 || !validate.OrganizationKeyPattern.MatchString(parts[0]) || parts[1] == "" || parts[3] == "" ||
		(parts[2] != "group" && parts[2] != "user") {
		resp.Diagnostics.AddError("Invalid quality profile editor import ID",
			"Use <organization>/<quality_profile_key>/group/<group_name> or <organization>/<quality_profile_key>/user/<login>.")
		return
	}
	principal := "group_name"
	if parts[2] == "user" {
		principal = "login"
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("quality_profile_key"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(principal), parts[3])...)
}
