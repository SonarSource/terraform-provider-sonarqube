package client

import (
	"context"
	"net/url"
)

// Project is a SonarQube Cloud project.
type Project struct {
	// ID is the UUID of the main branch.
	ID string `json:"id"`
	// LegacyID is the internal identifier of the project. The project
	// bindings API names a project by this identifier and by nothing else.
	LegacyID       string `json:"legacyId"`
	Key            string `json:"key"`
	Name           string `json:"name"`
	Visibility     string `json:"visibility"`
	OrganizationID string `json:"organizationId"`
}

// GetProject finds a project within an organization by its exact key through
// Web API v2.
//
// The list call needs the UUID of the organization, so the organization is
// read first. A missing project is an empty list and not a 404, and so is a
// project of another organization. An organization that is missing answers
// 404, and then the project is gone as well.
func (c *Client) GetProject(ctx context.Context, organization, key string) (*Project, error) {
	org, err := c.GetOrganization(ctx, organization)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("keys", key)
	params.Set("organizationIds", org.UUIDV4)

	// The path repeats the word for the same reason as the organization path.
	var result struct {
		Projects []Project `json:"projects"`
	}
	if err := c.apiGet(ctx, "/projects/projects", params, &result); err != nil {
		return nil, err
	}
	return findByKey(result.Projects, key, func(project Project) string { return project.Key })
}

// CreateProject creates a project without a repository binding.
func (c *Client) CreateProject(ctx context.Context, organization, key, name string) error {
	params := url.Values{}
	params.Set("organization", organization)
	params.Set("project", key)
	params.Set("name", name)
	return c.post(ctx, "/api/projects/create", params)
}

// DeleteProject removes a project. A project that is already gone is success.
func (c *Client) DeleteProject(ctx context.Context, key string) error {
	params := url.Values{}
	params.Set("project", key)
	return c.postIgnoringNotFound(ctx, "/api/projects/delete", params)
}
