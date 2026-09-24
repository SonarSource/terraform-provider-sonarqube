package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// keyPlanModifier plans an attribute as the key of the organization.
type keyPlanModifier struct{}

func (m keyPlanModifier) Description(_ context.Context) string {
	return "Takes the value of the key attribute."
}

func (m keyPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m keyPlanModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	var key types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("key"), &key)...)
	if resp.Diagnostics.HasError() || key.IsNull() || key.IsUnknown() {
		return
	}
	resp.PlanValue = key
}

// recordAndRead writes the organization into the state, twice.
//
// Create and Update both end here, and the order matters. The write already
// succeeded, so the planned values go into the state first: a read back that
// fails must not lose a created organization, and must not leave the state
// describing an organization that the server no longer holds. The read back
// then replaces them with what the server really holds, which picks up
// anything the server changed on the way in.
func (r *organizationResource) recordAndRead(
	ctx context.Context,
	key string,
	plan organizationResourceModel,
	state *tfsdk.State,
	diagnostics *diag.Diagnostics,
) {
	written := plan
	written.ID = types.StringValue(key)
	written.Key = types.StringValue(key)

	diagnostics.Append(state.Set(ctx, written)...)
	if diagnostics.HasError() {
		return
	}

	org, err := r.client.GetOrganization(ctx, key)
	if err != nil {
		diagnostics.AddError("Cannot read the organization "+key+" back", err.Error())
		return
	}

	diagnostics.Append(state.Set(ctx, stateFromOrganization(org, plan))...)
}

// stateFromOrganization turns an organization of the API into resource state.
//
// prior is the plan or the state that the operation started from. It settles
// how an optional attribute with no value is recorded, because the API leaves
// out a field that holds nothing.
func stateFromOrganization(org *client.Organization, prior organizationResourceModel) organizationResourceModel {
	return organizationResourceModel{
		ID:          types.StringValue(org.Key),
		Key:         types.StringValue(org.Key),
		Name:        types.StringValue(org.Name),
		Description: optionalString(org.Description, prior.Description),
		URL:         optionalString(org.URL, prior.URL),
		AvatarURL:   optionalString(org.AvatarURL, prior.AvatarURL),
	}
}

// optionalString records an optional attribute that the API can leave out. An
// attribute that was null stays null, so that an organization without a
// description does not report a difference on every plan.
func optionalString(apiValue string, prior types.String) types.String {
	if apiValue != "" {
		return types.StringValue(apiValue)
	}
	// Keep an empty string that the configuration asked for, and record
	// everything else as null.
	if !prior.IsNull() && !prior.IsUnknown() && prior.ValueString() == "" {
		return types.StringValue("")
	}
	return types.StringNull()
}

// valueIfSet returns nil for an attribute with no value, which leaves the
// matching parameter out of the request.
func valueIfSet(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

// valueOrEmpty always returns a pointer, so the parameter always goes with the
// request. An attribute with no value becomes an empty parameter, which clears
// the field: ValueString answers with an empty string for a value that is null
// and for one that is unknown.
func valueOrEmpty(v types.String) *string {
	s := v.ValueString()
	return &s
}
