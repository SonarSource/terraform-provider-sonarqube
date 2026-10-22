package shared

import (
	"context"
	"errors"
	"regexp"
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
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/validate"
)

var (
	_ resource.Resource                = &groupMemberResource{}
	_ resource.ResourceWithConfigure   = &groupMemberResource{}
	_ resource.ResourceWithImportState = &groupMemberResource{}
)

// NewGroupMemberResource returns the sonarqube_group_member resource.
func NewGroupMemberResource() resource.Resource {
	return &groupMemberResource{readRetries: []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}}
}

type groupMemberResource struct {
	client *client.Client
	// readRetries are the waits before Read reads a membership again. The
	// member list of SonarQube Cloud can show the old members for some
	// seconds after a change.
	readRetries []time.Duration
}

type groupMemberResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	GroupName    types.String `tfsdk:"group_name"`
	Login        types.String `tfsdk:"login"`
}

func (m groupMemberResourceModel) importID() string {
	return m.Organization.ValueString() + "/" + m.GroupName.ValueString() + "/" + m.Login.ValueString()
}

func (r *groupMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group_member"
}

func (r *groupMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "This resource adds one user to one group. Destroy removes the user from the group. It does not " +
			"change the other members of the group. A change to any attribute replaces the membership.\n\n" +
			"## Organization members only\n\n" +
			"The user must already be a member of the organization. This resource does not add the user to " +
			"the organization. If the user is not a member, the apply fails.\n\n" +
			"## Groups that SCIM synchronizes\n\n" +
			"Do not use this resource for a group that an identity provider synchronizes through SCIM. The " +
			"next synchronization can remove the user again, so the plan never converges. The provider " +
			"refuses to add a member to such a group. Add the user to the group in the identity provider.\n\n" +
			"## Groups that SonarQube Cloud refuses\n\n" +
			"SonarQube Cloud refuses the default group `Members`. Each member of the organization is in this " +
			"group.\n\n" +
			"SonarQube Cloud also refuses to remove a user from any group when no other user is an " +
			"administrator of the organization. This rule applies also to a group that has no permissions. " +
			"Then the destroy fails. To prevent this, give the `admin` permission directly to a user, or add " +
			"another user to a group that has it.\n\n" +
			"## Delays\n\n" +
			"The member list of SonarQube Cloud can show the old members for some seconds after a change. " +
			"When a refresh does not find the user, it reads the list again for about six seconds. A removal " +
			"outside Terraform thus shows some seconds later than other changes.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "`<organization>/<group_name>/<login>`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"organization": schema.StringAttribute{Required: true, Description: "Key of the organization that owns the group.",
				Validators: validate.OrganizationKey(), PlanModifiers: replace},
			"group_name": schema.StringAttribute{Required: true, Description: "Exact name of the group, with the same case.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 255)}, PlanModifiers: replace},
			"login": schema.StringAttribute{Required: true,
				Description: "Login of the user, with the same case. It cannot contain `/`. The user must be a member of the organization.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1),
					stringvalidator.RegexMatches(regexp.MustCompile(`^[^/]*$`), "must not contain \"/\"")},
				PlanModifiers: replace},
		},
	}
}

func (r *groupMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_group_member resource manages a group membership")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *groupMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization, groupName := plan.Organization.ValueString(), plan.GroupName.ValueString()
	resp.Diagnostics.Append(r.refuseManagedGroup(ctx, organization, groupName)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.AddGroupMember(ctx, organization, groupName, plan.Login.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot add the user to the group "+groupName, err.Error())
		return
	}
	plan.ID = types.StringValue(plan.importID())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// refuseManagedGroup refuses a group that SCIM synchronizes: the next
// synchronization can remove the user again. The group list can omit a group
// made some seconds ago. Such a group is not managed, so a group that the list
// does not show passes, and add_user reports a group that is really missing.
func (r *groupMemberResource) refuseManagedGroup(ctx context.Context, organization, name string) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	group, err := r.client.FindGroupByName(ctx, organization, name)
	if errors.Is(err, client.ErrNotFound) {
		return nil
	}
	switch {
	case err != nil:
		diagnostics.AddError("Cannot add a member to the group "+name, groupLookupDetail(err, organization))
	case group.Managed:
		diagnostics.AddError("Cannot add a member to the group "+name,
			"An identity provider synchronizes this group through SCIM, and the next synchronization "+
				"can remove the user again. Add the user to the group in the identity provider.")
	}
	return diagnostics
}

func (r *groupMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state groupMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization := state.Organization.ValueString()
	member, err := r.isMember(ctx, state)
	switch {
	case errors.Is(err, client.ErrOrganizationNotFound):
		resp.Diagnostics.AddWarning(
			"The organization "+organization+" was not found",
			"Terraform removes the group membership from the state. The organization was deleted, or "+
				"the token cannot read it any more. In the second case, the next apply fails, "+
				"because the organization cannot be found.",
		)
		resp.State.RemoveResource(ctx)
	case errors.Is(err, client.ErrNotFound) || (err == nil && !member):
		resp.State.RemoveResource(ctx)
	case err != nil:
		resp.Diagnostics.AddError("Cannot read the group membership", err.Error())
	default:
		state.ID = types.StringValue(state.importID())
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	}
}

// isMember reads the membership of the state. While the user is not in the
// list, it reads again after each wait of readRetries, so that a membership
// made a few seconds ago is not taken for a removed one.
func (r *groupMemberResource) isMember(ctx context.Context, state groupMemberResourceModel) (bool, error) {
	organization, group, login := state.Organization.ValueString(), state.GroupName.ValueString(), state.Login.ValueString()
	return readAgain(ctx, r.readRetries,
		func() (bool, error) { return r.client.IsGroupMember(ctx, organization, group, login) },
		func(member bool, err error) bool { return err != nil || member })
}

// Update is never called with a change, because each attribute replaces the
// resource.
func (r *groupMemberResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Cannot update a group membership",
		"Each attribute of sonarqube_group_member replaces the resource. This is a fault in the provider.")
}

func (r *groupMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.RemoveGroupMember(ctx, state.Organization.ValueString(), state.GroupName.ValueString(), state.Login.ValueString())
	if err == nil || errors.Is(err, client.ErrOrganizationNotFound) || errors.Is(err, client.ErrNotFound) {
		return
	}
	resp.Diagnostics.AddError("Cannot remove the user from the group "+state.GroupName.ValueString(), err.Error())
}

// ImportState takes "organization/group_name/login". An organization key and
// a login have no "/", but a group name can.
func (r *groupMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organization, rest, _ := strings.Cut(req.ID, "/")
	separator := strings.LastIndex(rest, "/")
	if !validate.OrganizationKeyPattern.MatchString(organization) || separator <= 0 || separator == len(rest)-1 {
		resp.Diagnostics.AddError("Invalid group member import ID", "Use <organization>/<group_name>/<login>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_name"), rest[:separator])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("login"), rest[separator+1:])...)
}
