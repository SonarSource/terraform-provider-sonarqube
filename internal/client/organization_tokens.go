package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const organizationTokensPath = "/authentication/token-definitions"

// The resource types of token role assignments and token resource links.
const (
	TokenResourceOrganization = "organization"
	TokenResourceProject      = "project"
)

type TokenRoleAssignment struct {
	ResourceType string   `json:"resourceType"`
	Roles        []string `json:"roles"`
}

type OrganizationToken struct {
	ID                          string                `json:"id"`
	OrganizationID              string                `json:"organizationId"`
	Name                        string                `json:"name"`
	Description                 *string               `json:"description,omitempty"`
	ExpiresAt                   *string               `json:"expiresAt,omitempty"`
	CreatedAt                   string                `json:"createdAt"`
	RoleAssignments             []TokenRoleAssignment `json:"roleAssignments"`
	AutoGrantProjectPermissions bool                  `json:"autoGrantProjectPermissions"`
}

type OrganizationTokenRequest struct {
	OrganizationID              string                `json:"organizationId"`
	Name                        string                `json:"name"`
	Description                 *string               `json:"description,omitempty"`
	ExpiresAt                   *string               `json:"expiresAt,omitempty"`
	RoleAssignments             []TokenRoleAssignment `json:"roleAssignments"`
	AutoGrantProjectPermissions bool                  `json:"autoGrantProjectPermissions"`
}

// CreatedOrganizationToken is the only response that contains the secret.
type CreatedOrganizationToken struct {
	OrganizationToken
	Token string `json:"token"`
}

func (c *Client) CreateOrganizationToken(ctx context.Context, request OrganizationTokenRequest) (*CreatedOrganizationToken, error) {
	var result CreatedOrganizationToken
	if err := c.apiPost(ctx, organizationTokensPath, request, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ListOrganizationTokens(ctx context.Context, organizationID string) ([]OrganizationToken, error) {
	const pageSize = 100
	return collectPages("token", func(page int) ([]OrganizationToken, int, error) {
		params := url.Values{"organizationId": {organizationID}, "pageIndex": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}
		var result struct {
			Tokens []OrganizationToken `json:"tokenDefinitions"`
			Page   *struct {
				Total *int `json:"total"`
			} `json:"page"`
		}
		if err := c.apiGet(ctx, organizationTokensPath, params, &result); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, 0, &ScopeNotFoundError{Kind: ScopeOrganization, Key: organizationID, Cause: err}
			}
			return nil, 0, err
		}
		if result.Tokens == nil || result.Page == nil || result.Page.Total == nil || *result.Page.Total < 0 {
			return nil, 0, fmt.Errorf("the token list has no valid collection or page total")
		}
		for _, token := range result.Tokens {
			if token.ID == "" || token.OrganizationID != organizationID {
				return nil, 0, fmt.Errorf("the token list contains a missing ID or a different organization")
			}
		}
		return result.Tokens, *result.Page.Total, nil
	})
}

func (c *Client) FindOrganizationToken(ctx context.Context, organizationID, id string) (*OrganizationToken, error) {
	tokens, err := c.ListOrganizationTokens(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	return findByKey(tokens, id, func(token OrganizationToken) string { return token.ID })
}

// DeleteOrganizationToken verifies deletion because the delete returns 204
// both for an unknown ID and for a caller that cannot delete the token.
func (c *Client) DeleteOrganizationToken(ctx context.Context, organizationID, id string) error {
	if err := c.apiSend(ctx, http.MethodDelete, organizationTokensPath+"/"+url.PathEscape(id), nil, nil, nil); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err := c.FindOrganizationToken(ctx, organizationID, id)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot confirm token deletion: %w", err)
	}
	return fmt.Errorf("the token still exists after deletion; check organization administration permissions")
}

const tokenResourceLinksPath = "/authentication/token-resource-links"

// TokenResourceLink gives a token its project permissions on one project.
type TokenResourceLink struct {
	ID                  string `json:"id"`
	OrganizationTokenID string `json:"organizationTokenId"`
	ResourceType        string `json:"resourceType"`
	ResourceID          string `json:"resourceId"`
}

// CreateTokenResourceLink returns the existing link when the token already
// has a link to the resource. The server does not check that the token has
// project permissions.
func (c *Client) CreateTokenResourceLink(ctx context.Context, tokenID, resourceType, resourceID string) (*TokenResourceLink, error) {
	request := TokenResourceLink{OrganizationTokenID: tokenID, ResourceType: resourceType, ResourceID: resourceID}
	var result TokenResourceLink
	if err := c.apiPost(ctx, tokenResourceLinksPath, request, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListTokenResourceLinks returns ErrNotFound when the token does not exist.
// The links of a deleted project stay in the list.
func (c *Client) ListTokenResourceLinks(ctx context.Context, tokenID string) ([]TokenResourceLink, error) {
	const pageSize = 500
	return collectPages("token resource link", func(page int) ([]TokenResourceLink, int, error) {
		params := url.Values{"organizationTokenId": {tokenID}, "pageIndex": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}
		var result struct {
			Links []TokenResourceLink `json:"tokenResourceLinks"`
			Page  *struct {
				Total *int `json:"total"`
			} `json:"page"`
		}
		if err := c.apiGet(ctx, tokenResourceLinksPath, params, &result); err != nil {
			return nil, 0, err
		}
		if result.Links == nil || result.Page == nil || result.Page.Total == nil || *result.Page.Total < 0 {
			return nil, 0, fmt.Errorf("the token resource link list has no valid collection or page total")
		}
		for _, link := range result.Links {
			if link.ID == "" || link.ResourceID == "" || link.OrganizationTokenID != tokenID {
				return nil, 0, fmt.Errorf("the token resource link list contains a missing ID or a different token")
			}
		}
		return result.Links, *result.Page.Total, nil
	})
}

// DeleteTokenResourceLink accepts a link that is already gone. The server
// answers 204 for it.
func (c *Client) DeleteTokenResourceLink(ctx context.Context, id string) error {
	err := c.apiSend(ctx, http.MethodDelete, tokenResourceLinksPath+"/"+url.PathEscape(id), nil, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
