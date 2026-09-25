package provider

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// resolveOrganization reads the organization that carries a key. It reports
// whether the caller may go on.
//
// The resource and the data source both start here: the bindings API names an
// organization by an internal identifier, and a read of the organization is
// the only place that reports it.
func resolveOrganization(ctx context.Context, c *client.Client, organizationKey string, diagnostics *diag.Diagnostics) (*client.Organization, bool) {
	org, err := c.GetOrganization(ctx, organizationKey)
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			// An organization that the token may not see answers 404 as well.
			diagnostics.AddAttributeError(
				path.Root("organization_key"),
				"Organization "+organizationKey+" not found",
				"No organization carries this key at "+c.APIURL()+
					". Check the key, and check that the token can read the organization.",
			)
			return nil, false
		}
		diagnostics.AddError("Cannot read the organization "+organizationKey, err.Error())
		return nil, false
	}
	return org, true
}

// findOrganizationBinding reads the binding of one organization, and reports
// whether the caller may go on.
//
// It takes the organization rather than its two identifiers, so that no
// caller can give the key where the internal identifier belongs. The compiler
// cannot see such a mistake between two strings.
//
// unboundDetail is one more sentence for an organization that carries no
// binding, or nothing. The import of the resource has more to say there than
// the read of the data source.
func findOrganizationBinding(
	ctx context.Context,
	c *client.Client,
	org *client.Organization,
	unboundDetail string,
	diagnostics *diag.Diagnostics,
) (*client.OrganizationBinding, bool) {
	binding, err := c.FindOrganizationBinding(ctx, org.ID)
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			detail := "The organization exists, but it is not bound to a DevOps platform."
			if unboundDetail != "" {
				detail += " " + unboundDetail
			}
			diagnostics.AddError("The organization "+org.Key+" has no binding", detail)
			return nil, false
		}
		diagnostics.AddError("Cannot read the binding of the organization "+org.Key, err.Error())
		return nil, false
	}
	return binding, true
}

// stateFromBinding turns a binding of the API into resource state.
//
// prior is the plan or the state that the operation started from. It supplies
// the organization key, which the caller writes and the API never reports:
// the API names an organization by its internal identifier only.
func stateFromBinding(binding *client.OrganizationBinding, prior organizationBindingModel) organizationBindingModel {
	return organizationBindingModel{
		ID:                 types.StringValue(binding.ID),
		OrganizationKey:    prior.OrganizationKey,
		OrganizationID:     nullIfEmpty(binding.OrganizationID),
		OrganizationUUIDV4: nullIfEmpty(binding.OrganizationUUIDV4),
		DevOpsPlatform:     types.StringValue(binding.DevOpsPlatform),
		// installation_id is required, so it must never hold null: a null
		// would not answer the configuration, and Terraform would report an
		// inconsistent result instead of the empty value that the API sent.
		InstallationID:        types.StringValue(binding.InstallationID),
		DevOpsPlatformURL:     nullIfEmpty(binding.DevOpsPlatformURL),
		RepoAutoImportEnabled: boolOrNull(binding.RepoAutoImportEnabled),
		DopFlavor:             nullIfEmpty(binding.DopFlavor),
		BindingType:           nullIfEmpty(binding.BindingType),
	}
}

// nullIfEmpty records a computed attribute that the API can leave out. The
// attribute holds no value at all rather than an empty string, because
// nothing in the configuration can ask for an empty one.
func nullIfEmpty(apiValue string) types.String {
	if apiValue == "" {
		return types.StringNull()
	}
	return types.StringValue(apiValue)
}

// boolOrNull records a boolean attribute that the API can leave out.
func boolOrNull(apiValue *bool) types.Bool {
	if apiValue == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*apiValue)
}

// boolIfSet returns nil for an attribute with no value, which leaves the
// matching field out of the request body.
func boolIfSet(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

// immutableInstallationIDPlanModifier refuses a changed installation, and
// says why.
//
// The API changes an installation for a binding to GitHub Enterprise only.
// A replacement does not help. A binding cannot be deleted, so the
// organization stays bound and the server refuses the new binding.
//
// This modifier therefore stops the plan. A failure during the plan is
// better than a failure during the apply.
type immutableInstallationIDPlanModifier struct{}

func (m immutableInstallationIDPlanModifier) Description(_ context.Context) string {
	return "The installation cannot change after the binding exists."
}

func (m immutableInstallationIDPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m immutableInstallationIDPlanModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// A create has no prior state, and a destroy has no plan.
	if req.StateValue.IsNull() || req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}
	if req.StateValue.Equal(req.PlanValue) {
		return
	}

	// A different organization is a different binding, and organization_key
	// asks for a replacement. The new organization is bound to nothing, so
	// its own installation is not a change of this installation, and the
	// server accepts the new bind. Only an installation that changes below
	// the same organization is refused.
	var plannedKey, priorKey types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("organization_key"), &plannedKey)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("organization_key"), &priorKey)...)
	if resp.Diagnostics.HasError() || !plannedKey.Equal(priorKey) {
		return
	}

	resp.Diagnostics.AddAttributeError(
		req.Path,
		"The installation of a binding cannot change",
		"The binding uses the installation "+req.StateValue.ValueString()+
			", and the configuration asks for "+req.PlanValue.ValueString()+".\n\n"+
			"The API changes an installation for a binding to GitHub Enterprise only.\n\n"+
			"To replace the resource does not help. A binding cannot be deleted, so the "+
			"organization stays bound and the server refuses the new binding.\n\n"+
			"To move an organization to a different installation, delete the organization and "+
			"make it again.",
	)
}
