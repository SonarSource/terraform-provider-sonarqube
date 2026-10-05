package client

import (
	"context"
	"errors"
	"net/url"
)

const qualityGateSettingsPath = "/quality-gates/settings"

// OrganizationQualityGateSettings holds the quality gate settings of an organization.
type OrganizationQualityGateSettings struct {
	ID                 string `json:"id"`
	IgnoreSmallChanges bool   `json:"ignoreSmallChanges"`
}

// GetOrganizationQualityGateSettings reads settings by the UUID of the organization.
// A patch needs the settings ID, so an answer without one is an error.
func (c *Client) GetOrganizationQualityGateSettings(ctx context.Context, organizationUUID string) (*OrganizationQualityGateSettings, error) {
	params := url.Values{}
	params.Set("resourceId", organizationUUID)
	params.Set("resourceType", "ORGANIZATION")

	var settings OrganizationQualityGateSettings
	if err := c.apiGet(ctx, qualityGateSettingsPath, params, &settings); err != nil {
		return nil, err
	}
	if settings.ID == "" {
		return nil, errors.New("the API returned no settings identifier")
	}
	return &settings, nil
}

// UpdateOrganizationQualityGateSettings changes the small-changes setting.
func (c *Client) UpdateOrganizationQualityGateSettings(ctx context.Context, id string, ignoreSmallChanges bool) error {
	return c.apiPatch(ctx, qualityGateSettingsPath+"/"+url.PathEscape(id), struct {
		IgnoreSmallChanges bool `json:"ignoreSmallChanges"`
	}{IgnoreSmallChanges: ignoreSmallChanges}, nil)
}
