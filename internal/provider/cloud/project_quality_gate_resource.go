package cloud

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/validate"
)

var (
	_ resource.Resource                = &projectQualityGateResource{}
	_ resource.ResourceWithConfigure   = &projectQualityGateResource{}
	_ resource.ResourceWithImportState = &projectQualityGateResource{}
)

// NewProjectQualityGateResource returns the
// sonarqube_cloud_project_quality_gate resource.
func NewProjectQualityGateResource() resource.Resource {
	return &projectQualityGateResource{}
}

type projectQualityGateResource struct {
	client *client.Client
}

type projectQualityGateModel struct {
	ID            types.String `tfsdk:"id"`
	Organization  types.String `tfsdk:"organization"`
	ProjectKey    types.String `tfsdk:"project_key"`
	QualityGateID types.String `tfsdk:"quality_gate_id"`
	AssociationID types.String `tfsdk:"association_id"`
}

func (r *projectQualityGateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_project_quality_gate"
}

func (r *projectQualityGateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Assigns a quality gate to a SonarQube Cloud project. The next analysis of the " +
			"project uses this gate.\n\n" +
			"`terraform destroy` removes the assignment. The project then uses the default quality " +
			"gate of the organization.\n\n" +
			"The gate can be one that Terraform does not manage, such as the built-in Sonar way " +
			"gate, which the `sonarqube_cloud_quality_gate` data source finds.\n\n" +
			"The organization must be on the Team or the Enterprise plan. On the Free plan, " +
			"SonarQube Cloud refuses to change the gate of a project.\n\n" +
			"If a project uses the default gate of the organization, this resource does not exist " +
			"for that project. A change of the gate in the UI shows in the next plan.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Organization key and project key, separated by a slash.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization": schema.StringAttribute{
				Required:    true,
				Description: "Key of the organization that owns the project. A change replaces this resource.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
					stringvalidator.RegexMatches(validate.OrganizationKeyPattern,
						"must hold lower-case letters, digits and dashes only, with no leading and no trailing dash"),
				},
				PlanModifiers: replace,
			},
			"project_key": schema.StringAttribute{
				Required:    true,
				Description: "Key of the project. A change replaces this resource.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 400),
					stringvalidator.RegexMatches(validate.ProjectKeyPattern,
						"must hold letters, digits, dashes, underscores, dots and colons only, "+
							"with at least one character that is not a digit"),
				},
				PlanModifiers: replace,
			},
			"quality_gate_id": schema.StringAttribute{
				Required:    true,
				Description: "UUID of the quality gate for the project. The gate must belong to the organization.",
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"association_id": schema.StringAttribute{
				Computed: true,
				// A different gate may give a new identifier, so the value is not
				// kept from the state.
				Description: "Identifier of the assignment of the gate to the project.",
			},
		},
	}
}

func (r *projectQualityGateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData,
		"The sonarqube_cloud_project_quality_gate resource assigns a quality gate")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *projectQualityGateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *projectQualityGateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

