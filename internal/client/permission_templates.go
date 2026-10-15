package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// PermissionTemplate is a permission template. An empty Description or
// ProjectKeyPattern means that the template has none.
type PermissionTemplate struct {
	ID                string
	Name              string
	Description       string
	ProjectKeyPattern string
	Grants            []TemplateGrant
}

// Grantee kinds of a TemplateGrant.
const (
	GranteeGroup          = "group"
	GranteeUser           = "user"
	GranteeProjectCreator = "project_creator"
)

// TemplateGrant is one permission that a template gives to a group, a user or
// the project creator. Name is the group name or the login, and it is empty
// for the project creator.
type TemplateGrant struct {
	Kind       string
	Name       string
	Permission string
}

type permissionTemplateAnswer struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	ProjectKeyPattern string `json:"projectKeyPattern"`
	Permissions       []struct {
		Key                string `json:"key"`
		WithProjectCreator bool   `json:"withProjectCreator"`
	} `json:"permissions"`
}

// templateParameters names the organization and the template. SonarQube
// Server has no organizations, so an empty organization sends no parameter.
func templateParameters(organization, id string) url.Values {
	values := url.Values{}
	if organization != "" {
		values.Set("organization", organization)
	}
	if id != "" {
		values.Set("templateId", id)
	}
	return values
}

// CreatePermissionTemplate creates a template without grants and returns its
// ID.
func (c *Client) CreatePermissionTemplate(ctx context.Context, organization string, template PermissionTemplate) (string, error) {
	params := templateParameters(organization, "")
	params.Set("name", template.Name)
	if template.Description != "" {
		params.Set("description", template.Description)
	}
	if template.ProjectKeyPattern != "" {
		params.Set("projectKeyPattern", template.ProjectKeyPattern)
	}
	var answer struct {
		PermissionTemplate permissionTemplateAnswer `json:"permissionTemplate"`
	}
	if err := c.postFor(ctx, "/api/permissions/create_template", params, &answer); err != nil {
		return "", permissionTemplateError(err, organization)
	}
	if answer.PermissionTemplate.ID == "" {
		return "", fmt.Errorf("the answer to create_template has no template id")
	}
	return answer.PermissionTemplate.ID, nil
}

// UpdatePermissionTemplate sets the name, the description and the project
// key pattern of the template with the ID. The server keeps a parameter that
// is not sent, so each one is sent, and an empty value removes the value.
func (c *Client) UpdatePermissionTemplate(ctx context.Context, organization string, template PermissionTemplate) error {
	params := url.Values{
		"id":                {template.ID},
		"name":              {template.Name},
		"description":       {template.Description},
		"projectKeyPattern": {template.ProjectKeyPattern},
	}
	return permissionTemplateError(c.post(ctx, "/api/permissions/update_template", params), organization)
}

// DeletePermissionTemplate deletes the template with the ID. A template that
// is already gone is success. The server refuses to delete the default
// template.
func (c *Client) DeletePermissionTemplate(ctx context.Context, organization, id string) error {
	err := c.postIgnoringNotFound(ctx, "/api/permissions/delete_template", templateParameters(organization, id))
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest &&
		hasMessagePrefix(apiErr.Messages, "It is not possible to delete the default permission template") {
		return fmt.Errorf("the template is the default template. Set a different default template first: %w", err)
	}
	return permissionTemplateError(err, organization)
}

type permissionTemplateList struct {
	PermissionTemplates []permissionTemplateAnswer `json:"permissionTemplates"`
}

// listPermissionTemplates reads all the templates of the organization. The web
// service has no pages.
func (c *Client) listPermissionTemplates(ctx context.Context, organization string) (*permissionTemplateList, error) {
	var answer permissionTemplateList
	if err := c.get(ctx, "/api/permissions/search_templates", templateParameters(organization, ""), &answer); err != nil {
		return nil, permissionTemplateError(err, organization)
	}
	return &answer, nil
}

