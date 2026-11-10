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
// organization. A binding to GitLab Self-Managed needs one, and it reaches
// GitLab only through it. An organization has at most one.
type GitLabConfiguration struct {
	ID string `json:"id"`
	// OrganizationID is the organization in the form of a UUID. This API does
	// not accept the internal identifier that the bindings API takes.
	OrganizationID string `json:"organizationId"`
	APIURL         string `json:"apiUrl"`
	WebURL         string `json:"webUrl"`
}

// GitLabConfigurationRequest is the body of the create call.
//
// The server uses the token and the group only to check that it can reach the
// group. It does not store them. The binding holds the token.
type GitLabConfigurationRequest struct {
	OrganizationID      string `json:"organizationId"`
	APIURL              string `json:"apiUrl"`
	WebURL              string `json:"webUrl"`
	PersonalAccessToken string `json:"personalAccessToken"`
	Group               string `json:"group"`
}

// GitLabConfigurationPatch is the body of the change call. A field that
// holds nothing stays out of the request, and the server keeps its value.
type GitLabConfigurationPatch struct {
	APIURL              string `json:"apiUrl,omitempty"`
	WebURL              string `json:"webUrl,omitempty"`
	PersonalAccessToken string `json:"personalAccessToken,omitempty"`
}

// CreateGitLabConfiguration stores the instance of an organization. The
// server checks the connection to the group with the token first.
//
// A refusal is a 400 that names the field at fault in RelatedField: an
// address that is not public, an instance that the server cannot reach, or a
// token that GitLab refuses.
//
// The server refuses an organization without an Enterprise plan with a 403,
// and an organization that already has a configuration with a 409.
func (c *Client) CreateGitLabConfiguration(ctx context.Context, req GitLabConfigurationRequest) (*GitLabConfiguration, error) {
	var out GitLabConfiguration
	if err := c.apiPost(ctx, gitlabConfigurationsPath, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FindGitLabConfiguration returns the configuration of one organization,
// which is named by its UUID. An organization without one gives ErrNotFound.
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

// UpdateGitLabConfiguration changes the addresses of the instance. The server
// checks the connection again with the token of the request.
//
// The endpoint accepts a JSON merge patch only. Plain JSON gives a 415.
func (c *Client) UpdateGitLabConfiguration(ctx context.Context, id string, req GitLabConfigurationPatch) (*GitLabConfiguration, error) {
	var out GitLabConfiguration
	if err := c.apiMergePatch(ctx, gitlabConfigurationsPath+"/"+url.PathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
