package cloud

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
)

var (
	_ resource.Resource                = &organizationDefaultResource{}
	_ resource.ResourceWithConfigure   = &organizationDefaultResource{}
	_ resource.ResourceWithImportState = &organizationDefaultResource{}
)

// organizationDefault is an object that an organization uses as its default,
// such as its default quality gate. Each one has an API to read and to set
// the default, but none to remove it.
type organizationDefault struct {
	// typeSuffix follows the provider type name, for example
	// "_cloud_organization_default_quality_gate".
	typeSuffix string
	// attribute holds the id of the default object, for example "quality_gate_id".
	attribute string
	// noun names the object in diagnostics, for example "quality gate".
	noun string
	// description is the description of the resource. The schema adds the
	// paragraph about destroy.
	description string
	// attributeDescription is the description of attribute.
	attributeDescription string
	// get returns the id of the default. A missing organization matches
	// client.ErrOrganizationNotFound.
	get func(ctx context.Context, c *client.Client, organization string) (string, error)
	// set makes the object with the id the default, and reports each error.
	set func(ctx context.Context, c *client.Client, organization, id string) diag.Diagnostics
}

// organizationDefaultResource sets the default of an organization. Destroy
// only removes it from the state, because the API cannot remove a default.
type organizationDefaultResource struct {
	client *client.Client
	organizationDefault
}

func (r *organizationDefaultResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + r.typeSuffix
}

func (r *organizationDefaultResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: r.description + "\n\n" +
			"~> **The API cannot remove a default.** `terraform destroy` drops this resource from " +
			"the state and gives a warning, but the organization keeps its default. SonarQube " +
			"Cloud refuses to delete the default " + r.noun + ", so before you destroy a " + r.noun +
			" that is the default, set a different " + r.noun + " as the default.",
		Attributes: organizationSettingAttributes(map[string]schema.Attribute{
			r.attribute: schema.StringAttribute{
				Required:    true,
				Description: r.attributeDescription,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
		}),
	}
}

func (r *organizationDefaultResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData,
		"The sonarqube"+r.typeSuffix+" resource sets a default "+r.noun)
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *organizationDefaultResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *organizationDefaultResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

// apply makes the object of the plan the default of the organization, then
// writes the plan to the state.
func (r *organizationDefaultResource) apply(ctx context.Context, plan tfsdk.Plan, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	var organization, id string
	diagnostics.Append(plan.GetAttribute(ctx, path.Root("organization"), &organization)...)
	diagnostics.Append(plan.GetAttribute(ctx, path.Root(r.attribute), &id)...)
	if diagnostics.HasError() {
		return
	}
	diagnostics.Append(r.set(ctx, r.client, organization, id)...)
	if diagnostics.HasError() {
		return
	}
	r.write(ctx, state, organization, id, diagnostics)
}

func (r *organizationDefaultResource) write(ctx context.Context, state *tfsdk.State, organization, id string, diagnostics *diag.Diagnostics) {
	diagnostics.Append(state.SetAttribute(ctx, path.Root("id"), organization)...)
	diagnostics.Append(state.SetAttribute(ctx, path.Root("organization"), organization)...)
	diagnostics.Append(state.SetAttribute(ctx, path.Root(r.attribute), id)...)
}

// Read writes the default of the API to the state. A change of the default in
// the UI shows here, so the next plan sets the object of the configuration
// again.
func (r *organizationDefaultResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var organization string
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("organization"), &organization)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.get(ctx, r.client, organization)
	switch {
	case errors.Is(err, client.ErrOrganizationNotFound):
		resp.Diagnostics.AddWarning(
			"The organization "+organization+" was not found",
			"Terraform removes the default "+r.noun+" of this organization from the state. The organization "+
				"was deleted, or the token cannot read it any more. In the second case, the next apply fails, "+
				"because the organization cannot be found.",
		)
		resp.State.RemoveResource(ctx)
	case err != nil:
		resp.Diagnostics.AddError("Cannot read the default "+r.noun+" of the organization "+organization, err.Error())
	// Every organization has a default. An empty value would show a change
	// in every plan, so it is an error and not a value for the state.
	case id == "":
		resp.Diagnostics.AddError("Cannot read the default "+r.noun+" of the organization "+organization,
			"The API gave no default "+r.noun+" for the organization.")
	default:
		r.write(ctx, &resp.State, organization, id, &resp.Diagnostics)
	}
}

// Delete takes the default out of the state, and nothing else. The API has no
// operation to remove a default. A different object can only take its place.
func (r *organizationDefaultResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var organization, id string
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("organization"), &organization)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root(r.attribute), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddWarning(
		"The default "+r.noun+" was not changed",
		"The API has no operation to remove a default, so the organization "+organization+" keeps the "+
			r.noun+" "+id+" as its default. Terraform has forgotten the default, but the default is still there.\n\n"+
			"SonarQube Cloud refuses to delete the default "+r.noun+". To delete this "+r.noun+", first set a "+
			"different "+r.noun+" as the default.",
	)
}

// ImportState takes the key of the organization. Read fills in the default.
func (r *organizationDefaultResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
