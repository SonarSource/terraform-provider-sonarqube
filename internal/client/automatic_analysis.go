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
	params := url.Values{"component": {projectKey}, "keys": {automaticAnalysisSetting}}
	var result struct {
		Settings []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"settings"`
	}
	if err := c.get(ctx, "/api/settings/values", params, &result); err != nil {
		return false, err
	}
	for _, setting := range result.Settings {
		if setting.Key != automaticAnalysisSetting {
			continue
		}
		enabled, err := strconv.ParseBool(setting.Value)
		if err != nil {
			return false, fmt.Errorf("the setting %s of the project %s has the value %q, which is not a boolean",
				automaticAnalysisSetting, projectKey, setting.Value)
		}
		return enabled, nil
	}
	return false, fmt.Errorf("the settings of the project %s do not contain %s", projectKey, automaticAnalysisSetting)
}

// SetProjectAutomaticAnalysis turns Automatic Analysis on or off for a
// project. The setting is not a normal setting, so api/settings/set cannot
// change it. To turn it on, the project must be the only project bound to its
// repository, and the server then starts an eligibility check.
func (c *Client) SetProjectAutomaticAnalysis(ctx context.Context, projectKey string, enabled bool) error {
	params := url.Values{"projectKey": {projectKey}, "enable": {strconv.FormatBool(enabled)}}
	return c.post(ctx, "/api/autoscan/activation", params)
}
