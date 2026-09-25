package provider

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

var (
	_ resource.Resource                = &organizationBindingResource{}
	_ resource.ResourceWithConfigure   = &organizationBindingResource{}
	_ resource.ResourceWithImportState = &organizationBindingResource{}
)

// NewOrganizationBindingResource returns the sonarqube_organization_binding
// resource.
func NewOrganizationBindingResource() resource.Resource {
	return &organizationBindingResource{}
}

type organizationBindingResource struct {
	client *client.Client
}

type organizationBindingModel struct {
	ID                    types.String `tfsdk:"id"`
	OrganizationKey       types.String `tfsdk:"organization_key"`
	OrganizationID        types.String `tfsdk:"organization_id"`
	OrganizationUUIDV4    types.String `tfsdk:"organization_uuid_v4"`
	DevOpsPlatform        types.String `tfsdk:"dev_ops_platform"`
	InstallationID        types.String `tfsdk:"installation_id"`
	DevOpsPlatformURL     types.String `tfsdk:"dev_ops_platform_url"`
	RepoAutoImportEnabled types.Bool   `tfsdk:"repo_auto_import_enabled"`
	DopFlavor             types.String `tfsdk:"dop_flavor"`
	BindingType           types.String `tfsdk:"binding_type"`
}

func (r *organizationBindingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_binding"
}

