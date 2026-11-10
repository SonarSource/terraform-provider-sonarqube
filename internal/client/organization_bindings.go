package client

import (
	"context"
	"fmt"
	"net/url"
)

// bindingsPath is the collection of organization bindings in Web API v2.
const bindingsPath = "/dop-translation/organization-bindings"

// PlatformGitHub and PlatformGitLab are the DevOps platforms that this
// provider binds to. It binds to GitLab Self-Managed only, not to gitlab.com.
//
// The API also takes azure and bitbucket. Neither is tested against a live
// instance, so neither is offered.
const (
	PlatformGitHub = "github"
	PlatformGitLab = "gitlab"
)

// FlavorGitLabSelfManaged is the dopFlavor of a binding to GitLab
// Self-Managed. A binding to gitlab.com has no flavor.
const FlavorGitLabSelfManaged = "GITLAB_SELF_MANAGED"

// OrganizationBinding ties an organization of SonarQube Cloud to an
// organization on a DevOps platform.
type OrganizationBinding struct {
	ID                    string `json:"id"`
	OrganizationID        string `json:"organizationId"`
	OrganizationUUIDV4    string `json:"organizationUuidV4"`
	DevOpsPlatform        string `json:"devOpsPlatform"`
	BindingType           string `json:"bindingType"`
	InstallationID        string `json:"installationId"`
	DevOpsPlatformURL     string `json:"devOpsPlatformUrl"`
	RepoAutoImportEnabled *bool  `json:"repoAutoImportEnabled,omitempty"`
	// DopFlavor names a variant of a platform, such as a GitHub Enterprise
	// variant or FlavorGitLabSelfManaged.
	DopFlavor string `json:"dopFlavor,omitempty"`
	// DevOpsPlatformOrganizationID is the numeric identifier of the GitLab
	// group. The bind call takes the path of the group in the same field,
	// and a read reports the number.
	DevOpsPlatformOrganizationID string `json:"devOpsPlatformOrganizationId,omitempty"`
	// TenantID is the web address of the GitLab Self-Managed instance.
	TenantID string `json:"tenantId,omitempty"`
}

// CreateGitLabSelfManagedBindingRequest is the body of a bind call to GitLab
// Self-Managed. The organization must have a GitLabConfiguration first: the
// server reaches the instance through it.
//
// The server stores the token in the binding, and no read returns it.
type CreateGitLabSelfManagedBindingRequest struct {
	OrganizationID string `json:"organizationId"`
	DevOpsPlatform string `json:"devOpsPlatform"`
	DopFlavor      string `json:"dopFlavor"`
	AccessToken    string `json:"accessToken"`
	// Group is the path of the top-level group, such as my-group.
	Group string `json:"devOpsPlatformOrganizationId"`
}

// PatchGitLabSelfManagedBindingRequest is the body of a change call to a
// binding to GitLab Self-Managed. A field that holds nothing stays out.
//
// The server checks a new token against GitLab. TenantID, the web address of
// a new instance, must come with DevOpsPlatformURL, the address of the group
// on that instance.
type PatchGitLabSelfManagedBindingRequest struct {
	AccessToken       string `json:"accessToken,omitempty"`
	TenantID          string `json:"tenantId,omitempty"`
	DevOpsPlatformURL string `json:"devOpsPlatformUrl,omitempty"`
}

// CreateBindingRequest is the body of a bind call.
//
// The organization field takes only the internal identifier. GetOrganization
// reports this identifier. The field does not accept the key or the UUID.
type CreateBindingRequest struct {
	OrganizationID        string `json:"organizationId"`
	DevOpsPlatform        string `json:"devOpsPlatform"`
	InstallationID        string `json:"installationId"`
	RepoAutoImportEnabled *bool  `json:"repoAutoImportEnabled,omitempty"`
}

// PatchBindingRequest is the body of a change call.
//
// This is the only field that can change after a binding exists. The
// installation cannot change, because the server allows that for a binding
// to GitHub Enterprise only.
//
// A field that holds nothing stays out of the request. The action reads an
// absent field as "keep the current value", not as "clear the field".
type PatchBindingRequest struct {
	RepoAutoImportEnabled *bool `json:"repoAutoImportEnabled,omitempty"`
}

// CreateOrganizationBinding binds an organization to a DevOps platform.
//
// A 404 from this call usually reports an installation that the instance
// does not know. It does not report a path that does not exist.
//
// For a binding to github.com the server looks the installation up in its
// own records. It never asks GitHub. An installation of a different GitHub
// application is therefore unknown to the server, although the identifier
// looks correct.
func (c *Client) CreateOrganizationBinding(ctx context.Context, req CreateBindingRequest) (*OrganizationBinding, error) {
	var out OrganizationBinding
	if err := c.apiPost(ctx, bindingsPath, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetOrganizationBinding reads one binding by the identifier of the binding
// itself.
func (c *Client) GetOrganizationBinding(ctx context.Context, id string) (*OrganizationBinding, error) {
	var out OrganizationBinding
	if err := c.apiGet(ctx, bindingsPath+"/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FindOrganizationBinding returns the binding of one organization, which is
// named by its internal identifier.
//
// This is the only way to reach a binding from an organization, so an import
// and the data source both start here.
//
// An organization that is not bound gives ErrNotFound. Every missing entity
// in this package gives the same error.
//
// The server reports an organization that is not bound with a 404 and an
// empty body. newAPIError makes ErrNotFound from that status.
//
// The test for an empty list is a second safeguard. The endpoint answers
// with a collection and a page. If it reports nothing found with a 200, the
// empty collection must not become a binding that holds no fields.
func (c *Client) FindOrganizationBinding(ctx context.Context, organizationID string) (*OrganizationBinding, error) {
	params := url.Values{}
	params.Set("organizationId", organizationID)

	var out struct {
		OrganizationBindings []OrganizationBinding `json:"organizationBindings"`
	}
	if err := c.apiGet(ctx, bindingsPath, params, &out); err != nil {
		return nil, err
	}
	if len(out.OrganizationBindings) == 0 {
		return nil, ErrNotFound
	}
	if len(out.OrganizationBindings) > 1 {
		return nil, fmt.Errorf("organization %q has %d bindings, want 1", organizationID, len(out.OrganizationBindings))
	}
	return &out.OrganizationBindings[0], nil
}

// UpdateOrganizationBinding writes the fields of a binding that can change.
func (c *Client) UpdateOrganizationBinding(ctx context.Context, id string, req PatchBindingRequest) (*OrganizationBinding, error) {
	var out OrganizationBinding
	if err := c.apiPatch(ctx, bindingsPath+"/"+url.PathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateGitLabSelfManagedBinding binds an organization to a group on a GitLab
// Self-Managed instance. It uses the same endpoint as
// CreateOrganizationBinding, with other fields.
func (c *Client) CreateGitLabSelfManagedBinding(ctx context.Context, req CreateGitLabSelfManagedBindingRequest) (*OrganizationBinding, error) {
	var out OrganizationBinding
	if err := c.apiPost(ctx, bindingsPath, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateGitLabSelfManagedBinding changes the token or the instance of a
// binding to GitLab Self-Managed. The endpoint takes a JSON merge patch.
func (c *Client) UpdateGitLabSelfManagedBinding(ctx context.Context, id string, req PatchGitLabSelfManagedBindingRequest) (*OrganizationBinding, error) {
	var out OrganizationBinding
	if err := c.apiMergePatch(ctx, bindingsPath+"/"+url.PathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// A delete is deliberately absent. The API has no such operation. The server
// removes a binding only when it deletes the organization, because that also
// removes the rows that this API writes. See the documentation of the
// resource.
