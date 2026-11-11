package cloud

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
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
	_ resource.Resource                = &organizationGitLabSelfManagedBindingResource{}
	_ resource.ResourceWithConfigure   = &organizationGitLabSelfManagedBindingResource{}
	_ resource.ResourceWithImportState = &organizationGitLabSelfManagedBindingResource{}
	_ resource.ResourceWithModifyPlan  = &organizationGitLabSelfManagedBindingResource{}
)

// NewOrganizationGitLabSelfManagedBindingResource returns the
// sonarqube_cloud_organization_gitlab_self_managed_binding resource.
func NewOrganizationGitLabSelfManagedBindingResource() resource.Resource {
	return &organizationGitLabSelfManagedBindingResource{}
}

// organizationGitLabSelfManagedBindingResource manages two objects of the API: the GitLab
// configuration of the organization, which holds the addresses of the
// instance, and the binding, which holds the group and the token. The server
// needs the configuration before it accepts the binding, and a binding
// without its configuration cannot reach GitLab, so the two go together.
type organizationGitLabSelfManagedBindingResource struct {
	client *client.Client
}

type organizationGitLabSelfManagedBindingModel struct {
	ID                 types.String `tfsdk:"id"`
	OrganizationKey    types.String `tfsdk:"organization_key"`
	ServerURL          types.String `tfsdk:"server_url"`
	APIURL             types.String `tfsdk:"api_url"`
	Group              types.String `tfsdk:"group"`
	AccessToken        types.String `tfsdk:"access_token"`
	ConfigurationID    types.String `tfsdk:"configuration_id"`
	GroupID            types.String `tfsdk:"group_id"`
	DevOpsPlatformURL  types.String `tfsdk:"dev_ops_platform_url"`
	OrganizationID     types.String `tfsdk:"organization_id"`
	OrganizationUUIDV4 types.String `tfsdk:"organization_uuid_v4"`
}

// cannotReadBinding starts the summary of a failed read of a binding.
const cannotReadBinding = "Cannot read the organization binding "

// gitlabAPIPath is the part that the API address adds to the web address on
// a GitLab instance with the default setup.
const gitlabAPIPath = "/api/v4"

// httpsAddress takes an HTTPS address without a trailing slash, a query or a
// fragment. The server refuses each of these, so the plan refuses them first.
var httpsAddress = regexp.MustCompile(`^https://[^\s?#]+[^/\s?#]$`)

func (r *organizationGitLabSelfManagedBindingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_organization_gitlab_self_managed_binding"
}

func (r *organizationGitLabSelfManagedBindingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	addressValidators := []validator.String{stringvalidator.RegexMatches(httpsAddress,
		"must be an HTTPS address without a trailing slash, a query or a fragment")}

	resp.Schema = schema.Schema{
		Description: "Binds a SonarQube Cloud organization to a top-level group on a GitLab " +
			"Self-Managed instance. It also stores the GitLab configuration of the organization, " +
			"which holds the addresses of the instance.\n\n" +
			"The organization must have the Enterprise plan. SonarQube Cloud must reach the " +
			"GitLab API over HTTPS at a public address, with a certificate of a public " +
			"certificate authority.\n\n" +
			"~> **The API cannot remove a binding.** `terraform destroy` drops this resource " +
			"from the state and gives a warning, but the binding and the GitLab configuration " +
			"stay. The provider keeps the configuration, because a binding without it cannot " +
			"reach GitLab. The server removes the binding when it deletes the organization, " +
			"and the configuration then belongs to no organization that exists.\n\n" +
			"To destroy an organization and its binding together is therefore correct. " +
			"Terraform deletes the binding before the organization that it depends on.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Identifier of the binding.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_key": schema.StringAttribute{
				Required:      true,
				Description:   "Key of the SonarQube Cloud organization to bind.",
				Validators:    validate.OrganizationKey(),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"server_url": schema.StringAttribute{
				Required: true,
				Description: "Address that opens the GitLab instance in a browser, for example " +
					"`https://gitlab.example.com`. Include the installation path if the instance " +
					"uses one. Do not include a group or a project path.",
				Validators: addressValidators,
			},
			"api_url": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Address of the GitLab REST API. The default is `server_url` followed " +
					"by `" + gitlabAPIPath + "`. Set it only when the instance uses a different API " +
					"address, for example behind a proxy.",
				Validators: addressValidators,
			},
			"group": schema.StringAttribute{
				Required: true,
				Description: "Path of the top-level GitLab group, for example `my-group` for " +
					"`https://gitlab.example.com/groups/my-group`. The owner of `access_token` must " +
					"own this group.\n\n" +
					"~> This cannot change after the binding exists. To replace the resource does " +
					"not help, because a binding cannot be deleted.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{immutableGroup},
			},
			"access_token": schema.StringAttribute{
				Required:  true,
				Sensitive: true,
				Description: "GitLab personal access token with the `api` scope. SonarQube Cloud " +
					"uses it to read the group and to report analysis results. Use a token of a " +
					"technical account that owns the group.\n\n" +
					"The API never returns the token, so the provider cannot see a change made " +
					"outside Terraform. A new value here replaces the token of the binding.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"configuration_id": schema.StringAttribute{
				Computed:      true,
				Description:   "Identifier of the GitLab configuration of the organization.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"group_id": schema.StringAttribute{
				Computed:      true,
				Description:   "Numeric identifier of the group on the GitLab instance.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"dev_ops_platform_url": schema.StringAttribute{
				Computed:      true,
				Description:   "Address of the group, as the server reports it.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
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
		},
	}
}

func (r *organizationGitLabSelfManagedBindingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData,
		"The sonarqube_cloud_organization_gitlab_self_managed_binding resource binds an organization")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

// ModifyPlan gives api_url its default. A static default cannot do this,
// because the default depends on server_url. A changed server_url also
// changes the default, so the plan shows the new API address.
func (r *organizationGitLabSelfManagedBindingResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}

	var configured, serverURL types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("api_url"), &configured)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("server_url"), &serverURL)...)
	if resp.Diagnostics.HasError() || !configured.IsNull() {
		return
	}

	apiURL := types.StringUnknown()
	if !serverURL.IsUnknown() {
		apiURL = types.StringValue(serverURL.ValueString() + gitlabAPIPath)
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("api_url"), apiURL)...)
}

func (r *organizationGitLabSelfManagedBindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationGitLabSelfManagedBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationKey := plan.OrganizationKey.ValueString()
	org, ok := lookupOrganization(ctx, r.client, organizationKey, path.Root("organization_key"), &resp.Diagnostics)
	if !ok {
		return
	}

	configuration, ok := r.syncConfiguration(ctx, organizationKey, org.UUIDV4, plan, nil, &resp.Diagnostics)
	if !ok {
		return
	}

	binding, err := r.client.CreateGitLabSelfManagedBinding(ctx, client.CreateGitLabSelfManagedBindingRequest{
		OrganizationID: org.ID,
		DevOpsPlatform: client.PlatformGitLab,
		DopFlavor:      client.FlavorGitLabSelfManaged,
		AccessToken:    plan.AccessToken.ValueString(),
		Group:          plan.Group.ValueString(),
	})
	if err != nil {
		// The configuration stays. The next create takes it over, and a
		// binding that the server stored although the call failed can be
		// imported.
		addGitLabError(&resp.Diagnostics, "Cannot bind the organization "+organizationKey, err)
		return
	}

	// The binding exists from here on, and nothing can delete it. Record it
	// before anything else can fail, for the reason given in the create of
	// sonarqube_cloud_organization_binding.
	resp.Diagnostics.Append(resp.State.Set(ctx, gitlabStateFromAPI(plan, configuration, binding))...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The answer of the write can differ from what the server stored. A
	// failed read gives a warning, not an error, because an error from a
	// create taints the object, and a tainted binding would leave the state
	// for good.
	stored, err := r.client.GetOrganizationBinding(ctx, binding.ID)
	if err != nil {
		resp.Diagnostics.AddWarning(cannotReadBinding+binding.ID+" back",
			err.Error()+"\n\nThe binding was written, and the state holds the answer of that "+
				"write. The next refresh reads the binding again and reports any difference.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, gitlabStateFromAPI(plan, configuration, stored))...)
}

// syncConfiguration makes the GitLab configuration of the organization match
// the plan, and reports whether the caller may go on. current is the
// configuration that the server holds, or nil.
//
// A create of a configuration answers 409 when one exists that the caller
// did not know, for example after a bind that failed. The provider takes it
// over when its addresses are the planned ones.
func (r *organizationGitLabSelfManagedBindingResource) syncConfiguration(
	ctx context.Context,
	organizationKey, organizationUUID string,
	plan organizationGitLabSelfManagedBindingModel,
	current *client.GitLabConfiguration,
	diagnostics *diag.Diagnostics,
) (*client.GitLabConfiguration, bool) {
	webURL, apiURL := plan.ServerURL.ValueString(), plan.APIURL.ValueString()
	if current != nil && current.WebURL == webURL && current.APIURL == apiURL {
		return current, true
	}

	if current != nil {
		configuration, err := r.client.UpdateGitLabConfiguration(ctx, current.ID, client.GitLabConfigurationPatch{
			APIURL:              apiURL,
			WebURL:              webURL,
			PersonalAccessToken: plan.AccessToken.ValueString(),
		})
		if err != nil {
			addGitLabError(diagnostics, "Cannot update the GitLab configuration of the organization "+organizationKey, err)
			return nil, false
		}
		return configuration, true
	}

	summary := "Cannot store the GitLab configuration of the organization " + organizationKey
	configuration, err := r.client.CreateGitLabConfiguration(ctx, client.GitLabConfigurationRequest{
		OrganizationID:      organizationUUID,
		APIURL:              apiURL,
		WebURL:              webURL,
		PersonalAccessToken: plan.AccessToken.ValueString(),
		Group:               plan.Group.ValueString(),
	})
	var apiErr *client.APIError
	switch {
	case err == nil:
		return configuration, true
	case !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict:
		addGitLabError(diagnostics, summary, err)
		return nil, false
	}

	existing, findErr := r.client.FindGitLabConfiguration(ctx, organizationUUID)
	if findErr != nil {
		diagnostics.AddError(summary, err.Error()+"\n\nThe existing configuration cannot be read: "+findErr.Error())
		return nil, false
	}
	if existing.WebURL != webURL || existing.APIURL != apiURL {
		diagnostics.AddError(summary,
			"The organization already has a GitLab configuration for "+existing.WebURL+" (API "+
				existing.APIURL+"), and the configuration asks for "+webURL+" (API "+apiURL+").\n\n"+
				"If the organization is already bound, import the binding instead. Otherwise, "+
				"change server_url and api_url to the values above, apply, and change them after.")
		return nil, false
	}
	return existing, true
}

// readServer reads the binding and the GitLab configuration of its
// organization. The configuration is nil when the server has none. A binding
// that the server does not know gives ErrNotFound.
func (r *organizationGitLabSelfManagedBindingResource) readServer(ctx context.Context, id string) (*client.OrganizationBinding, *client.GitLabConfiguration, error) {
	binding, err := r.client.GetOrganizationBinding(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	// The configuration API names an organization by its UUID only, and the
	// binding always reports it.
	if binding.OrganizationUUIDV4 == "" {
		return nil, nil, errors.New("the binding names no organization UUID, so its GitLab configuration cannot be read")
	}
	configuration, err := r.client.FindGitLabConfiguration(ctx, binding.OrganizationUUIDV4)
	switch {
	case errors.Is(err, client.ErrNotFound):
		return binding, nil, nil
	case err != nil:
		return binding, nil, fmt.Errorf("the GitLab configuration of the organization cannot be read: %w", err)
	}
	return binding, configuration, nil
}

func (r *organizationGitLabSelfManagedBindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationGitLabSelfManagedBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	binding, configuration, err := r.readServer(ctx, id)
	if errors.Is(err, client.ErrNotFound) {
		// A binding that the token may not see gives the same answer, as in
		// sonarqube_cloud_organization_binding.
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(cannotReadBinding+id, err.Error())
		return
	}
	// A configuration that somebody deleted gives null addresses. The plan
	// then shows a change of server_url, and the update stores the
	// configuration again.
	resp.Diagnostics.Append(resp.State.Set(ctx, gitlabStateFromAPI(state, configuration, binding))...)
}

// Update starts from what the server holds, not from the state, so that a
// configuration deleted outside Terraform needs no special case.
func (r *organizationGitLabSelfManagedBindingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state organizationGitLabSelfManagedBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	binding, current, err := r.readServer(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(cannotReadBinding+id, err.Error())
		return
	}

	// The binding goes first, as in the web interface: it checks the new
	// token, and it moves to the new instance before the configuration does.
	patch := client.PatchGitLabSelfManagedBindingRequest{}
	if !plan.AccessToken.Equal(state.AccessToken) {
		patch.AccessToken = plan.AccessToken.ValueString()
	}
	if !plan.ServerURL.Equal(state.ServerURL) {
		patch.TenantID = plan.ServerURL.ValueString()
		patch.DevOpsPlatformURL = plan.ServerURL.ValueString() + "/groups/" + state.Group.ValueString()
	}
	if patch != (client.PatchGitLabSelfManagedBindingRequest{}) {
		if binding, err = r.client.UpdateGitLabSelfManagedBinding(ctx, id, patch); err != nil {
			addGitLabError(&resp.Diagnostics, "Cannot update the organization binding "+id, err)
			return
		}
	}

	configuration, ok := r.syncConfiguration(ctx, state.OrganizationKey.ValueString(),
		binding.OrganizationUUIDV4, plan, current, &resp.Diagnostics)
	if !ok {
		// The binding may have changed already. Record what the server holds
		// now, so that the next plan starts from it.
		resp.Diagnostics.Append(resp.State.Set(ctx, gitlabStateFromAPI(state, current, binding))...)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, gitlabStateFromAPI(plan, configuration, binding))...)
}

// Delete takes the binding out of the state, and nothing else. The API has no
// operation to remove a binding, and a binding without its GitLab
// configuration cannot reach GitLab, so the configuration stays too.
func (r *organizationGitLabSelfManagedBindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationGitLabSelfManagedBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddWarning(
		"The organization binding was not removed",
		"The API has no operation to remove a binding, so the organization "+
			state.OrganizationKey.ValueString()+" stays bound to GitLab, and its GitLab "+
			"configuration stays too. Terraform has forgotten both.\n\n"+
			"A binding goes away when its organization is deleted. If this destroy also deletes "+
			"the organization, the binding goes with it.",
	)
}

// ImportState takes the key of the organization, as the import of
// sonarqube_cloud_organization_binding does.
//
// The API never returns the token, so access_token is null after an import,
// and the next plan writes the configured token to the binding.
func (r *organizationGitLabSelfManagedBindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organizationKey := req.ID

	org, ok := lookupOrganization(ctx, r.client, organizationKey, path.Root("organization_key"), &resp.Diagnostics)
	if !ok {
		return
	}

	binding, ok := findOrganizationBinding(ctx, r.client, org, "There is nothing to import.", &resp.Diagnostics)
	if !ok {
		return
	}

	if binding.DevOpsPlatform != client.PlatformGitLab || binding.DopFlavor != client.FlavorGitLabSelfManaged {
		resp.Diagnostics.AddError(
			"The organization "+organizationKey+" is not bound to GitLab Self-Managed",
			"The organization is bound to "+binding.DevOpsPlatform+platformFlavorNote(binding.DopFlavor)+
				". This resource manages a binding to GitLab Self-Managed only. Read the "+
				"sonarqube_cloud_organization_binding data source to see this binding.",
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), binding.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_key"), organizationKey)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), org.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_uuid_v4"), org.UUIDV4)...)
}

func platformFlavorNote(flavor string) string {
	if flavor == "" {
		return ""
	}
	return " (" + flavor + ")"
}

// gitlabStateFromAPI turns the configuration and the binding into resource
// state. prior supplies what the API never reports: the organization key and
// the token. A nil configuration gives null addresses.
func gitlabStateFromAPI(
	prior organizationGitLabSelfManagedBindingModel,
	configuration *client.GitLabConfiguration,
	binding *client.OrganizationBinding,
) organizationGitLabSelfManagedBindingModel {
	state := organizationGitLabSelfManagedBindingModel{
		ID:                 types.StringValue(binding.ID),
		OrganizationKey:    prior.OrganizationKey,
		AccessToken:        prior.AccessToken,
		ConfigurationID:    types.StringNull(),
		ServerURL:          types.StringNull(),
		APIURL:             types.StringNull(),
		Group:              groupFromBinding(prior.Group, binding),
		GroupID:            nullIfEmpty(binding.DevOpsPlatformOrganizationID),
		DevOpsPlatformURL:  nullIfEmpty(binding.DevOpsPlatformURL),
		OrganizationID:     nullIfEmpty(binding.OrganizationID),
		OrganizationUUIDV4: nullIfEmpty(binding.OrganizationUUIDV4),
	}
	if configuration != nil {
		state.ConfigurationID = types.StringValue(configuration.ID)
		state.ServerURL = types.StringValue(configuration.WebURL)
		state.APIURL = types.StringValue(configuration.APIURL)
	}
	return state
}

// groupFromBinding gives the path of the group.
//
// The bind call takes the path, but a read reports the numeric identifier in
// the same field. The path is the last part of the address of the group,
// which is the web address of the instance followed by /groups/<path>. GitLab
// paths ignore case, so a prior value that differs only in case stays.
func groupFromBinding(prior types.String, binding *client.OrganizationBinding) types.String {
	prefix := strings.TrimSuffix(binding.TenantID, "/") + "/groups/"
	address := binding.DevOpsPlatformURL
	if binding.TenantID == "" || len(address) <= len(prefix) || !strings.EqualFold(address[:len(prefix)], prefix) {
		return prior
	}
	reported := address[len(prefix):]
	if strings.EqualFold(reported, prior.ValueString()) {
		return prior
	}
	return types.StringValue(reported)
}

// gitlabFieldAttributes maps a field that the server names in relatedField
// to the attribute that holds it.
var gitlabFieldAttributes = map[string]string{
	"apiUrl":                       "api_url",
	"webUrl":                       "server_url",
	"personalAccessToken":          "access_token",
	"accessToken":                  "access_token",
	"group":                        "group",
	"devOpsPlatformOrganizationId": "group",
}

// addGitLabError reports a failure of the GitLab API calls. When the server
// names the field at fault, the error goes to the matching attribute, so that
// Terraform shows the line of the configuration.
func addGitLabError(diagnostics *diag.Diagnostics, summary string, err error) {
	detail := err.Error()

	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		detail += "\n\nThe organization must have the Enterprise plan, and the token of the " +
			"provider must have the Administer Organization permission."
	}

	if apiErr != nil {
		if attribute, found := gitlabFieldAttributes[apiErr.RelatedField]; found {
			diagnostics.AddAttributeError(path.Root(attribute), summary, detail)
			return
		}
	}
	diagnostics.AddError(summary, detail)
}

// immutableGroup guards the group of a binding. The API cannot move a
// binding to another group. GitLab paths ignore case.
var immutableGroup = immutableWithinOrganization{
	description: "The group cannot change after the binding exists.",
	same:        strings.EqualFold,
	summary:     "The group of a binding cannot change",
	detail: func(prior, planned string) string {
		return "The binding uses the group " + prior + ", and the configuration asks for " + planned + ".\n\n" +
			"The API cannot move a binding to another group. To replace the resource does not help. " +
			"A binding cannot be deleted, so the organization stays bound and the server refuses " +
			"the new binding.\n\n" +
			"To bind to a different group, delete the organization and make it again."
	},
}
