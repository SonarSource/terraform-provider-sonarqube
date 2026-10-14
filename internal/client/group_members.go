package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
)

// AddGroupMember adds the user to the group. A user that is already in the
// group is success. The user must be a member of the organization.
func (c *Client) AddGroupMember(ctx context.Context, organization, group, login string) error {
	err := c.post(ctx, "/api/user_groups/add_user", groupMemberParameters(organization, group, login))
	return groupMemberError(err, organization, login)
}

// RemoveGroupMember removes the user from the group. A user that is not in
// the group is success.
func (c *Client) RemoveGroupMember(ctx context.Context, organization, group, login string) error {
	err := c.post(ctx, "/api/user_groups/remove_user", groupMemberParameters(organization, group, login))
	return groupMemberError(err, organization, login)
}

// IsGroupMember reports if the user is in the group. The q filter keeps the
// users whose login or name holds the value, without regard to case, so the
// exact match happens here.
func (c *Client) IsGroupMember(ctx context.Context, organization, group, login string) (bool, error) {
	members, err := c.listGroupMembers(ctx, url.Values{"organization": {organization}, "name": {group}, "q": {login}})
	if err != nil {
		return false, groupMemberError(err, organization, login)
	}
	return slices.ContainsFunc(members, func(member groupMember) bool { return member.Login == login }), nil
}

type groupMember struct {
	Login string `json:"login"`
}

func (c *Client) listGroupMembers(ctx context.Context, params url.Values) ([]groupMember, error) {
	const pageSize = 500
	params.Set("selected", "selected")
	params.Set("ps", strconv.Itoa(pageSize))
	var members []groupMember
	for page := 1; ; page++ {
		params.Set("p", strconv.Itoa(page))
		var answer struct {
			Users []groupMember `json:"users"`
			Total int           `json:"total"`
		}
		if err := c.get(ctx, "/api/user_groups/users", params, &answer); err != nil {
			return nil, err
		}
		members = append(members, answer.Users...)
		if len(members) >= answer.Total {
			return members, nil
		}
		if len(answer.Users) == 0 {
			return nil, fmt.Errorf("group member page %d was empty before the reported total", page)
		}
	}
}

func groupMemberParameters(organization, group, login string) url.Values {
	return url.Values{"organization": {organization}, "name": {group}, "login": {login}}
}

// groupMemberError adds hints to the errors of the membership web services. A
// 404 names the missing organization, group or user. Only a missing
// organization becomes a ScopeNotFoundError, so that a caller can warn about
// it. The other 404 errors still match ErrNotFound.
func groupMemberError(err error, organization, login string) error {
	var apiErr *APIError
	if err == nil || !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case apiErr.StatusCode == http.StatusNotFound && hasMessagePrefix(apiErr.Messages, "No organization with key "):
		return &ScopeNotFoundError{Kind: ScopeOrganization, Key: organization, Cause: err}
	case apiErr.StatusCode == http.StatusBadRequest && hasMessageContaining(apiErr.Messages, "is not member of organization"):
		return fmt.Errorf("user %q is not a member of the organization %q. Add the user to the organization "+
			"first: %w", login, organization, err)
	case apiErr.StatusCode == http.StatusBadRequest && hasMessagePrefix(apiErr.Messages, "The last administrator user cannot be removed"):
		return fmt.Errorf("the organization %q has no other administrator, so SonarQube Cloud refuses to remove "+
			"the user from any group. Give the admin permission directly to a user, or add another user to a "+
			"group that has it: %w", organization, err)
	default:
		return err
	}
}
