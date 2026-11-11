package shared

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// This file holds the parts of sonarqube_quality_profile_rule for
// active = false.

// ValidateConfig refuses values for a rule that the profile deactivates. An
// unknown value can still become null, so only known values are refused.
func (r *qualityProfileRuleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config qualityProfileRuleResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || !config.inactive() {
		return
	}
	for name, value := range map[string]types.Map{"impacts": config.Impacts, "params": config.Params} {
		if !value.IsNull() && !value.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Invalid "+name,
				"A rule with active = false has no values. Remove "+name+", or set active = true.")
		}
	}
}

// parentRule returns the activation of the rule in the parent of the
// profile. It returns client.ErrNotFound when the profile does not get the
// rule from a parent: the profile has no parent, the parent does not activate
// the rule, or the profile itself does not exist. In each of these cases,
// there is no inherited rule to manage.
func (r *qualityProfileRuleResource) parentRule(ctx context.Context, key, rule string) (*client.ActiveRule, error) {
	parentKey, err := r.client.GetQualityProfileParentKey(ctx, key)
	switch {
	case err != nil:
		return nil, err
	case parentKey == "":
		return nil, client.ErrNotFound
	}
	return r.client.GetActiveRule(ctx, parentKey, rule)
}

// deactivate deactivates the inherited rule of the plan, saves the plan in
// the state, and waits until the search does not show the rule as active.
// After deactivate_rule, the state keeps the rule also when the wait fails,
// because the rule can be inactive. Values that were unknown in the plan are
// not applied, so the state does not keep them.
func (r *qualityProfileRuleResource) deactivate(ctx context.Context, plan qualityProfileRuleResourceModel, state *tfsdk.State) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	key, rule := plan.QualityProfileKey.ValueString(), plan.Rule.ValueString()
	summary := "Cannot deactivate the rule " + rule + " in the quality profile " + key
	_, err := r.parentRule(ctx, key, rule)
	switch {
	case errors.Is(err, client.ErrNotFound):
		diagnostics.AddAttributeError(path.Root("active"), summary,
			"The parent profile does not activate the rule, so the profile does not get it from its parent. "+
				"active = false is only for a rule that the profile gets from its parent. Set active = true, or "+
				"remove the resource. Also check that the quality profile exists.")
		return diagnostics
	case err != nil:
		diagnostics.AddError(summary, ruleErrorDetail(err))
		return diagnostics
	}
	if err := r.client.DeactivateQualityProfileRule(ctx, key, rule); err != nil {
		diagnostics.AddError(summary, ruleErrorDetail(err))
		return diagnostics
	}
	plan.setValues(client.RuleValues{})
	diagnostics.Append(state.Set(ctx, &plan)...)
	diagnostics.Append(r.waitInactive(ctx, key, rule)...)
	return diagnostics
}

// waitInactive reads the rule until the search does not show it as active,
// because the index can lag behind the deactivation.
func (r *qualityProfileRuleResource) waitInactive(ctx context.Context, key, rule string) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	_, err := readAgain(ctx, r.writeRetries, r.getRule(ctx, key, rule),
		func(_ *client.ActiveRule, err error) bool { return err != nil })
	switch {
	case err == nil:
		diagnostics.AddError("The rule "+rule+" is still active after the change",
			"The search still shows the rule as active in the quality profile "+key+". The search index can "+
				"lag behind a change. Run the apply again.")
	case !errors.Is(err, client.ErrNotFound):
		diagnostics.AddError(readRuleSummary(key, rule), err.Error())
	}
	return diagnostics
}

// readInactive reads a rule of the state with active = false, without
// retries: a rule that is not active is the expected state. A rule that is
// active again, for example after a change in the parent, gets active = true,
// and the plan shows the change. It reports gone when there is no inherited
// rule any more, also when the profile was deleted.
func (r *qualityProfileRuleResource) readInactive(ctx context.Context, state *qualityProfileRuleResourceModel) (gone bool, err error) {
	key, rule := state.QualityProfileKey.ValueString(), state.Rule.ValueString()
	_, err = r.client.GetActiveRule(ctx, key, rule)
	switch {
	case err == nil:
		state.Active = types.BoolValue(true)
		return false, nil
	case !errors.Is(err, client.ErrNotFound):
		return false, err
	}
	return r.notInherited(ctx, key, rule)
}

// notInherited reports if the profile does not get the rule from its parent,
// or does not exist. A rule that is not active is then gone. Otherwise it is
// a deactivated inherited rule.
func (r *qualityProfileRuleResource) notInherited(ctx context.Context, key, rule string) (bool, error) {
	_, err := r.parentRule(ctx, key, rule)
	if errors.Is(err, client.ErrNotFound) {
		return true, nil
	}
	return false, err
}

// isDeactivatedInherited reports a rule that the profile gets from its
// parent but does not activate.
func (r *qualityProfileRuleResource) isDeactivatedInherited(ctx context.Context, key, rule string) (bool, error) {
	_, err := r.client.GetActiveRule(ctx, key, rule)
	switch {
	case err == nil:
		return false, nil
	case !errors.Is(err, client.ErrNotFound):
		return false, err
	}
	gone, err := r.notInherited(ctx, key, rule)
	return err == nil && !gone, err
}

// activateInherited activates a deactivated rule again, with the values of
// the parent. When there is no inherited rule any more, also because the
// profile was deleted, there is nothing to activate again.
func (r *qualityProfileRuleResource) activateInherited(ctx context.Context, key, rule string) error {
	gone, err := r.notInherited(ctx, key, rule)
	if err != nil || gone {
		return err
	}
	return r.client.ActivateQualityProfileRule(ctx, key, rule, client.RuleValues{})
}
