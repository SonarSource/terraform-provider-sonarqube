package cloud

import (
	"context"
	"errors"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
)

var (
	_ resource.Resource                = &organizationDefaultQualityGateResource{}
	_ resource.ResourceWithConfigure   = &organizationDefaultQualityGateResource{}
	_ resource.ResourceWithImportState = &organizationDefaultQualityGateResource{}
)

// NewOrganizationDefaultQualityGateResource returns the
// sonarqube_cloud_organization_default_quality_gate resource.
func NewOrganizationDefaultQualityGateResource() resource.Resource {
	return &organizationDefaultQualityGateResource{}
}

type organizationDefaultQualityGateResource struct {
	client *client.Client
}

type organizationDefaultQualityGateModel struct {
	ID            types.String `tfsdk:"id"`
	Organization  types.String `tfsdk:"organization"`
	QualityGateID types.String `tfsdk:"quality_gate_id"`
}

func (r *organizationDefaultQualityGateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_organization_default_quality_gate"
}

func (r *organizationDefaultQualityGateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Sets the default quality gate of a SonarQube Cloud organization. Each project " +
			"that has no quality gate of its own uses the default.\n\n" +
			"The gate can be one that Terraform does not manage, such as the built-in Sonar way " +
			"gate, which the `sonarqube_cloud_quality_gate` data source finds.\n\n" +
			"The organization must be on the Team or the Enterprise plan. On the Free plan, " +
			"SonarQube Cloud refuses to change the default.\n\n" +
			"~> **The API cannot remove a default.** `terraform destroy` drops this resource from " +
			"the state and gives a warning, but the organization keeps its default. SonarQube " +
			"Cloud refuses to delete the default gate, so before you destroy a gate that is the " +
			"default, set a different gate as the default.",
		Attributes: organizationSettingAttributes(map[string]schema.Attribute{
			"quality_gate_id": schema.StringAttribute{
				Required: true,
				Description: "UUID of the quality gate to set as the default. The gate must belong " +
					"to the organization.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
		}),
	}
}

func (r *organizationDefaultQualityGateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData,
		"The sonarqube_cloud_organization_default_quality_gate resource sets a default quality gate")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *organizationDefaultQualityGateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *organizationDefaultQualityGateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

// apply makes the gate of the plan the default of the organization, then
// writes the plan to the state.
func (r *organizationDefaultQualityGateResource) apply(ctx context.Context, planned tfsdk.Plan, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	var plan organizationDefaultQualityGateModel
	diagnostics.Append(planned.Get(ctx, &plan)...)
	if diagnostics.HasError() {
		return
	}
	organization := plan.Organization.ValueString()
	gateID := plan.QualityGateID.ValueString()

	org, ok := lookupOrganization(ctx, r.client, organization, path.Root("organization"), diagnostics)
	if !ok {
		return
	}

	err := r.client.SetDefaultQualityGate(ctx, org.UUIDV4, gateID)
	if errors.Is(err, client.ErrNotFound) {
		diagnostics.AddAttributeError(path.Root("quality_gate_id"),
			"Quality gate "+gateID+" not found",
			"No quality gate has this UUID. Check the UUID, and check that the token can read the gate.")
		return
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		diagnostics.AddError("Cannot set the default quality gate of the organization "+organization,
			"SonarQube Cloud refused the change: "+err.Error()+"\n\n"+
				"The organization must be on the Team or the Enterprise plan to change its default "+
				"quality gate, and the token must have the Administer Quality Gates permission.")
		return
	}
	if err != nil {
		diagnostics.AddError("Cannot set the default quality gate of the organization "+organization, err.Error())
		return
	}

	plan.ID = plan.Organization
	diagnostics.Append(state.Set(ctx, &plan)...)
}

func (r *organizationDefaultQualityGateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationDefaultQualityGateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization := state.Organization.ValueString()

	org, ok := refreshOrganization(ctx, r.client, organization,
		"the default quality gate of this organization", resp)
	if !ok {
		return
	}

	// Every organization has a default gate. An empty value would show a
	// change in every plan, so it is an error and not a value for the state.
	if org.DefaultQualityGateUUID == "" {
		resp.Diagnostics.AddError("Cannot read the default quality gate of the organization "+organization,
			"The API gave no default quality gate for the organization.")
		return
	}

	// A change of the default in the UI shows here, so the next plan sets the
	// gate of the configuration again.
	state.ID = types.StringValue(org.Key)
	state.QualityGateID = types.StringValue(org.DefaultQualityGateUUID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete takes the default out of the state, and nothing else. The API has no
// operation to remove a default. A different gate can only take its place.
func (r *organizationDefaultQualityGateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationDefaultQualityGateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddWarning(
		"The default quality gate was not changed",
		"The API has no operation to remove a default, so the organization "+
			state.Organization.ValueString()+" keeps the quality gate "+state.QualityGateID.ValueString()+
			" as its default. Terraform has forgotten the default, but the default is still there.\n\n"+
			"SonarQube Cloud refuses to delete the default gate. To delete this gate, first set a "+
			"different gate as the default.",
	)
}

// ImportState takes the key of the organization. Read fills in the gate.
func (r *organizationDefaultQualityGateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
