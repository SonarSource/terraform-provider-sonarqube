package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

const enterprisesPath = "/enterprises/enterprises"

// Enterprise is a SonarQube Cloud enterprise. The API leaves out avatar when
// the enterprise never had one.
type Enterprise struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

// EnterpriseRequest is the body of a create. The gateway refuses a body
// without avatar, license or organizationId, so each is always sent. Avatar
// must be a string: "" is accepted, null is not.
type EnterpriseRequest struct {
	Key            string `json:"key"`
	Name           string `json:"name"`
	Avatar         string `json:"avatar"`
	License        string `json:"license"`
	OrganizationID string `json:"organizationId"`
}

// EnterpriseUpdate is the body of a merge patch. The API also accepts key,
// but the docs do not list it, so the provider does not send it.
type EnterpriseUpdate struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

// CreateEnterprise creates an enterprise. A successful create uses up the
// license, also when the enterprise is deleted later.
func (c *Client) CreateEnterprise(ctx context.Context, request EnterpriseRequest) (*Enterprise, error) {
	var result Enterprise
	if err := c.apiPost(ctx, enterprisesPath, request, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetEnterprise reads an enterprise by its UUID. A 404 matches ErrNotFound.
func (c *Client) GetEnterprise(ctx context.Context, id string) (*Enterprise, error) {
	var result Enterprise
	if err := c.apiGet(ctx, enterprisesPath+"/"+url.PathEscape(id), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetEnterpriseByKey reads an enterprise by its key. The API answers an
// unknown key with 404, which matches ErrNotFound, and a known key with a list
// of one item.
func (c *Client) GetEnterpriseByKey(ctx context.Context, key string) (*Enterprise, error) {
	var result []Enterprise
	if err := c.apiGet(ctx, enterprisesPath, url.Values{"enterpriseKey": {key}}, &result); err != nil {
		return nil, err
	}
	return findByKey(result, key, func(e Enterprise) string { return e.Key })
}

// ListEnterprises returns the enterprises that the token user can see. A
// scoped organization token gets an empty list.
func (c *Client) ListEnterprises(ctx context.Context) ([]Enterprise, error) {
	var result []Enterprise
	if err := c.apiGet(ctx, enterprisesPath, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// UpdateEnterprise changes the name and the avatar of an enterprise.
func (c *Client) UpdateEnterprise(ctx context.Context, id string, update EnterpriseUpdate) (*Enterprise, error) {
	var result Enterprise
	if err := c.apiMergePatch(ctx, enterprisesPath+"/"+url.PathEscape(id), update, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteEnterprise deletes an enterprise and its portfolios. The organizations
// stay, on a paid plan without enterprise features. The API answers 204 also
// for an enterprise that is already gone, so the client reads the enterprise
// after the delete to confirm it.
func (c *Client) DeleteEnterprise(ctx context.Context, id string) error {
	err := c.apiSend(ctx, http.MethodDelete, enterprisesPath+"/"+url.PathEscape(id), nil, nil, nil)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err = c.GetEnterprise(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot confirm the enterprise deletion: %w", err)
	}
	return fmt.Errorf("the enterprise still exists after the delete; check that the token user is an enterprise administrator")
}

// IsEnterpriseKeyRefused reports the 400 that a create gives for a key that
// another enterprise uses. The message does not tell the cause.
func IsEnterpriseKeyRefused(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest &&
		hasMessageContaining(apiErr.Messages, "Enterprise key is not valid")
}
