package client

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

// projectBindingsPath is the collection of project bindings in Web API v2.
const projectBindingsPath = "/dop-translation/project-bindings"

// ProjectBinding ties a SonarQube Cloud project to a repository on the DevOps
// platform that its organization is bound to.
type ProjectBinding struct {
	ID string `json:"id"`
	// ProjectID is the internal identifier of the project, not its key.
	ProjectID      string `json:"projectId"`
	DevOpsPlatform string `json:"devOpsPlatform"`
	// RepositoryID is the identifier that the DevOps platform gives the
	// repository. For GitHub it is a number, sent as a string.
	RepositoryID string `json:"repositoryId"`
	// Slug is "owner/name" for GitHub.
	Slug string `json:"slug"`
	URL  string `json:"url"`
}

// createProjectBindingRequest is the body of a bind call.
//
// For GitHub, repositoryId takes the slug "owner/name". It does not take the
// number that a read reports in the field of the same name: the server gives
// that value to GitHub, and GitHub answers 404 for a number.
type createProjectBindingRequest struct {
	ProjectID    string `json:"projectId"`
	RepositoryID string `json:"repositoryId"`
}

// patchProjectBindingRequest is the body of a change call. The repository is
// the only field that can change.
type patchProjectBindingRequest struct {
	RepositoryID string `json:"repositoryId"`
}

// ErrNotBound reports a project that exists but is not bound to a
// repository. A project that does not exist gives ErrNotFound.
var ErrNotBound = errors.New("project not bound")

// CreateProjectBinding binds a project to a repository. repository is the
// slug "owner/name" for GitHub.
//
// It takes the project that GetProject returns rather than an identifier, so
// that no caller can give the key or the UUID where the legacy identifier
// belongs. The compiler cannot see such a mistake between two strings.
//
// The server refuses the bind when the organization of the project is not
// bound, when its application installation cannot see the repository, when
// the project is public and the repository is private, when the project is
// already bound, and when one other project of the organization is already
// bound to the repository.
func (c *Client) CreateProjectBinding(ctx context.Context, project *Project, repository string) (*ProjectBinding, error) {
	var out ProjectBinding
	req := createProjectBindingRequest{ProjectID: project.LegacyID, RepositoryID: repository}
	if err := c.apiPost(ctx, projectBindingsPath, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FindProjectBinding returns the binding of the project that GetProject
// returns.
//
// A project that is not bound gives ErrNotBound. The server answers such a
// project with a 200 and an empty collection, unlike the search of
// organization bindings, which answers 404. A project that was deleted after
// GetProject read it answers 404, which gives ErrNotFound.
func (c *Client) FindProjectBinding(ctx context.Context, project *Project) (*ProjectBinding, error) {
	params := url.Values{}
	params.Set("projectId", project.LegacyID)

	var out struct {
		Bindings []ProjectBinding `json:"bindings"`
	}
	if err := c.apiGet(ctx, projectBindingsPath, params, &out); err != nil {
		return nil, err
	}
	if len(out.Bindings) == 0 {
		return nil, ErrNotBound
	}
	if len(out.Bindings) > 1 {
		return nil, fmt.Errorf("project %q has %d bindings, want 1", project.Key, len(out.Bindings))
	}
	return &out.Bindings[0], nil
}

// UpdateProjectBinding binds an existing binding to a different repository.
func (c *Client) UpdateProjectBinding(ctx context.Context, id, repository string) (*ProjectBinding, error) {
	var out ProjectBinding
	req := patchProjectBindingRequest{RepositoryID: repository}
	if err := c.apiMergePatch(ctx, projectBindingsPath+"/"+url.PathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// A delete is deliberately absent. The API has no such operation. A binding
// goes away when its project is deleted.
