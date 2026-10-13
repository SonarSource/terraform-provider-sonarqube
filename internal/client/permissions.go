package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// PermissionScope identifies the object whose permissions are managed.
// ProjectKey is empty for organization permissions.
type PermissionScope struct {
	Organization string
	ProjectKey   string
}

func (s PermissionScope) parameters() url.Values {
	values := url.Values{"organization": {s.Organization}}
	if s.ProjectKey != "" {
		values.Set("projectKey", s.ProjectKey)
	}
	return values
}

// PermissionPrincipal is the group or the user that holds permissions. Name is
// the group name or the user login.
type PermissionPrincipal struct {
	Group bool
	Name  string
}

func (p PermissionPrincipal) kind() string {
	if p.Group {
		return "group"
	}
	return "user"
}

func (p PermissionPrincipal) nameParameter() string {
	if p.Group {
		return "groupName"
	}
	return "login"
}

// The read web services refuse a q shorter than this.
const permissionQueryMinLength = 3

// CurrentUserLogin returns the login associated with this client's token.
func (c *Client) CurrentUserLogin(ctx context.Context) (string, error) {
	var current struct {
		Login string `json:"login"`
	}
	if err := c.get(ctx, "/api/users/current", nil, &current); err != nil {
		return "", err
	}
	if current.Login == "" {
		return "", fmt.Errorf("the current user answer has no login")
	}
	return current.Login, nil
}

type permissionEntry struct {
	Name        string   `json:"name"`
	Login       string   `json:"login"`
	Permissions []string `json:"permissions"`
}

// GetPermissions returns the permissions of the principal in the scope. The
// list holds the principals without permissions too, so ErrNotFound means that
// the group, or the user as an organization member, does not exist.
//
// The q filter keeps the names and logins that hold the value, and also user
// display names, so the result is matched exactly here.
func (c *Client) GetPermissions(ctx context.Context, scope PermissionScope, principal PermissionPrincipal) ([]string, error) {
	params := scope.parameters()
	if utf8.RuneCountInString(principal.Name) >= permissionQueryMinLength {
		params.Set("q", principal.Name)
	}
	entries, err := c.listPermissions(ctx, "/api/permissions/"+principal.kind()+"s", params)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("%w: %q: %s", ErrOrganizationNotFound, scope.Organization, err)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the permissions of %s %q: %w", principal.kind(), principal.Name, err)
	}
	entry, err := findByKey(entries, principal.Name, func(entry permissionEntry) string {
		if principal.Group {
			return entry.Name
		}
		return entry.Login
	})
	if err != nil {
		return nil, err
	}
	return entry.Permissions, nil
}

func (c *Client) listPermissions(ctx context.Context, path string, params url.Values) ([]permissionEntry, error) {
	const pageSize = 100
	var entries []permissionEntry
	for page := 1; ; page++ {
		params.Set("p", strconv.Itoa(page))
		params.Set("ps", strconv.Itoa(pageSize))
		var answer struct {
			Groups []permissionEntry `json:"groups"`
			Users  []permissionEntry `json:"users"`
			Paging struct {
				Total int `json:"total"`
			} `json:"paging"`
		}
		if err := c.get(ctx, path, params, &answer); err != nil {
			return nil, err
		}
		items := append(answer.Groups, answer.Users...)
		entries = append(entries, items...)
		if len(entries) >= answer.Paging.Total {
			return entries, nil
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("permission page %d was empty before the reported total", page)
		}
	}
}

// AddPermission grants one permission to the principal.
func (c *Client) AddPermission(ctx context.Context, scope PermissionScope, principal PermissionPrincipal, permission string) error {
	return c.changePermission(ctx, "add", scope, principal, permission)
}

// RemovePermission revokes one permission from the principal. The server
// refuses to remove the last administrator.
func (c *Client) RemovePermission(ctx context.Context, scope PermissionScope, principal PermissionPrincipal, permission string) error {
	return c.changePermission(ctx, "remove", scope, principal, permission)
}

func (c *Client) changePermission(ctx context.Context, action string, scope PermissionScope, principal PermissionPrincipal, permission string) error {
	params := scope.parameters()
	params.Set(principal.nameParameter(), principal.Name)
	params.Set("permission", permission)
	err := c.post(ctx, "/api/permissions/"+action+"_"+principal.kind(), params)
	var apiErr *APIError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return fmt.Errorf("%s %q was not found: %w", principal.kind(), principal.Name, err)
	case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && isLastAdminMessage(apiErr.Messages):
		return fmt.Errorf("cannot remove the last administrator. Give admin to another user first: %w", err)
	default:
		return err
	}
}

// isLastAdminMessage matches "Last user with permission 'admin'. ..." and
// "Last group with permission 'admin'. ...".
func isLastAdminMessage(messages []string) bool {
	for _, message := range messages {
		if strings.HasPrefix(message, "Last ") && strings.Contains(message, "with permission 'admin'") {
			return true
		}
	}
	return false
}
