package cloud

import (
	"context"
	_ "embed" // for the go:embed of the schema description
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/validate"
)

var (
	_ resource.Resource                = &enterpriseResource{}
	_ resource.ResourceWithConfigure   = &enterpriseResource{}
	_ resource.ResourceWithImportState = &enterpriseResource{}
)

// The description is long and has backticks, so it is in its own file.
//
//go:embed enterprise_resource.md
var enterpriseDescription string

type enterpriseResource struct{ client *client.Client }

type enterpriseModel struct {
	ID           types.String `tfsdk:"id"`
	Key          types.String `tfsdk:"key"`
	Name         types.String `tfsdk:"name"`
	Avatar       types.String `tfsdk:"avatar"`
	License      types.String `tfsdk:"license"`
	Organization types.String `tfsdk:"organization"`
}

// NewEnterpriseResource returns the sonarqube_cloud_enterprise resource.
func NewEnterpriseResource() resource.Resource {
	return &enterpriseResource{}
}

func (r *enterpriseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_enterprise"
}

func (r *enterpriseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	createOnly := []planmodifier.String{enterpriseCreateOnlyPlanModifier{}}
	resp.Schema = schema.Schema{MarkdownDescription: strings.TrimSpace(enterpriseDescription), Attributes: map[string]schema.Attribute{
		"key": schema.StringAttribute{Required: true, MarkdownDescription: "Enterprise key. It cannot change after create.",
			Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: createOnly},
		"name": schema.StringAttribute{Required: true, MarkdownDescription: "Enterprise name.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"avatar": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
			MarkdownDescription: "Address of the avatar image. The default is no avatar."},
		"license": schema.StringAttribute{Required: true, Sensitive: true, PlanModifiers: createOnly,
			MarkdownDescription: "Enterprise license key. A license can be used for one enterprise only. It cannot change after create. The API does not return it, so it is null after import."},
		"organization": schema.StringAttribute{Required: true, Validators: validate.OrganizationKey(), PlanModifiers: createOnly,
			MarkdownDescription: "Key of the first organization of the enterprise. It cannot change after create. The API does not return it, so it is null after import."},
		"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Enterprise UUID.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
	}}
}

func (r *enterpriseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_cloud_enterprise resource manages an enterprise")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

// enterpriseCreateOnlyPlanModifier refuses a change of an attribute that the
// API cannot update. A replacement deletes the enterprise and its portfolios,
// and the new enterprise needs a new license, so the plan stops instead.
//
// A null state value is not a change: the API does not return license and
// organization, so they are null after an import.
type enterpriseCreateOnlyPlanModifier struct{}

func (m enterpriseCreateOnlyPlanModifier) Description(_ context.Context) string {
	return "The value cannot change after the enterprise exists."
}

func (m enterpriseCreateOnlyPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m enterpriseCreateOnlyPlanModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || req.StateValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}
	if req.StateValue.Equal(req.PlanValue) {
		return
	}
	addEnterpriseCreateOnlyError(&resp.Diagnostics, req.Path)
}

func addEnterpriseCreateOnlyError(diagnostics *diag.Diagnostics, attribute path.Path) {
	diagnostics.AddAttributeError(attribute, "The "+attribute.String()+" of an enterprise cannot change",
		"The API cannot change "+attribute.String()+" of an existing enterprise. The provider does not replace the "+
			"enterprise, because a replacement deletes the enterprise and its portfolios, and a license can be used "+
			"only one time. Set the value back to the value in the state.")
}

