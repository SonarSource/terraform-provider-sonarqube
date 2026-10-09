package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
)

// Group is a user group. ID stays opaque across SonarQube products.
type Group struct {
	ID          string
	Name        string
	Description string
	Default     bool
}

type cloudGroup struct {
	ID          json.Number `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Default     bool        `json:"default"`
}

func (g cloudGroup) group() Group {
	return Group{ID: string(g.ID), Name: g.Name, Description: g.Description, Default: g.Default}
}

// CreateGroup creates a custom group and returns its numeric Cloud ID. An
// empty description makes a group with no description.
func (c *Client) CreateGroup(ctx context.Context, organization, name, description string) (*Group, error) {
	params := url.Values{"organization": {organization}, "name": {name}, "description": {description}}
	var answer struct {
		Group cloudGroup `json:"group"`
	}
	if err := c.postFor(ctx, "/api/user_groups/create", params, &answer); err != nil {
		return nil, err
	}
	if answer.Group.ID == "" {
		// A search cannot find the id at once, because the group search is
		// eventually consistent. The group can exist, so the error says so.
		return nil, fmt.Errorf("the create answer for group %q has no id. The group can exist: "+
			"if it does, import it with <organization>/<name>", name)
	}
	group := answer.Group.group()
	return &group, nil
}

// UpdateGroup sets the name and the description of a group. An empty
// description removes it.
//
// It always sends both fields. SonarQube Cloud applies an update to a copy of
// the group that can be some seconds old, and writes back the fields that the
// request does not send. An update of the description only, soon after a
// rename, thus gives the group its old name again.
func (c *Client) UpdateGroup(ctx context.Context, id, name, description string) error {
	params := url.Values{"id": {id}, "name": {name}, "description": {description}}
	return c.post(ctx, "/api/user_groups/update", params)
}

// DeleteGroup deletes a custom group. A group already absent is accepted.
func (c *Client) DeleteGroup(ctx context.Context, id string) error {
	return c.postIgnoringNotFound(ctx, "/api/user_groups/delete", url.Values{"id": {id}})
}

// ListGroups reads every page. A non-empty query keeps the groups whose name
// holds it, so a caller still has to match the exact name.
//
// A missing organization answers 404, and so does an organization that the
// token cannot read. The error is then ErrOrganizationNotFound.
func (c *Client) ListGroups(ctx context.Context, organization, query string) ([]Group, error) {
	const pageSize = 500
	var groups []Group
	for page := 1; ; page++ {
		params := url.Values{
			"organization": {organization},
			"p":            {strconv.Itoa(page)},
			"ps":           {strconv.Itoa(pageSize)},
		}
		if query != "" {
			params.Set("q", query)
		}
		var answer struct {
			Groups []cloudGroup `json:"groups"`
			Paging struct {
				Total int `json:"total"`
			} `json:"paging"`
		}
		if err := c.get(ctx, "/api/user_groups/search", params, &answer); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, fmt.Errorf("%w: %q: %s", ErrOrganizationNotFound, organization, err)
			}
			return nil, fmt.Errorf("cannot search groups in organization %q: %w", organization, err)
		}
		for _, item := range answer.Groups {
			groups = append(groups, item.group())
		}
		// The total decides the end, and not the page size: a server that
		// caps the page size lower would otherwise end the search at page 1.
		if len(groups) >= answer.Paging.Total {
			return groups, nil
		}
		if len(answer.Groups) == 0 {
			return nil, fmt.Errorf("group page %d was empty before the reported total", page)
		}
	}
}

// GetGroup finds a group by its stable ID, including after an outside rename.
func (c *Client) GetGroup(ctx context.Context, organization, id string) (*Group, error) {
	groups, err := c.ListGroups(ctx, organization, "")
	if err != nil {
		return nil, err
	}
	return findByKey(groups, id, func(group Group) string { return group.ID })
}

// FindGroupByName finds an exact name for import and data source reads. The
// query narrows the search to the names that hold the name.
func (c *Client) FindGroupByName(ctx context.Context, organization, name string) (*Group, error) {
	groups, err := c.ListGroups(ctx, organization, name)
	if err != nil {
		return nil, err
	}
	return findByKey(groups, name, func(group Group) string { return group.Name })
}