// GetPermissionTemplate returns the template with the ID and all its grants,
// or ErrNotFound.
//
// search_templates gives the project creator grants, but only counts of the
// other grants. template_groups and template_users give those. They list all
// the groups, the Anyone group and the members of the organization, also the
// ones without permissions, which give no grant.
func (c *Client) GetPermissionTemplate(ctx context.Context, organization, id string) (*PermissionTemplate, error) {
	list, err := c.listPermissionTemplates(ctx, organization)
	if err != nil {
		return nil, err
	}
	answer, err := findByKey(list.PermissionTemplates, id, func(t permissionTemplateAnswer) string { return t.ID })
	if err != nil {
		return nil, err
	}
	template := &PermissionTemplate{ID: answer.ID, Name: answer.Name, Description: answer.Description, ProjectKeyPattern: answer.ProjectKeyPattern}
	for _, permission := range answer.Permissions {
		if permission.WithProjectCreator {
			template.Grants = append(template.Grants, TemplateGrant{Kind: GranteeProjectCreator, Permission: permission.Key})
		}
	}
	for _, kind := range []string{GranteeGroup, GranteeUser} {
		grants, err := c.listTemplateGrants(ctx, organization, id, kind)
		if err != nil {
			return nil, err
		}
		template.Grants = append(template.Grants, grants...)
	}
	return template, nil
}

// listTemplateGrants reads all pages of template_groups or template_users.
func (c *Client) listTemplateGrants(ctx context.Context, organization, id, kind string) ([]TemplateGrant, error) {
	entries, err := c.listPermissions(ctx, "/api/permissions/template_"+kind+"s", templateParameters(organization, id))
	if err != nil {
		return nil, permissionTemplateError(err, organization)
	}
	var grants []TemplateGrant
	for _, entry := range entries {
		name := entry.Name
		if kind == GranteeUser {
			name = entry.Login
		}
		for _, permission := range entry.Permissions {
			grants = append(grants, TemplateGrant{Kind: kind, Name: name, Permission: permission})
		}
	}
	return grants, nil
}

// FindPermissionTemplateID returns the ID of the template with the name, or
// ErrNotFound. The server keeps the names unique without regard to case, and
// it finds a template by name in the same way.
func (c *Client) FindPermissionTemplateID(ctx context.Context, organization, name string) (string, error) {
	list, err := c.listPermissionTemplates(ctx, organization)
	if err != nil {
		return "", err
	}
	for _, answer := range list.PermissionTemplates {
		if strings.EqualFold(answer.Name, name) {
			return answer.ID, nil
		}
	}
	return "", ErrNotFound
}

// AddPermissionTemplateGrant makes the template give the grant. A grant that
// is already there is success. A user must be a member of the organization.
func (c *Client) AddPermissionTemplateGrant(ctx context.Context, organization, id string, grant TemplateGrant) error {
	return c.changePermissionTemplateGrant(ctx, "add_%s_to_template", organization, id, grant)
}

// RemovePermissionTemplateGrant makes the template stop giving the grant. A
// grant that is not there is success.
func (c *Client) RemovePermissionTemplateGrant(ctx context.Context, organization, id string, grant TemplateGrant) error {
	return c.changePermissionTemplateGrant(ctx, "remove_%s_from_template", organization, id, grant)
}

func (c *Client) changePermissionTemplateGrant(ctx context.Context, action, organization, id string, grant TemplateGrant) error {
	// The group web services find the group by its name in this organization.
	params := templateParameters(organization, id)
	params.Set("permission", grant.Permission)
	switch grant.Kind {
	case GranteeGroup:
		params.Set("groupName", grant.Name)
	case GranteeUser:
		params.Set("login", grant.Name)
	case GranteeProjectCreator:
	default:
		return fmt.Errorf("unknown grantee kind %q", grant.Kind)
	}
	err := c.post(ctx, "/api/permissions/"+fmt.Sprintf(action, grant.Kind), params)
	return permissionTemplateError(err, organization)
}

// permissionTemplateError adds hints to the errors of the template web
// services. A missing organization becomes a ScopeNotFoundError. The other
// 404 errors, such as a missing template, group or user, still match
// ErrNotFound.
func permissionTemplateError(err error, organization string) error {
	var apiErr *APIError
	if err == nil || !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case apiErr.StatusCode == http.StatusNotFound && hasMessagePrefix(apiErr.Messages, "No organization with key "):
		return &ScopeNotFoundError{Kind: ScopeOrganization, Key: organization, Cause: err}
	case apiErr.StatusCode == http.StatusForbidden && hasMessageContaining(apiErr.Messages, "is not allowed to use custom permission templates"):
		return fmt.Errorf("the organization %q cannot use custom permission templates. They need the Team or "+
			"the Enterprise plan: %w", organization, err)
	case apiErr.StatusCode == http.StatusBadRequest && hasMessageContaining(apiErr.Messages, "is not member of organization"):
		return fmt.Errorf("the user is not a member of the organization %q. Add the user to the organization "+
			"first: %w", organization, err)
	default:
		return err
	}
}
