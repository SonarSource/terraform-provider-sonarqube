package cloud

import (
	"context"
	"errors"
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
	// scope is set when the organization has one default for each value of
	// an attribute, such as one default quality profile for each language.
	scope *organizationDefaultScope
	// get returns the id of the default. A missing organization matches
	// client.ErrOrganizationNotFound. scope is empty without a scope.
	get func(ctx context.Context, c *client.Client, organization, scope string) (string, error)
	// set makes the object with the id the default, and reports each error.
	set func(ctx context.Context, c *client.Client, organization, scope, id string) diag.Diagnostics
}

// organizationDefaultScope is the attribute that selects one of the defaults
// of an organization. A change replaces the resource. The id and the import ID
// are <organization>/<scope>.
type organizationDefaultScope struct {
	// attribute is the name of the attribute, for example "language".
	attribute string
	// description is the description of attribute.
	description string
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
	attributes := map[string]schema.Attribute{
		r.attribute: schema.StringAttribute{
			Required:    true,
			Description: r.attributeDescription,
			Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
		},
	}
	if r.scope != nil {
		attributes["id"] = schema.StringAttribute{
			Computed:      true,
			Description:   "Key of the organization and " + r.scope.attribute + ", as `<organization>/<" + r.scope.attribute + ">`.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
		attributes[r.scope.attribute] = schema.StringAttribute{
			Required:    true,
			Description: r.scope.description + " A change replaces this resource.",
			// The id joins the organization and the scope with a slash.
			Validators: []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^[^/]+$`),
				"must not be empty or contain a slash")},
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
		}
	}
	resp.Schema = schema.Schema{
		Description: r.description + "\n\n" +
			"~> **The API cannot remove a default.** `terraform destroy` drops this resource from " +
			"the state and gives a warning, but the organization keeps its default. SonarQube " +
			"Cloud refuses to delete the default " + r.noun + ", so before you destroy a " + r.noun +
			" that is the default, set a different " + r.noun + " as the default.",
		Attributes: organizationSettingAttributes(attributes),
	}
}

// keys reads the organization and the scope. The scope is empty without a
// scope attribute.
func (r *organizationDefaultResource) keys(ctx context.Context, get func(context.Context, path.Path, any) diag.Diagnostics, diagnostics *diag.Diagnostics) (organization, scope string) {
	diagnostics.Append(get(ctx, path.Root("organization"), &organization)...)
	if r.scope != nil {
		diagnostics.Append(get(ctx, path.Root(r.scope.attribute), &scope)...)
	}
	return organization, scope
}

// target names the default in diagnostics, for example "the organization
// my-org" or "the language java of the organization my-org".
func (r *organizationDefaultResource) target(organization, scope string) string {
	if r.scope == nil {
		return "the organization " + organization
	}
	return "the " + r.scope.attribute + " " + scope + " of the organization " + organization
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
	organization, scope := r.keys(ctx, plan.GetAttribute, diagnostics)
	var id string
	diagnostics.Append(plan.GetAttribute(ctx, path.Root(r.attribute), &id)...)
	if diagnostics.HasError() {
		return
	}
	diagnostics.Append(r.set(ctx, r.client, organization, scope, id)...)
	if diagnostics.HasError() {
		return
	}
	r.write(ctx, state, organization, scope, id, diagnostics)
}

func (r *organizationDefaultResource) write(ctx context.Context, state *tfsdk.State, organization, scope, id string, diagnostics *diag.Diagnostics) {
	resourceID := organization
	if r.scope != nil {
		resourceID += "/" + scope
		diagnostics.Append(state.SetAttribute(ctx, path.Root(r.scope.attribute), scope)...)
	}
	diagnostics.Append(state.SetAttribute(ctx, path.Root("id"), resourceID)...)
	diagnostics.Append(state.SetAttribute(ctx, path.Root("organization"), organization)...)
	diagnostics.Append(state.SetAttribute(ctx, path.Root(r.attribute), id)...)
}

// Read writes the default of the API to the state. A change of the default in
// the UI shows here, so the next plan sets the object of the configuration
// again.
func (r *organizationDefaultResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	organization, scope := r.keys(ctx, req.State.GetAttribute, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.get(ctx, r.client, organization, scope)
	switch {
	case errors.Is(err, client.ErrOrganizationNotFound):
		resp.Diagnostics.AddWarning(
			"The organization "+organization+" was not found",
			"Terraform removes the default "+r.noun+" of "+r.target(organization, scope)+" from the state. The organization "+
				"was deleted, or the token cannot read it any more. In the second case, the next apply fails, "+
				"because the organization cannot be found.",
		)
		resp.State.RemoveResource(ctx)
	case err != nil:
		resp.Diagnostics.AddError("Cannot read the default "+r.noun+" of "+r.target(organization, scope), err.Error())
	// Every organization has a default. An empty value would show a change
	// in every plan, so it is an error and not a value for the state.
	case id == "":
		resp.Diagnostics.AddError("Cannot read the default "+r.noun+" of "+r.target(organization, scope),
			"The API gave no default "+r.noun+" for "+r.target(organization, scope)+".")
	default:
		r.write(ctx, &resp.State, organization, scope, id, &resp.Diagnostics)
	}
}

// Delete takes the default out of the state, and nothing else. The API has no
// operation to remove a default. A different object can only take its place.
func (r *organizationDefaultResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	organization, scope := r.keys(ctx, req.State.GetAttribute, &resp.Diagnostics)
	var id string
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root(r.attribute), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddWarning(
		"The default "+r.noun+" was not changed",
		"The API has no operation to remove a default, so "+r.target(organization, scope)+" keeps the "+
			r.noun+" "+id+" as its default. Terraform has forgotten the default, but the default is still there.\n\n"+
			"SonarQube Cloud refuses to delete the default "+r.noun+". To delete this "+r.noun+", first set a "+
			"different "+r.noun+" as the default.",
	)
}

// ImportState takes the key of the organization, or <organization>/<scope>
// with a scope. Read fills in the default.
func (r *organizationDefaultResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organization := req.ID
	if r.scope != nil {
		var scope string
		var ok bool
		organization, scope, ok = strings.Cut(req.ID, "/")
		if !ok || organization == "" || scope == "" || strings.Contains(scope, "/") {
			resp.Diagnostics.AddError("Invalid import ID",
				"The import ID must be <organization>/<"+r.scope.attribute+">, but it is "+req.ID+".")
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(r.scope.attribute), scope)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
