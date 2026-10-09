package shared

import (
	"context"
	"errors"
	"strings"
	"time"

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
	_ resource.Resource                = &groupResource{}
	_ resource.ResourceWithConfigure   = &groupResource{}
	_ resource.ResourceWithImportState = &groupResource{}
)

// NewGroupResource returns the sonarqube_group resource.
func NewGroupResource() resource.Resource {
	return &groupResource{missingRetries: []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second}}
}

type groupResource struct {
	client *client.Client
	// missingRetries are the waits before a new search for a group that the
	// search does not hold. The search of SonarQube Cloud is eventually
	// consistent: it can omit a new group, or show old values, for some
	// seconds after a write.
	missingRetries []time.Duration
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
			"The built-in groups Members and Owners cannot be managed or imported. " +
			"A destroy of Owners can remove the administrators of the organization. " +
			"Use the sonarqube_group data source to read them.\n\n" +
			"SonarQube Cloud refuses updates and deletions of a group that an identity " +
			"provider synchronizes through SCIM. The change fails with the API error, and " +
			"the state keeps the group. Manage such a group through the identity provider.\n\n" +
			"A delete removes the access that the group gives. SonarQube Server also removes " +
			"the permission and permission template grants of the group. The SonarQube Cloud " +
			"documentation does not say if it does the same. Do not expect a new group with " +
			"the same name to get the grants back.\n\n" +
			"The group search of SonarQube Cloud is eventually consistent. For some seconds " +
			"after an apply, a refresh can show the old name or description as a change.\n\n" +
			"Import a group with `<organization>/<name>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Opaque group identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"organization": schema.StringAttribute{Required: true, Description: "Key of the organization that owns the group.",
				Validators:    organizationKeyValidators(),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name": schema.StringAttribute{Required: true, Description: "Unique group name. A change renames the group in place.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 255)}},
			"description": schema.StringAttribute{Optional: true, Description: "Group description, up to 200 characters.",
				Validators: []validator.String{stringvalidator.LengthAtMost(200)}},
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
	group, err := r.client.CreateGroup(ctx, plan.Organization.ValueString(), plan.Name.ValueString(), groupDescriptionIfSet(plan.Description))
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the group", err.Error())
		return
	}
	// The state takes the plan and not a read back: on SonarQube Cloud the
	// group search can answer with the old values for some seconds after a
	// write.
	plan.ID = types.StringValue(group.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state groupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.readIntoState(ctx, state, &resp.State, &resp.Diagnostics)
}

func (r *groupResource) readIntoState(ctx context.Context, prior groupResourceModel, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	organization := prior.Organization.ValueString()
	var group *client.Group
	var err error
	hasID := !prior.ID.IsNull() && prior.ID.ValueString() != ""
	if !hasID {
		group, err = r.client.FindGroupByName(ctx, organization, prior.Name.ValueString())
	} else {
		group, err = r.getGroupWithRetries(ctx, organization, prior.ID.ValueString())
	}
	if err != nil {
		if hasID && errors.Is(err, client.ErrNotFound) {
			// An *APIError is a 404 of the search, so the organization is gone or
			// the token cannot read it. A plain ErrNotFound is a group that the
			// list does not hold.
			var apiErr *client.APIError
			if errors.As(err, &apiErr) {
				diagnostics.AddWarning(
					"The organization "+organization+" was not found",
					"Terraform removes the group from the state. The organization was deleted, or "+
						"the token cannot read it any more. In the second case, the next apply fails, "+
						"because the organization cannot be found.",
				)
			}
			state.RemoveResource(ctx)
			return
		}
		diagnostics.AddError("Cannot read the group", groupLookupDetail(err, organization))
		return
	}
	if r.builtInGroup(group) {
		diagnostics.AddError("Cannot manage the "+group.Name+" group",
			"SonarQube Cloud makes the "+group.Name+" group for each organization. "+
				"Use the sonarqube_group data source to read it.")
		return
	}
	prior.ID = types.StringValue(group.ID)
	prior.Name = types.StringValue(group.Name)
	prior.Description = groupDescriptionState(group.Description, prior.Description)
	diagnostics.Append(state.Set(ctx, &prior)...)
}

func (r *groupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior groupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var name, description *string
	if !plan.Name.Equal(prior.Name) {
		value := plan.Name.ValueString()
		name = &value
	}
	if !plan.Description.Equal(prior.Description) {
		value := plan.Description.ValueString()
		description = &value
	}
	if err := r.client.UpdateGroup(ctx, prior.ID.ValueString(), name, description); err != nil {
		resp.Diagnostics.AddError("Cannot update the group", err.Error())
		return
	}
	// The state takes the plan for the same reason as in Create.
	plan.ID = prior.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Read and import refuse the built-in groups. A destroy with
	// -refresh=false skips Read, so the name in the state is checked again.
	if r.client.IsCloud() && builtInGroupName(state.Name.ValueString()) {
		resp.Diagnostics.AddError("Cannot delete the "+state.Name.ValueString()+" group",
			"SonarQube Cloud makes this group for each organization. Remove it from the state with terraform state rm.")
		return
	}
	if err := r.client.DeleteGroup(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot delete the group", err.Error())
	}
}

// getGroupWithRetries reads a group by id. It searches again when the group is
// not in the list, so that a group made a few seconds ago is not taken for a
// deleted one. A search that fails, or a 404 of the organization, ends the
// retries.
func (r *groupResource) getGroupWithRetries(ctx context.Context, organization, id string) (*client.Group, error) {
	group, err := r.client.GetGroup(ctx, organization, id)
	for _, wait := range r.missingRetries {
		var apiErr *client.APIError
		if !errors.Is(err, client.ErrNotFound) || errors.As(err, &apiErr) {
			return group, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		group, err = r.client.GetGroup(ctx, organization, id)
	}
	return group, err
}

// builtInGroup reports a group that SonarQube Cloud makes for each
// organization. A delete of Members fails, and a delete of Owners can remove
// the administrators of the organization.
func (r *groupResource) builtInGroup(group *client.Group) bool {
	return r.client.IsCloud() && builtInGroupName(group.Name)
}

func builtInGroupName(name string) bool {
	return name == "Members" || name == "Owners"
}

func (r *groupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organization, name, ok := strings.Cut(req.ID, "/")
	if !ok || organization == "" || name == "" {
		resp.Diagnostics.AddError("Invalid group import ID", "Use <organization>/<name>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}

func groupDescriptionIfSet(value types.String) *string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	text := value.ValueString()
	return &text
}

func groupDescriptionState(value string, prior types.String) types.String {
	if value != "" {
		return types.StringValue(value)
	}
	if !prior.IsNull() && !prior.IsUnknown() && prior.ValueString() == "" {
		return types.StringValue("")
	}
	return types.StringNull()
}

// groupLookupDetail explains a group that a search did not find. A 404 of the
// search also comes from an organization that the token cannot read. Only a
// lookup by name gets here with a group that the list does not hold: a lookup
// by id that misses removes the group from the state.
func groupLookupDetail(err error, organization string) string {
	if !errors.Is(err, client.ErrNotFound) {
		return err.Error()
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return "The organization " + organization + " was not found. Check the key, and check " +
			"that the token can read the organization."
	}
	return "No group with this name is in the organization " + organization + ". Check the name. " +
		"The name must match exactly, with the same case."
}

func organizationKeyValidators() []validator.String {
	return []validator.String{
		stringvalidator.LengthBetween(1, 255),
		stringvalidator.RegexMatches(validate.OrganizationKeyPattern,
			"must hold lower-case letters, digits and dashes only, with no leading and no trailing dash"),
	}
}
