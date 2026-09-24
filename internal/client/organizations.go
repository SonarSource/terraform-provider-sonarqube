package client

import (
	"context"
	"errors"
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

// OrganizationRequest carries the fields of an organization that can be
// written.
//
// Each field is a pointer, because the update action reads an absent parameter
// and an empty parameter differently. An absent parameter keeps the current
// value. An empty parameter clears the field. A nil field stays out of the
// request.
type OrganizationRequest struct {
	Key         string
	Name        *string
	Description *string
	URL         *string
	Avatar      *string
}

func (r OrganizationRequest) values() url.Values {
	params := url.Values{}
	if r.Name != nil {
		params.Set("name", *r.Name)
	}
	if r.Description != nil {
		params.Set("description", *r.Description)
	}
	if r.URL != nil {
		params.Set("url", *r.URL)
	}
	if r.Avatar != nil {
		params.Set("avatar", *r.Avatar)
	}
	return params
}

// CreateOrganization creates an organization.
//
// The answer of the web service is discarded, because it names its fields as
// Web API v1 does. The caller reads the organization back with
// GetOrganization, which keeps one conversion path.
func (c *Client) CreateOrganization(ctx context.Context, req OrganizationRequest) error {
	params := req.values()
	params.Set("key", req.Key)

	return c.post(ctx, "/api/organizations/create", params)
}

// UpdateOrganization writes the fields of an organization that can change. The
// key is not one of them: UpdateOrganizationKey changes that.
func (c *Client) UpdateOrganization(ctx context.Context, req OrganizationRequest) error {
	params := req.values()
	params.Set("key", req.Key)

	return c.post(ctx, "/api/organizations/update", params)
}

// UpdateOrganizationKey renames an organization in place.
//
// The organization and everything in it stay, so this is not the same as a
// delete and a create. The update action cannot do it, because it finds the
// organization by the key.
func (c *Client) UpdateOrganizationKey(ctx context.Context, key, newKey string) error {
	params := url.Values{}
	params.Set("key", key)
	params.Set("newKey", newKey)

	return c.post(ctx, "/api/organizations/update_key", params)
}

// DeleteOrganization deletes an organization. An organization that is already
// gone is not an error, so that a repeated delete succeeds.
func (c *Client) DeleteOrganization(ctx context.Context, key string) error {
	params := url.Values{}
	params.Set("organization", key)

	if err := c.post(ctx, "/api/organizations/delete", params); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}