func (r *enterpriseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan enterpriseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key := plan.Key.ValueString()
	org, ok := lookupOrganization(ctx, r.client, plan.Organization.ValueString(), path.Root("organization"), &resp.Diagnostics)
	if !ok {
		return
	}
	// The create gives the same 400 for a used key as for other faults, so
	// the key is checked first. A failed create does not use up the license.
	_, err := r.client.GetEnterpriseByKey(ctx, key)
	switch {
	case err == nil:
		resp.Diagnostics.AddAttributeError(path.Root("key"), "Enterprise key "+key+" is already used",
			"An enterprise with this key exists. Use a different key, or import the enterprise.")
		return
	case !errors.Is(err, client.ErrNotFound):
		resp.Diagnostics.AddError("Cannot check the enterprise key "+key, err.Error())
		return
	}
	enterprise, err := r.client.CreateEnterprise(ctx, client.EnterpriseRequest{
		Key: key, Name: plan.Name.ValueString(), Avatar: plan.Avatar.ValueString(),
		License: plan.License.ValueString(), OrganizationID: org.ID,
	})
	if client.IsEnterpriseKeyRefused(err) {
		resp.Diagnostics.AddAttributeError(path.Root("key"), "The enterprise key "+key+" was refused",
			"The key is not valid, or another enterprise that the token user cannot see uses it. "+err.Error())
		return
	}
	if refusedByServer(err) {
		resp.Diagnostics.AddError("Cannot create the enterprise "+key, err.Error())
		return
	}
	if err != nil || enterprise.ID == "" {
		// The server can create the enterprise and use up the license although
		// the answer is lost or incomplete. A read by key then finds the ID.
		enterprise = r.recoverCreatedEnterprise(ctx, key, err, &resp.Diagnostics)
		if enterprise == nil {
			return
		}
	}
	// The API answers with the values that it got, so the plan is kept as it is.
	plan.ID = types.StringValue(enterprise.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// refusedByServer reports a 4xx answer: the server did not create anything.
func refusedByServer(err error) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500
}

func (r *enterpriseResource) recoverCreatedEnterprise(ctx context.Context, key string, createErr error, diagnostics *diag.Diagnostics) *client.Enterprise {
	detail := "The API returned no enterprise ID."
	if createErr != nil {
		detail = createErr.Error()
	}
	enterprise, err := r.client.GetEnterpriseByKey(ctx, key)
	if err != nil || enterprise.ID == "" {
		diagnostics.AddError("Cannot create the enterprise "+key, detail+"\n\nThe enterprise was possibly created, "+
			"and its license used up. If an enterprise with the key "+key+" exists, import it.")
		return nil
	}
	return enterprise
}

func (m *enterpriseModel) setFrom(enterprise *client.Enterprise) {
	m.Key = types.StringValue(enterprise.Key)
	m.Name = types.StringValue(enterprise.Name)
	m.Avatar = types.StringValue(enterprise.Avatar)
}

func (r *enterpriseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state enterpriseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	enterprise, err := r.client.GetEnterprise(ctx, state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the enterprise "+state.Key.ValueString(),
			err.Error()+"\n\nThe enterprise API needs a user token. Scoped organization tokens are not supported.")
		return
	}
	state.setFrom(enterprise)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *enterpriseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state enterpriseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// A value that was unknown during the plan skips the plan modifier.
	for _, attribute := range []struct {
		name         string
		prior, value types.String
	}{{"key", state.Key, plan.Key}, {"license", state.License, plan.License}, {"organization", state.Organization, plan.Organization}} {
		if !attribute.prior.IsNull() && !attribute.prior.Equal(attribute.value) {
			addEnterpriseCreateOnlyError(&resp.Diagnostics, path.Root(attribute.name))
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	enterprise, err := r.client.UpdateEnterprise(ctx, plan.ID.ValueString(), client.EnterpriseUpdate{
		Name: plan.Name.ValueString(), Avatar: plan.Avatar.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot update the enterprise "+plan.Key.ValueString(), err.Error())
		return
	}
	plan.Name = types.StringValue(enterprise.Name)
	plan.Avatar = types.StringValue(enterprise.Avatar)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *enterpriseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state enterpriseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteEnterprise(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot delete the enterprise "+state.Key.ValueString(), err.Error())
	}
}

func (r *enterpriseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	enterprise, ok := lookupEnterprise(ctx, r.client, req.ID, &resp.Diagnostics)
	if !ok {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), enterprise.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), enterprise.Key)...)
}
