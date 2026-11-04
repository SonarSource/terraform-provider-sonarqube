package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// ErrEditorNotFound reports that the group or the user of an editor call does
// not exist, or that the user is not a member of the organization any more.
var ErrEditorNotFound = errors.New("the group or the user was not found in the organization")

// editorCalls are the web services and parameters of one kind of editor.
type editorCalls struct {
	add, remove, search, param string
}

func editorCallsOf(editor PermissionPrincipal) editorCalls {
	if editor.Group {
		return editorCalls{add: "/add_group", remove: "/remove_group", search: "/search_groups", param: "group"}
	}
	return editorCalls{add: "/add_user", remove: "/remove_user", search: "/search_users", param: "login"}
}

// editorParameters names the profile as the editor web services need it: by
// its name and language, not by its key. Thus the caller reads the profile
// from its key before each call, and a rename does not break the editor.
func editorParameters(organization string, profile QualityProfile) url.Values {
	params := profileParameters(organization)
	params.Set("qualityProfile", profile.Name)
	params.Set("language", profile.Language)
	return params
}

// AddQualityProfileEditor lets the editor edit the profile. An editor that
// can already edit it is success.
func (c *Client) AddQualityProfileEditor(ctx context.Context, organization string, profile QualityProfile, editor PermissionPrincipal) error {
	calls := editorCallsOf(editor)
	params := editorParameters(organization, profile)
	params.Set(calls.param, editor.Name)
	return qualityProfileEditorError(c.post(ctx, qualityProfilesPath+calls.add, params), organization)
}

// RemoveQualityProfileEditor takes the right to edit the profile from the
// editor. An editor without the right is success, and so is a missing group or
// user, or a user who is not a member of the organization any more. A missing
// profile stays an error: the name can be out of date after a rename, and
// then the right would stay.
func (c *Client) RemoveQualityProfileEditor(ctx context.Context, organization string, profile QualityProfile, editor PermissionPrincipal) error {
	calls := editorCallsOf(editor)
	params := editorParameters(organization, profile)
	params.Set(calls.param, editor.Name)
	err := qualityProfileEditorError(c.post(ctx, qualityProfilesPath+calls.remove, params), organization)
	if errors.Is(err, ErrEditorNotFound) {
		return nil
	}
	return err
}

// HasQualityProfileEditor reports if the editor can edit the profile. The q
// filter keeps the groups whose name holds the value, and the users whose
// login or name holds it, without regard to case. Thus the match happens
// here. A login matches exactly. A group name matches without regard to case,
// because SonarQube Cloud finds the group without regard to case in add_group
// too. SonarQube Server is not checked yet. When the plan of the
// organization does not permit editors, the server gives an empty list, and
// the editor is reported as missing.
func (c *Client) HasQualityProfileEditor(ctx context.Context, organization string, profile QualityProfile, editor PermissionPrincipal) (bool, error) {
	const pageSize = 500
	calls := editorCallsOf(editor)
	params := editorParameters(organization, profile)
	params.Set("selected", "selected")
	params.Set("q", editor.Name)
	params.Set("ps", strconv.Itoa(pageSize))
	editors, err := collectPages("quality profile editor", func(page int) ([]string, int, error) {
		params.Set("p", strconv.Itoa(page))
		var answer struct {
			Groups []struct {
				Name string `json:"name"`
			} `json:"groups"`
			Users []struct {
				Login string `json:"login"`
			} `json:"users"`
			Paging struct {
				Total int `json:"total"`
			} `json:"paging"`
		}
		if err := c.get(ctx, qualityProfilesPath+calls.search, params, &answer); err != nil {
			return nil, 0, qualityProfileEditorError(err, organization)
		}
		names := make([]string, 0, len(answer.Groups)+len(answer.Users))
		for _, group := range answer.Groups {
			names = append(names, group.Name)
		}
		for _, user := range answer.Users {
			names = append(names, user.Login)
		}
		return names, answer.Paging.Total, nil
	})
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(editors, func(name string) bool {
		return name == editor.Name || (editor.Group && strings.EqualFold(name, editor.Name))
	}), nil
}

// qualityProfileEditorError explains the refusals of the editor web services.
// A missing organization becomes a ScopeNotFoundError. A missing group or
// user, and a user who is not a member, match ErrEditorNotFound. A missing
// profile stays a 404 that matches ErrNotFound.
func qualityProfileEditorError(err error, organization string) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case isMissingOrganization(err):
		return &ScopeNotFoundError{Kind: ScopeOrganization, Key: organization, Cause: err}
	case apiErr.StatusCode == http.StatusNotFound &&
		(hasMessagePrefix(apiErr.Messages, "No group with name ") || hasMessagePrefix(apiErr.Messages, "User with login ")):
		return fmt.Errorf("%w: %s", ErrEditorNotFound, apiErr.Error())
	case apiErr.StatusCode == http.StatusBadRequest && hasMessagePrefix(apiErr.Messages, "Operation forbidden for built-in Quality Profile"):
		return fmt.Errorf("a built-in quality profile cannot have editors. Use a custom profile: %w", err)
	case apiErr.StatusCode == http.StatusBadRequest && hasMessageContaining(apiErr.Messages, "is not member of organization"):
		return fmt.Errorf("the user is not a member of the organization %q. Add the user to the organization "+
			"first (%w): %s", organization, ErrEditorNotFound, apiErr.Error())
	case apiErr.StatusCode == http.StatusForbidden && hasMessageContaining(apiErr.Messages, "subscription plan"):
		return fmt.Errorf("the plan of the organization does not permit quality profile editors. On SonarQube "+
			"Cloud, move the organization to the Team or the Enterprise plan: %w", err)
	case apiErr.StatusCode == http.StatusForbidden:
		return fmt.Errorf("the server refused the call. Check that the token has the Administer Quality "+
			"Profiles permission, or the permission to edit the profile: %w", err)
	}
	return err
}
