package client

import (
	"context"
	"fmt"
	"net/url"
)

// gitlabConfigurationsPath is the collection of GitLab Self-Managed
// configurations in Web API v2.
const gitlabConfigurationsPath = "/gitlab-configurations"

// GitLabConfiguration names the GitLab Self-Managed instance of an
// organization. A binding to GitLab Self-Managed needs one.
type GitLabConfiguration struct {
	ID string `json:"id"`
	// OrganizationID is the UUID of the organization.
	OrganizationID string `json:"organizationId"`
	APIURL         string `json:"apiUrl"`
	WebURL         string `json:"webUrl"`
}

// GitLabConfigurationRequest is the body of the create call.
type GitLabConfigurationRequest struct {
	OrganizationID      string `json:"organizationId"`
	APIURL              string `json:"apiUrl"`
	WebURL              string `json:"webUrl"`
	PersonalAccessToken string `json:"personalAccessToken"`
	Group               string `json:"group"`
}

// GitLabConfigurationPatch is the body of the change call. An empty field
// stays out of the request.
type GitLabConfigurationPatch struct {
	APIURL              string `json:"apiUrl,omitempty"`
	WebURL              string `json:"webUrl,omitempty"`
	PersonalAccessToken string `json:"personalAccessToken,omitempty"`
}

// CreateGitLabConfiguration stores the instance of an organization. A refusal
// of a field names it in RelatedField.
func (c *Client) CreateGitLabConfiguration(ctx context.Context, req GitLabConfigurationRequest) (*GitLabConfiguration, error) {
	var out GitLabConfiguration
	if err := c.apiPost(ctx, gitlabConfigurationsPath, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FindGitLabConfiguration returns the configuration of an organization, named
// by its UUID. An organization without one gives ErrNotFound.
func (c *Client) FindGitLabConfiguration(ctx context.Context, organizationUUID string) (*GitLabConfiguration, error) {
	params := url.Values{}
	params.Set("organizationId", organizationUUID)

	var out struct {
		GitLabConfigurations []GitLabConfiguration `json:"gitlabConfigurations"`
	}
	if err := c.apiGet(ctx, gitlabConfigurationsPath, params, &out); err != nil {
		return nil, err
	}
	switch len(out.GitLabConfigurations) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return &out.GitLabConfigurations[0], nil
	default:
		return nil, fmt.Errorf("organization %q has %d GitLab configurations, want 1",
			organizationUUID, len(out.GitLabConfigurations))
	}
}

// UpdateGitLabConfiguration changes the addresses of a configuration. It takes
// a JSON merge patch.
func (c *Client) UpdateGitLabConfiguration(ctx context.Context, id string, req GitLabConfigurationPatch) (*GitLabConfiguration, error) {
	var out GitLabConfiguration
	if err := c.apiMergePatch(ctx, gitlabConfigurationsPath+"/"+url.PathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
