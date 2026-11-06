package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
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

// SearchProjectKeys returns the keys of the projects of an organization whose
// key or name holds text.
func (c *Client) SearchProjectKeys(ctx context.Context, organization, text string) ([]string, error) {
	return collectPages("project", func(page int) ([]string, int, error) {
		params := url.Values{"organization": {organization}, "q": {text}, "p": {strconv.Itoa(page)}, "ps": {"500"}}
		var answer struct {
			Components []struct {
				Key string `json:"key"`
			} `json:"components"`
			Paging struct {
				Total int `json:"total"`
			} `json:"paging"`
		}
		if err := c.get(ctx, "/api/projects/search", params, &answer); err != nil {
			return nil, 0, err
		}
		keys := make([]string, len(answer.Components))
		for i, project := range answer.Components {
			keys[i] = project.Key
		}
		return keys, answer.Paging.Total, nil
	})
}

// projectFilterChunk and projectFilterBytes limit the values in one project
// list request. A project key can have 400 characters, and gateways commonly
// refuse a request line of more than 8 KB, so the number of values alone is
// not sufficient.
const (
	projectFilterChunk = 50
	projectFilterBytes = 4000
)

// filterChunks joins the values with commas into filter values of at most
// projectFilterChunk values and projectFilterBytes encoded bytes. A value
// that is longer than the byte limit is a chunk by itself.
func filterChunks(values []string) []string {
	var chunks, chunk []string
	size := 0
	for _, value := range values {
		length := len(url.QueryEscape(value)) + len("%2C")
		if len(chunk) > 0 && (len(chunk) == projectFilterChunk || size+length > projectFilterBytes) {
			chunks = append(chunks, strings.Join(chunk, ","))
			chunk, size = nil, 0
		}
		chunk = append(chunk, value)
		size += length
	}
	if len(chunk) > 0 {
		chunks = append(chunks, strings.Join(chunk, ","))
	}
	return chunks
}

// FindProjectsByKey returns the projects of the organization that have one of
// the keys. A key without a project is absent from the result.
func (c *Client) FindProjectsByKey(ctx context.Context, organizationID string, keys []string) ([]Project, error) {
	return c.findProjects(ctx, organizationID, "keys", keys)
}

// FindProjectsByID returns the projects of the organization that have one of
// the UUIDs. A UUID without a project is absent from the result.
func (c *Client) FindProjectsByID(ctx context.Context, organizationID string, ids []string) ([]Project, error) {
	return c.findProjects(ctx, organizationID, "ids", ids)
}

// The list filters accept a list only as one comma-separated value. When the
// parameter is repeated, the server uses only the last value.
func (c *Client) findProjects(ctx context.Context, organizationID, filter string, values []string) ([]Project, error) {
	var all []Project
	for _, chunk := range filterChunks(values) {
		projects, err := collectPages("project", func(page int) ([]Project, int, error) {
			return c.projectPage(ctx, organizationID, filter, chunk, page)
		})
		if err != nil {
			return nil, err
		}
		all = append(all, projects...)
	}
	return all, nil
}

func (c *Client) projectPage(ctx context.Context, organizationID, filter, value string, page int) ([]Project, int, error) {
	const pageSize = 500
	params := url.Values{filter: {value}, "organizationIds": {organizationID}, "pageIndex": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}
	var result struct {
		Projects []Project `json:"projects"`
		Page     *struct {
			Total *int `json:"total"`
		} `json:"page"`
	}
	if err := c.apiGet(ctx, "/projects/projects", params, &result); err != nil {
		return nil, 0, err
	}
	if result.Projects == nil || result.Page == nil || result.Page.Total == nil || *result.Page.Total < 0 {
		return nil, 0, fmt.Errorf("the project list has no valid collection or page total")
	}
	for _, project := range result.Projects {
		if project.ID == "" || project.Key == "" || project.OrganizationID != organizationID {
			return nil, 0, fmt.Errorf("the project list contains a missing ID or key, or a different organization")
		}
	}
	return result.Projects, *result.Page.Total, nil
}
