package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Organization is an organization of SonarQube Cloud.
type Organization struct {
	// ID is the internal identifier, an opaque string of 20 characters. The
	// bindings API names an organization by this and by nothing else, so a
	// read of the organization is how a key reaches that API.
	ID string `json:"id"`
	// UUIDV4 is the same organization in the form of a UUID.
	UUIDV4      string `json:"uuidV4"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	AvatarURL   string `json:"avatarUrl"`
	// DefaultQualityGateUUID is the gate of each project that has no gate of
	// its own.
	DefaultQualityGateUUID string `json:"defaultQualityGateUuid"`
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

	return findByKey(out, key, func(org Organization) string { return org.Key })
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

// organizationDeleteWaits are the pauses between the tries of
// DeleteOrganization. They add up to 30 seconds.
var organizationDeleteWaits = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second}

// DeleteOrganization deletes an organization. An organization that is already
// gone is not an error, so that a repeated delete succeeds.
//
// Soon after a change, the server can refuse the delete for a short time, so
// a transient failure is tried again after each of organizationDeleteWaits.
func (c *Client) DeleteOrganization(ctx context.Context, key string) error {
	params := url.Values{}
	params.Set("organization", key)

	return retry(ctx, organizationDeleteWaits, func() error {
		return c.postIgnoringNotFound(ctx, "/api/organizations/delete", params)
	})
}

// ListMemberOrganizationKeys returns the keys of every organization that the
// token user is a member of.
func (c *Client) ListMemberOrganizationKeys(ctx context.Context) ([]string, error) {
	return collectPages("organization", func(page int) ([]string, int, error) {
		params := url.Values{"member": {"true"}, "p": {strconv.Itoa(page)}, "ps": {"500"}}
		var answer struct {
			Organizations []struct {
				Key string `json:"key"`
			} `json:"organizations"`
			Paging struct {
				Total int `json:"total"`
			} `json:"paging"`
		}
		if err := c.get(ctx, "/api/organizations/search", params, &answer); err != nil {
			return nil, 0, err
		}
		keys := make([]string, len(answer.Organizations))
		for i, org := range answer.Organizations {
			keys[i] = org.Key
		}
		return keys, answer.Paging.Total, nil
	})
}

// GetCurrentPlan returns the key of the plan of an organization in lower case,
// such as "free_v2", "team" or "enterprise".
func (c *Client) GetCurrentPlan(ctx context.Context, organizationUUID string) (string, error) {
	var answer struct {
		PlanKey string `json:"planKey"`
	}
	if err := c.apiGet(ctx, "/billing/current-plan", url.Values{"organizationId": {organizationUUID}}, &answer); err != nil {
		return "", err
	}
	if answer.PlanKey == "" {
		return "", fmt.Errorf("the plan answer for organization %s has no planKey", organizationUUID)
	}
	return strings.ToLower(answer.PlanKey), nil
}