// apply assigns the gate of the plan to the project, then writes the plan and
// the identifier of the assignment to the state.
func (r *projectQualityGateResource) apply(ctx context.Context, planned tfsdk.Plan, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	var plan projectQualityGateModel
	diagnostics.Append(planned.Get(ctx, &plan)...)
	if diagnostics.HasError() {
		return
	}
	organization := plan.Organization.ValueString()
	key := plan.ProjectKey.ValueString()
	gateID := plan.QualityGateID.ValueString()

	org, ok := lookupOrganization(ctx, r.client, organization, path.Root("organization"), diagnostics)
	if !ok {
		return
	}
	project, err := r.client.GetProject(ctx, organization, key)
	if errors.Is(err, client.ErrNotFound) {
		addProjectNotFound(diagnostics, organization, key)
		return
	}
	if err != nil {
		diagnostics.AddError("Cannot read the project "+key, err.Error())
		return
	}

	association, err := r.client.CreateQualityGateProjectAssociation(ctx, project, gateID)
	if errors.Is(err, client.ErrNotFound) {
		diagnostics.AddAttributeError(path.Root("quality_gate_id"),
			"Quality gate "+gateID+" not found",
			"No quality gate has this UUID. Check the UUID, and check that the token can read the gate.")
		return
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		diagnostics.AddError("Cannot assign the quality gate to the project "+key,
			"SonarQube Cloud refused the change: "+err.Error()+"\n\n"+
				"The organization must be on the Team or the Enterprise plan to assign a quality "+
				"gate to a project, and the token must have the Administer Quality Gates permission.")
		return
	}
	if err != nil {
		diagnostics.AddError("Cannot assign the quality gate to the project "+key, err.Error())
		return
	}

	// The reply of the create call may not hold the identifier, so it is read
	// again when it is empty.
	if association.ID == "" {
		association, err = r.client.FindQualityGateProjectAssociation(ctx, org.UUIDV4, project)
		if err != nil {
			diagnostics.AddError("Cannot read the quality gate of the project "+key, err.Error())
			return
		}
	}

	plan.ID = types.StringValue(organization + "/" + key)
	plan.AssociationID = types.StringValue(association.ID)
	diagnostics.Append(state.Set(ctx, &plan)...)
}

func (r *projectQualityGateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectQualityGateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization := state.Organization.ValueString()
	key := state.ProjectKey.ValueString()

	org, err := r.client.GetOrganization(ctx, organization)
	if errors.Is(err, client.ErrNotFound) {
		addAssignmentGone(&resp.Diagnostics, "organization", organization)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the organization "+organization, err.Error())
		return
	}

	project, err := r.client.GetProject(ctx, organization, key)
	if errors.Is(err, client.ErrNotFound) {
		addAssignmentGone(&resp.Diagnostics, "project", key)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the project "+key, err.Error())
		return
	}

	association, err := r.client.FindQualityGateProjectAssociation(ctx, org.UUIDV4, project)
	if errors.Is(err, client.ErrNotFound) {
		// The project uses the default gate of the organization, so the
		// assignment does not exist. The next plan creates it again.
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the quality gate of the project "+key, err.Error())
		return
	}

	state.ID = types.StringValue(organization + "/" + key)
	state.QualityGateID = types.StringValue(association.QualityGateID)
	state.AssociationID = types.StringValue(association.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// addAssignmentGone warns that the state loses the assignment of a gate,
// because its organization or its project cannot be read.
func addAssignmentGone(diagnostics *diag.Diagnostics, kind, name string) {
	diagnostics.AddWarning(
		"The "+kind+" "+name+" was not found",
		"Terraform removes the quality gate assignment of this project from the state. The "+kind+
			" was deleted, or the token cannot read it any more. In the second case, the next "+
			"apply fails, because the "+kind+" cannot be found.",
	)
}

// Delete removes the assignment. The project then uses the default quality gate
// of its organization.
func (r *projectQualityGateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectQualityGateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Without an identifier there is nothing to delete, and the request would
	// go to the collection path.
	id := state.AssociationID.ValueString()
	if id == "" {
		return
	}
	if err := r.client.DeleteQualityGateProjectAssociation(ctx, id); err != nil {
		resp.Diagnostics.AddError(
			"Cannot remove the quality gate of the project "+state.ProjectKey.ValueString(), err.Error())
	}
}

// ImportState takes "organization/project_key". Read fills in the rest.
func (r *projectQualityGateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organization, key, separated := strings.Cut(req.ID, "/")
	if !separated || organization == "" || key == "" {
		resp.Diagnostics.AddError(
			"Cannot import the project quality gate",
			"The identifier must hold the organization and the project key, separated "+
				"by a slash, as in \"my-organization/my-project\". It holds "+req.ID+".")
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_key"), key)...)
}
