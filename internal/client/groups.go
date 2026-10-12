package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const groupsPath = "/users/groups"

// Group is a user group of an organization. ID is a UUID.
type Group struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// BuiltIn is true for the Members group of each organization. The Owners
	// group is not built in, although each organization has one.
	BuiltIn bool `json:"builtIn"`
	// Managed is true for a group that an identity provider synchronizes
	// through SCIM. SonarQube Cloud refuses changes to such a group.
	Managed bool `json:"managed"`
}

// CreateGroup creates a custom group. An empty description makes a group with
// no description.
func (c *Client) CreateGroup(ctx context.Context, organization, name, description string) (*Group, error) {
	organizationID, err := c.organizationUUID(ctx, organization)
	if err != nil {
		return nil, err
	}
	body := map[string]string{"organizationId": organizationID, "name": name, "description": description}
	var group Group
	if err := c.apiPost(ctx, groupsPath, body, &group); err != nil {
		return nil, err
	}
	if group.ID == "" {
		return nil, fmt.Errorf("the create answer for group %q has no id. The group can exist: "+
			"if it does, import it with <organization>/<name>", name)
	}
	return &group, nil
}

// UpdateGroup sets the name and the description of a group. An empty
// description removes it.
func (c *Client) UpdateGroup(ctx context.Context, id, name, description string) error {
	body := map[string]string{"name": name, "description": description}
	return c.apiMergePatch(ctx, groupsPath+"/"+url.PathEscape(id), body, nil)
}

// DeleteGroup deletes a group. A group that is already gone is success.
func (c *Client) DeleteGroup(ctx context.Context, id string) error {
	err := c.apiSend(ctx, http.MethodDelete, groupsPath+"/"+url.PathEscape(id), nil, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// GetGroup reads a group of an organization by its id, also after a rename
// outside Terraform. A group that the organization does not hold is
// ErrNotFound.
func (c *Client) GetGroup(ctx context.Context, organization, id string) (*Group, error) {
	groups, err := c.listGroups(ctx, organization, url.Values{"ids": {id}})
	if err != nil {
		return nil, err
	}
	return findByKey(groups, id, func(group Group) string { return group.ID })
}

// FindGroupByName finds a group by its exact name. The q filter keeps the
// groups whose name holds the value, so the exact match happens here.
func (c *Client) FindGroupByName(ctx context.Context, organization, name string) (*Group, error) {
	groups, err := c.listGroups(ctx, organization, url.Values{"q": {name}})
	if err != nil {
		return nil, err
	}
	return findByKey(groups, name, func(group Group) string { return group.Name })
}

// listGroups reads every page of the groups of an organization that match
// filter.
func (c *Client) listGroups(ctx context.Context, organization string, filter url.Values) ([]Group, error) {
	organizationID, err := c.organizationUUID(ctx, organization)
	if err != nil {
		return nil, err
	}
	const pageSize = 500
	var groups []Group
	for page := 1; ; page++ {
		params := url.Values{
			"organizationIds": {organizationID},
			"pageIndex":       {strconv.Itoa(page)},
			"pageSize":        {strconv.Itoa(pageSize)},
		}
		for key, values := range filter {
			params[key] = values
		}
		var result struct {
			Groups []Group `json:"groups"`
			Page   struct {
				Total int `json:"total"`
			} `json:"page"`
		}
		if err := c.apiGet(ctx, groupsPath, params, &result); err != nil {
			return nil, err
		}
		groups = append(groups, result.Groups...)
		if len(groups) >= result.Page.Total {
			return groups, nil
		}
		if len(result.Groups) == 0 {
			return nil, fmt.Errorf("group page %d was empty before the reported total", page)
		}
	}
}

// organizationUUID reads the UUID of an organization, which the group API
// takes in place of the key. The group API answers 403 for an organization
// that does not exist, so this read is the one that finds a missing
// organization.
func (c *Client) organizationUUID(ctx context.Context, organization string) (string, error) {
	org, err := c.GetOrganization(ctx, organization)
	if errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("%w: %q: %s", ErrOrganizationNotFound, organization, err)
	}
	if err != nil {
		return "", err
	}
	return org.UUIDV4, nil
}
