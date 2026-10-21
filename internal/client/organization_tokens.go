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
	var tokens []OrganizationToken
	for page := 1; ; page++ {
		params := url.Values{"organizationId": {organizationID}, "pageIndex": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}
		var result struct {
			Tokens []OrganizationToken `json:"tokenDefinitions"`
			Page   *struct {
				Total *int `json:"total"`
			} `json:"page"`
		}
		if err := c.apiGet(ctx, organizationTokensPath, params, &result); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, &ScopeNotFoundError{Kind: ScopeOrganization, Key: organizationID, Cause: err}
			}
			return nil, err
		}
		if result.Page == nil {
			return nil, fmt.Errorf("the token list has no valid page total")
		}
		if err := validateOrganizationTokenList(result.Tokens, result.Page.Total, organizationID); err != nil {
			return nil, err
		}
		tokens = append(tokens, result.Tokens...)
		if len(tokens) >= *result.Page.Total {
			return tokens, nil
		}
		if len(result.Tokens) == 0 {
			return nil, fmt.Errorf("token page %d was empty before the reported total", page)
		}
	}
}

func validateOrganizationTokenList(tokens []OrganizationToken, total *int, organizationID string) error {
	if tokens == nil || total == nil || *total < 0 {
		return fmt.Errorf("the token list has no valid collection or page total")
	}
	for _, token := range tokens {
		if token.ID == "" || token.OrganizationID != organizationID {
			return fmt.Errorf("the token list contains a missing ID or a different organization")
		}
	}
	return nil
}

func (c *Client) FindOrganizationToken(ctx context.Context, organizationID, id string) (*OrganizationToken, error) {
	tokens, err := c.ListOrganizationTokens(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	return findByKey(tokens, id, func(token OrganizationToken) string { return token.ID })
}

// DeleteOrganizationToken verifies deletion because an unauthorized delete can return 204.
func (c *Client) DeleteOrganizationToken(ctx context.Context, organizationID, id string) error {
	if _, err := c.FindOrganizationToken(ctx, organizationID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if err := c.apiSend(ctx, http.MethodDelete, organizationTokensPath+"/"+url.PathEscape(id), nil, nil, nil); err != nil {
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
