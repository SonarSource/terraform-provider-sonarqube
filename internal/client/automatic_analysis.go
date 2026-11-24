package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

const automaticAnalysisSetting = "sonar.autoscan.enabled"

// GetProjectAutomaticAnalysis tells if Automatic Analysis is on for a
// project. The value is the one of the project, or the inherited default when
// the project has none. A missing project answers 404.
func (c *Client) GetProjectAutomaticAnalysis(ctx context.Context, projectKey string) (bool, error) {
	value, err := c.getAutomaticAnalysisValue(ctx, url.Values{"component": {projectKey}}, "project "+projectKey)
	return value.Enabled, err
}

// SetProjectAutomaticAnalysis turns Automatic Analysis on or off for a
// project. The setting is not a normal setting, so api/settings/set cannot
// change it. To turn it on, the project must be the only project bound to its
// repository, and the server then starts an eligibility check.
func (c *Client) SetProjectAutomaticAnalysis(ctx context.Context, projectKey string, enabled bool) error {
	params := url.Values{"projectKey": {projectKey}, "enable": {strconv.FormatBool(enabled)}}
	return c.post(ctx, "/api/autoscan/activation", params)
}

// AutomaticAnalysisValue is the Automatic Analysis setting of a scope.
type AutomaticAnalysisValue struct {
	Enabled bool
	// Own tells that the scope has its own value, and does not inherit it.
	Own bool
}

// GetOrganizationAutomaticAnalysis tells if new projects of an organization
// get Automatic Analysis, and if the organization has its own value. The
// server answers with the default also for an organization that does not
// exist.
func (c *Client) GetOrganizationAutomaticAnalysis(ctx context.Context, organization string) (AutomaticAnalysisValue, error) {
	return c.getAutomaticAnalysisValue(ctx, url.Values{"organization": {organization}}, "organization "+organization)
}

// SetOrganizationAutomaticAnalysis sets the Automatic Analysis value for new
// projects of an organization. Only an organization on the Enterprise plan
// can have its own value. The server refuses the others with 403.
func (c *Client) SetOrganizationAutomaticAnalysis(ctx context.Context, organization string, enabled bool) error {
	params := url.Values{"organization": {organization}, "key": {automaticAnalysisSetting}, "value": {strconv.FormatBool(enabled)}}
	return c.post(ctx, "/api/settings/set", params)
}

// ResetOrganizationAutomaticAnalysis removes the value of the organization,
// so that new projects get the default of the instance again. The server
// accepts a reset when the organization has no value.
func (c *Client) ResetOrganizationAutomaticAnalysis(ctx context.Context, organization string) error {
	params := url.Values{"organization": {organization}, "keys": {automaticAnalysisSetting}}
	return c.post(ctx, "/api/settings/reset", params)
}

// getAutomaticAnalysisValue reads the setting for the scope in params.
// subject names the scope in the errors.
func (c *Client) getAutomaticAnalysisValue(ctx context.Context, params url.Values, subject string) (AutomaticAnalysisValue, error) {
	params.Set("keys", automaticAnalysisSetting)
	var result struct {
		Settings []struct {
			Key       string `json:"key"`
			Value     string `json:"value"`
			Inherited bool   `json:"inherited"`
		} `json:"settings"`
	}
	if err := c.get(ctx, "/api/settings/values", params, &result); err != nil {
		return AutomaticAnalysisValue{}, err
	}
	for _, setting := range result.Settings {
		if setting.Key != automaticAnalysisSetting {
			continue
		}
		enabled, err := strconv.ParseBool(setting.Value)
		if err != nil {
			return AutomaticAnalysisValue{}, fmt.Errorf("the setting %s of the %s has the value %q, which is not a boolean",
				automaticAnalysisSetting, subject, setting.Value)
		}
		return AutomaticAnalysisValue{Enabled: enabled, Own: !setting.Inherited}, nil
	}
	return AutomaticAnalysisValue{}, fmt.Errorf("the settings of the %s do not contain %s", subject, automaticAnalysisSetting)
}
