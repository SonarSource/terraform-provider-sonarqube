package client

import (
	"context"
	"net/url"
)

// Organization is an organization of SonarQube Cloud.
type Organization struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	AvatarURL   string `json:"avatarUrl"`
}

// GetOrganization reads one organization by its key through Web API v2, which
// answers 404 when no organization carries the key.
//
// A 404 is no proof that the organization is absent: an organization that the
// token may not see answers in the same way.
func (c *Client) GetOrganization(ctx context.Context, key string) (*Organization, error) {
	params := url.Values{}
	params.Set("organizationKey", key)

	// The path repeats the word because the first segment is the stage of the
	// API gateway and the second is the resource.
	var out []Organization
	if err := c.apiGet(ctx, "/organizations/organizations", params, &out); err != nil {
		return nil, err
	}

	for _, org := range out {
		if org.Key == key {
			return &org, nil
		}
	}
	return nil, ErrNotFound
}
