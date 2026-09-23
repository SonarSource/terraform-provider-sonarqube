package client

import (
	"context"
	"net/url"
)

// Organization is an organization of SonarQube Cloud.
//
// "search" leaves out an optional field that holds no value, so an unset field
// and an empty field look the same in the answer.
type Organization struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Avatar      string `json:"avatar"`
}

// GetOrganization reads one organization by its key.
//
// The web service has no action that reads a single organization, so this
// searches by key. An empty result becomes ErrNotFound.
//
// Take care with the answer: a token that cannot see the organization gets an
// empty list and a status 200, not a 401. A caller must therefore not read
// "not found" as proof that the organization is absent.
func (c *Client) GetOrganization(ctx context.Context, key string) (*Organization, error) {
	params := url.Values{}
	params.Set("organizations", key)

	var out struct {
		Organizations []Organization `json:"organizations"`
	}
	if err := c.get(ctx, "/api/organizations/search", params, &out); err != nil {
		return nil, err
	}

	// "search" matches the given keys exactly, so at most one entry can carry
	// the key that was asked for.
	for _, org := range out.Organizations {
		if org.Key == key {
			return &org, nil
		}
	}
	return nil, ErrNotFound
}