func (r *organizationBindingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Binds a SonarQube Cloud organization to an organization on a DevOps " +
			"platform. Only github.com is supported.\n\n" +
			"~> **The API cannot remove a binding.** `terraform destroy` drops this resource " +
			"from the state and gives a warning, but the binding stays. The server removes a " +
			"binding only when it deletes the organization, because that also removes the " +
			"records that this API writes.\n\n" +
			"To destroy an organization and its binding together is therefore correct. " +
			"Terraform deletes the binding before the organization that it depends on.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Identifier of the binding.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_key": schema.StringAttribute{
				Required: true,
				Description: "Key of the SonarQube Cloud organization to bind. The provider " +
					"reads the organization to find the internal identifier that the bindings " +
					"API needs.",
				// A different organization is a different binding. The
				// organization that was bound before stays bound, because a
				// binding cannot be deleted; the destroy says so.
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"dev_ops_platform": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(client.PlatformGitHub),
				Description: "DevOps platform to bind to. Only `" + client.PlatformGitHub +
					"` is supported, and that is the default.",
				Validators: []validator.String{
					stringvalidator.OneOf(client.PlatformGitHub),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"installation_id": schema.StringAttribute{
				Required: true,
				Description: "Identifier of the GitHub application installation. It is the last " +
					"segment of the address of the installed application on GitHub.\n\n" +
					"The installation must belong to the GitHub application of this SonarQube " +
					"Cloud instance. Read the `sonarqube_dop_applications` data source to find " +
					"that application.\n\n" +
					"~> This cannot change after the binding exists. The API allows a change " +
					"for a binding to GitHub Enterprise only. To replace the resource does not " +
					"help, because a binding cannot be deleted.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{immutableInstallationIDPlanModifier{}},
			},
			"repo_auto_import_enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Description: "Whether repositories are imported automatically.\n\n" +
					"~> The server accepts this for GitHub only, and only while the matching " +
					"feature is on for the organization. It writes `false` otherwise, and the " +
					"provider then reports which value the server holds.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"organization_id": schema.StringAttribute{
				Computed:      true,
				Description:   "Internal identifier of the bound organization.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_uuid_v4": schema.StringAttribute{
				Computed:      true,
				Description:   "Identifier of the bound organization in the form of a UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"dev_ops_platform_url": schema.StringAttribute{
				Computed: true,
				Description: "Address of the organization on the DevOps platform, as the server " +
					"reports it. The bind call takes no address, so this is read only.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"dop_flavor": schema.StringAttribute{
				Computed: true,
				Description: "Variant of the binding, such as `GITHUB_GHEC_DR`. Holds no value " +
					"for a binding to github.com. This provider cannot make a binding to GitHub " +
					"Enterprise, but it reports the variant of one that was made elsewhere.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"binding_type": schema.StringAttribute{
				Computed:      true,
				Description:   "Type of the binding, as the API reports it.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *organizationBindingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := requireCloudClient(req.ProviderData,
		"The sonarqube_organization_binding resource binds an organization")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *organizationBindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationKey := plan.OrganizationKey.ValueString()

	org, ok := resolveOrganization(ctx, r.client, organizationKey, &resp.Diagnostics)
	if !ok {
		return
	}

	binding, err := r.client.CreateOrganizationBinding(ctx, client.CreateBindingRequest{
		OrganizationID:        org.ID,
		DevOpsPlatform:        plan.DevOpsPlatform.ValueString(),
		InstallationID:        plan.InstallationID.ValueString(),
		RepoAutoImportEnabled: boolIfSet(plan.RepoAutoImportEnabled),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Cannot bind the organization "+organizationKey,
			err.Error()+installationHint(err),
		)
		return
	}

	// The binding exists from here on, and nothing can delete it. Record it
	// before anything else can fail, or Terraform would not know a binding
	// that the organization now carries.
	state := stateFromBinding(binding, plan)

	if autoImportRefused(plan, binding) {
		// Keep the value that the plan asked for, and warn. A create that
		// ends with an error marks the object tainted, and the next plan
		// replaces a tainted object: the delete would remove nothing, the
		// organization would stay bound, and the new bind would be refused.
		// The binding would then leave the state for good, which is the very
		// thing that the lines above prevent.
		//
		// The next refresh reads the value that the server holds, reports the
		// difference, and plans an update. An error from an update taints
		// nothing.
		state.RepoAutoImportEnabled = plan.RepoAutoImportEnabled
		resp.Diagnostics.AddAttributeWarning(
			path.Root("repo_auto_import_enabled"),
			"The server refused the automatic import of repositories",
			autoImportRefusedDetail(plan, binding)+"\n\n"+
				"The binding itself was written. The next plan reports this difference and "+
				"tries to correct it.",
		)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *organizationBindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	binding, err := r.client.GetOrganizationBinding(ctx, id)
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			// A binding that the token may not see gives the same answer.
			// Record it as gone. The next plan then binds the organization
			// again. If the token lost its permission, that bind fails on an
			// organization that is already bound, and the message of that
			// failure gives the reason.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Cannot read the organization binding "+id, err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, stateFromBinding(binding, state))...)
}

func (r *organizationBindingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state organizationBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A computed attribute that keeps its prior value is not part of the
	// plan, so the identifier comes from the state.
	id := state.ID.ValueString()

	// Only the fields that the change action takes go with the request. The
	// installation is not one of them, and the plan already refused a change
	// to it.
	binding, err := r.client.UpdateOrganizationBinding(ctx, id, client.PatchBindingRequest{
		RepoAutoImportEnabled: boolIfSet(plan.RepoAutoImportEnabled),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot update the organization binding "+id, err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, stateFromBinding(binding, plan))...)
	if resp.Diagnostics.HasError() {
		return
	}

	if autoImportRefused(plan, binding) {
		// An update reports this as an error, unlike a create. The object is
		// already in the state and an error from an update does not taint it,
		// so the apply stops loudly and the binding stays where Terraform can
		// manage it.
		resp.Diagnostics.AddAttributeError(
			path.Root("repo_auto_import_enabled"),
			"The server refused the automatic import of repositories",
			autoImportRefusedDetail(plan, binding),
		)
	}
}

// Delete takes the binding out of the state, and nothing else.
//
// The API has no operation to remove a binding. The server removes the
// records that it writes when it deletes the organization.
//
// A configuration that holds the binding and the organization is therefore
// correct. Terraform deletes this resource first, then the organization that
// it depends on. The delete of the organization removes the binding.
func (r *organizationBindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationKey := state.OrganizationKey.ValueString()

	resp.Diagnostics.AddWarning(
		"The organization binding was not removed",
		"The API has no operation to remove a binding, so the organization "+organizationKey+
			" stays bound. Terraform has forgotten the binding, but the binding is still "+
			"there.\n\n"+
			"A binding goes away when its organization is deleted, or when the application is "+
			"removed on the side of the DevOps platform. If this destroy also deletes the "+
			"organization, the binding goes with it and nothing stays behind.",
	)
}

// ImportState takes the key of the organization, not the identifier of the
// binding. Nobody knows that identifier before the import, and the
// organization key is what the configuration already holds.
func (r *organizationBindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organizationKey := req.ID

	org, ok := resolveOrganization(ctx, r.client, organizationKey, &resp.Diagnostics)
	if !ok {
		return
	}

	binding, ok := findOrganizationBinding(ctx, r.client, org,
		"There is nothing to import.", &resp.Diagnostics)
	if !ok {
		return
	}

	// This resource binds to github.com only, and its schema says so. A
	// binding to another platform carries no installation, and the resource
	// requires one, so the import must stop here instead of writing a state
	// that no plan can answer.
	if binding.DevOpsPlatform != client.PlatformGitHub {
		resp.Diagnostics.AddError(
			"The organization "+organizationKey+" is bound to "+binding.DevOpsPlatform,
			"This resource manages a binding to "+client.PlatformGitHub+" only. Read the "+
				"sonarqube_organization_binding data source to see this binding.",
		)
		return
	}

	// Read fills in everything else from the identifier.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), binding.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_key"), organizationKey)...)
}

// autoImportRefused reports a repository import that the server did not turn
// on.
//
// The server writes false unless the platform is GitHub and the matching
// feature is on for the organization.
func autoImportRefused(plan organizationBindingModel, binding *client.OrganizationBinding) bool {
	asked := plan.RepoAutoImportEnabled
	if asked.IsNull() || asked.IsUnknown() || binding.RepoAutoImportEnabled == nil {
		return false
	}
	return asked.ValueBool() != *binding.RepoAutoImportEnabled
}

// autoImportRefusedDetail names the cause. Terraform reports a value that does
// not answer the configuration as an inconsistent result, which names no
// cause, so the provider says the cause first.
func autoImportRefusedDetail(plan organizationBindingModel, binding *client.OrganizationBinding) string {
	return "The configuration asks for repo_auto_import_enabled = " +
		strconv.FormatBool(plan.RepoAutoImportEnabled.ValueBool()) + ", and the server holds " +
		strconv.FormatBool(*binding.RepoAutoImportEnabled) + ".\n\n" +
		"The server accepts this for GitHub only, and only while the matching feature is on " +
		"for the organization.\n\n" +
		"Ask for the feature for this organization, or take repo_auto_import_enabled out of " +
		"the configuration."
}

// installationHint explains the most confusing failure of the bind call.
//
// For a binding to github.com the server looks the installation up in its own
// records. It does not ask GitHub, and those records hold its own application
// only. An installation of a different GitHub application therefore gives an
// identifier that looks correct but means nothing to the server. The message
// of the API does not give the reason.
//
// Every 404 gets the hint. The caller reads the organization before it binds,
// so the organization is there, and the installation is the only other thing
// that this call looks for. A test of the text of the message would stop the
// hint silently if the server changed one word.
func installationHint(err error) string {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		return ""
	}

	return "\n\nThe installation must belong to the GitHub application of this SonarQube Cloud " +
		"instance. An installation of a different application means nothing to it, although " +
		"the identifier looks correct.\n\n" +
		"Read the sonarqube_dop_applications data source to find that application, install it " +
		"from https://github.com/apps/<application_key> on an organization that is not bound " +
		"yet, then use the installation identifier that GitHub reports."
}
