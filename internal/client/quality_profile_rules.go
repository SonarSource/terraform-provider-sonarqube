package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Values of ActiveRule.Inherit.
const (
	// RuleInheritNone means that the profile activates the rule itself.
	RuleInheritNone = "NONE"
	// RuleInheritInherited means that the profile gets the rule from its
	// parent, with the values of the parent.
	RuleInheritInherited = "INHERITED"
	// RuleInheritOverrides means that the profile gets the rule from its
	// parent, and changes some of its values.
	RuleInheritOverrides = "OVERRIDES"
)

// RuleValues are the impacts and the parameters of a rule. Impacts maps a
// software quality to its severity, and Params maps a parameter key to its
// value.
type RuleValues struct {
	Impacts map[string]string
	Params  map[string]string
}

// IsEmpty reports values that have no impact and no parameter.
func (v RuleValues) IsEmpty() bool {
	return len(v.Impacts) == 0 && len(v.Params) == 0
}

// ActiveRule is a rule that a quality profile activates.
type ActiveRule struct {
	Inherit string
	// Values are the values in the profile: all the impacts and parameters
	// of the rule, also the ones that keep their default value.
	Values RuleValues
	// Defaults are the values of the rule itself, before a profile changes
	// them.
	Defaults RuleValues
}

// ActivateQualityProfileRule activates the rule in the profile, or changes an
// active rule. The server keeps the current value of each parameter and
// impact that the call does not send. It also ignores an impact for a
// software quality that the rule does not have.
func (c *Client) ActivateQualityProfileRule(ctx context.Context, key, rule string, v RuleValues) error {
	values := url.Values{"key": {key}, "rule": {rule}}
	if len(v.Impacts) > 0 {
		values.Set("impacts", keyValueList(v.Impacts))
	}
	if len(v.Params) > 0 {
		values.Set("params", keyValueList(v.Params))
	}
	return ruleActivationError(c.post(ctx, qualityProfilesPath+"/activate_rule", values))
}

// ResetQualityProfileRule sets the parameters and impacts of the rule in the
// profile back to the values of the parent profile, or to the default values
// of the rule when the profile does not inherit the rule.
func (c *Client) ResetQualityProfileRule(ctx context.Context, key, rule string) error {
	values := url.Values{"key": {key}, "rule": {rule}, "reset": {"true"}}
	return ruleActivationError(c.post(ctx, qualityProfilesPath+"/activate_rule", values))
}

// DeactivateQualityProfileRule deactivates the rule in the profile. A rule
// that is not active, or a profile that does not exist, is success.
func (c *Client) DeactivateQualityProfileRule(ctx context.Context, key, rule string) error {
	values := url.Values{"key": {key}, "rule": {rule}}
	return ruleActivationError(c.postIgnoringNotFound(ctx, qualityProfilesPath+"/deactivate_rule", values))
}

// GetActiveRule returns the activation of the rule in the profile, or
// ErrNotFound when the rule is not active or the profile does not exist. The
// search finds the profile by its key, so it needs no organization. The
// search index can lag behind a write by some seconds.
func (c *Client) GetActiveRule(ctx context.Context, key, rule string) (*ActiveRule, error) {
	params := url.Values{
		"qprofile":   {key},
		"activation": {"true"},
		"rule_key":   {rule},
		"f":          {"actives,params,impacts"},
	}
	var answer struct {
		Rules   []searchedRule            `json:"rules"`
		Actives map[string][]ruleActivity `json:"actives"`
	}
	if err := c.get(ctx, "/api/rules/search", params, &answer); err != nil {
		return nil, err
	}
	i := slices.IndexFunc(answer.Actives[rule], func(a ruleActivity) bool { return a.QProfile == key })
	if i < 0 {
		return nil, ErrNotFound
	}
	active := answer.Actives[rule][i]
	result := &ActiveRule{
		Inherit:  active.Inherit,
		Values:   RuleValues{Impacts: impactMap(active.Impacts), Params: map[string]string{}},
		Defaults: RuleValues{Impacts: map[string]string{}, Params: map[string]string{}},
	}
	for _, param := range active.Params {
		result.Values.Params[param.Key] = param.Value
	}
	if j := slices.IndexFunc(answer.Rules, func(r searchedRule) bool { return r.Key == rule }); j >= 0 {
		for _, param := range answer.Rules[j].Params {
			result.Defaults.Params[param.Key] = param.DefaultValue
		}
		result.Defaults.Impacts = impactMap(answer.Rules[j].Impacts)
	}
	return result, nil
}

// searchedRule is a rule of api/rules/search, with its default values.
type searchedRule struct {
	Key    string `json:"key"`
	Params []struct {
		Key          string `json:"key"`
		DefaultValue string `json:"defaultValue"`
	} `json:"params"`
	Impacts []impact `json:"impacts"`
}

// ruleActivity is the activation of a rule in one profile.
type ruleActivity struct {
	QProfile string     `json:"qProfile"`
	Inherit  string     `json:"inherit"`
	Params   []keyValue `json:"params"`
	Impacts  []impact   `json:"impacts"`
}

type keyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type impact struct {
	SoftwareQuality string `json:"softwareQuality"`
	Severity        string `json:"severity"`
}

func impactMap(impacts []impact) map[string]string {
	result := make(map[string]string, len(impacts))
	for _, i := range impacts {
		result[i.SoftwareQuality] = i.Severity
	}
	return result
}

// keyValueList encodes the map as key1="value1";key2="value2", in key order.
// The quotes let a value contain ";". The server does not read a doubled
// quote as one quote, so a value cannot contain a quote.
func keyValueList(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+`="`+values[key]+`"`)
	}
	return strings.Join(pairs, ";")
}

// ruleActivationError explains the refusals that the server message does not
// make clear.
func ruleActivationError(err error) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case apiErr.StatusCode == http.StatusBadRequest &&
		hasMessagePrefix(apiErr.Messages, "Operation forbidden for built-in Quality Profile"):
		return fmt.Errorf("a built-in quality profile is read-only. Create a sonarqube_quality_profile that "+
			"extends it, and change the rules of that profile: %w", err)
	case isMissingMisraEntitlement(apiErr):
		return fmt.Errorf("the rule is a MISRA rule, and the organization does not have the MISRA "+
			"entitlement that it needs. Remove the rule from the configuration, or get the entitlement "+
			"for the organization: %w", err)
	}
	return err
}

// isMissingMisraEntitlement reports the refusal of a MISRA rule or profile in
// an organization without the MISRA entitlement.
func isMissingMisraEntitlement(apiErr *APIError) bool {
	return apiErr.StatusCode == http.StatusForbidden && hasMessageContaining(apiErr.Messages, "MISRA entitlement")
}
