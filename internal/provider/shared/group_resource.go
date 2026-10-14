package shared

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                = &groupResource{}
	_ resource.ResourceWithConfigure   = &groupResource{}
	_ resource.ResourceWithImportState = &groupResource{}
)

// NewGroupResource returns the sonarqube_group resource.
func NewGroupResource() resource.Resource {
	return &groupResource{readRetries: []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}}
}

type groupResource struct {
	client *client.Client
	// readRetries are the waits before Read reads a group again. The group
	// list of SonarQube Cloud is eventually consistent: for some seconds after
	// a write, it can omit a new group or show the old name and description.
	readRetries []time.Duration
}

type groupResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
}

func (r *groupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (r *groupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a SonarQube Cloud group. A change to `name` renames the " +
			"group in place. A change to `organization` replaces it.\n\n" +
			"The built-in groups Members and Owners cannot be imported. A destroy of " +
			"Owners can remove the administrators of the organization. Use the " +
			"sonarqube_group data source to read them.\n\n" +
			"SonarQube Cloud refuses updates and deletions of a group that an identity " +
			"provider synchronizes through SCIM. The change fails with the API error, and " +
			"the state keeps the group. Manage such a group through the identity provider.\n\n" +
			"A delete removes the access that the group gives. SonarQube Server also removes " +
			"the permission and permission template grants of the group. The SonarQube Cloud " +
			"documentation does not say if it does the same. Do not expect a new group with " +
			"the same name to get the grants back.\n\n" +
			"The group list of SonarQube Cloud is eventually consistent. When a refresh finds " +
			"a name or a description that differs from the state, it reads the group again " +
			"for about six seconds. A change outside Terraform thus shows some seconds later " +
			"than other changes.\n\n" +
			"Import a group with `<organization>/<name>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Opaque group identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"organization": schema.StringAttribute{Required: true, Description: "Key of the organization that owns the group.",
				Validators:    validate.OrganizationKey(),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name": schema.StringAttribute{Required: true, Description: "Unique group name. A change renames the group in place.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 255)}},
			"description": schema.StringAttribute{Optional: true, Computed: true,
				Description: "Group description, up to 200 characters. Empty when not set.",
				Default:     stringdefault.StaticString(""),
				Validators:  []validator.String{stringvalidator.LengthAtMost(200)}},
		},
	}
}

func (r *groupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_group resource manages a group")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *groupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	group, err := r.client.CreateGroup(ctx, plan.Organization.ValueString(), plan.Name.ValueString(), plan.Description.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the group", err.Error())
		return
	}
	// Create and Update put the plan in the state and do not read the group
	// back: the group list of SonarQube Cloud can answer with old values for
	// some seconds after a write.
	plan.ID = types.StringValue(group.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state groupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization := state.Organization.ValueString()
	group, err := r.readGroup(ctx, state)
	switch {
	case errors.Is(err, client.ErrOrganizationNotFound):
		resp.Diagnostics.AddWarning(
			"The organization "+organization+" was not found",
			"Terraform removes the group from the state. The organization was deleted, or "+
				"the token cannot read it any more. In the second case, the next apply fails, "+
				"because the organization cannot be found.",
		)
		resp.State.RemoveResource(ctx)
		return
	case errors.Is(err, client.ErrNotFound):
		resp.State.RemoveResource(ctx)
		return
	case err != nil:
		resp.Diagnostics.AddError("Cannot read the group", err.Error())
		return
	}
	state.Name = types.StringValue(group.Name)
	state.Description = types.StringValue(group.Description)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *groupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior groupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.UpdateGroup(ctx, prior.ID.ValueString(), plan.Name.ValueString(), plan.Description.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot update the group", err.Error())
		return
	}
	plan.ID = prior.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteGroup(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot delete the group", err.Error())
	}
}

// ImportState takes "organization/name" and finds the id, so that Read always
// reads by id.
//
// It refuses the groups that SonarQube Cloud makes for each organization. A
// delete of Members fails, and a delete of Owners can remove the
// administrators of the organization. Import is the only way into the state
// for them: Create and a rename cannot use a name that is already taken.
func (r *groupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organization, name, ok := strings.Cut(req.ID, "/")
	if !ok || organization == "" || name == "" {
		resp.Diagnostics.AddError("Invalid group import ID", "Use <organization>/<name>.")
		return
	}
	if name == "Members" || name == "Owners" {
		resp.Diagnostics.AddError("Cannot import the "+name+" group",
			"SonarQube Cloud makes the "+name+" group for each organization. "+
				"Use the sonarqube_group data source to read it.")
		return
	}
	group, err := r.client.FindGroupByName(ctx, organization, name)
	if err != nil {
		resp.Diagnostics.AddError("Cannot import the group "+name, groupLookupDetail(err, organization))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), group.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}

// readGroup reads the group of the state by id. While the group is missing,
// or its name or description differs from the state, it reads again after
// each wait of readRetries. A group made or changed a few seconds ago is thus
// not taken for a deleted or changed one. A real change outside Terraform
// shows after the last wait. Any error other than a missing group ends the
// retries.
func (r *groupResource) readGroup(ctx context.Context, state groupResourceModel) (*client.Group, error) {
	organization, id := state.Organization.ValueString(), state.ID.ValueString()
	return readAgain(ctx, r.readRetries,
		func() (*client.Group, error) { return r.client.GetGroup(ctx, organization, id) },
		func(group *client.Group, err error) bool {
			if err != nil {
				return !errors.Is(err, client.ErrNotFound)
			}
			return matchesState(group, state)
		})
}

// matchesState reports a group that has the name and the description of the
// state. An import has no description in the state yet, so any description
// matches it.
func matchesState(group *client.Group, state groupResourceModel) bool {
	return group.Name == state.Name.ValueString() &&
		(state.Description.IsNull() || group.Description == state.Description.ValueString())
}

// groupLookupDetail explains why a lookup by name found no group, for import
// and for the data source.
func groupLookupDetail(err error, organization string) string {
	switch {
	case errors.Is(err, client.ErrOrganizationNotFound):
		return "The organization " + organization + " was not found. Check the key, and check " +
			"that the token can read the organization."
	case errors.Is(err, client.ErrNotFound):
		return "No group with this name is in the organization " + organization + ". Check the name. " +
			"The name must match exactly, with the same case."
	default:
		return err.Error()
	}
}
