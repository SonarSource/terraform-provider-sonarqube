package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

const enterpriseOrganizationsPath = "/enterprises/enterprise-organizations"

// EnterpriseOrganization is the relation between an enterprise and one of its
// organizations. OrganizationID is the legacy ID of the organization.
type EnterpriseOrganization struct {
	ID                 string `json:"enterpriseOrganizationId"`
	EnterpriseID       string `json:"enterpriseId"`
	OrganizationID     string `json:"organizationId"`
	OrganizationUUIDV4 string `json:"organizationUuidV4"`
}

// Matches reports whether the relation is for the organization.
func (r EnterpriseOrganization) Matches(org *Organization) bool {
	return r.OrganizationID == org.ID || (org.UUIDV4 != "" && r.OrganizationUUIDV4 == org.UUIDV4)
}

type enterpriseOrganizationRequest struct {
	EnterpriseID   string `json:"enterpriseId"`
	OrganizationID string `json:"organizationId"`
}

// ListEnterpriseOrganizationsOfEnterprise returns the relations of an
// enterprise. The list has no pages.
//
// An enterprise that the token cannot read gives an empty list, not an error.
// The same is true for a scoped organization token.
func (c *Client) ListEnterpriseOrganizationsOfEnterprise(ctx context.Context, enterpriseID string) ([]EnterpriseOrganization, error) {
	return c.listEnterpriseOrganizations(ctx, url.Values{"enterpriseId": {enterpriseID}})
}

// ListEnterpriseOrganizationsOfOrganization returns the relations of an
// organization: one item when it is in an enterprise, and none in other cases.
// The filter accepts the legacy ID and the UUID. An unknown organization gives
// an empty list.
func (c *Client) ListEnterpriseOrganizationsOfOrganization(ctx context.Context, organizationID string) ([]EnterpriseOrganization, error) {
	return c.listEnterpriseOrganizations(ctx, url.Values{"organizationId": {organizationID}})
}

func (c *Client) listEnterpriseOrganizations(ctx context.Context, params url.Values) ([]EnterpriseOrganization, error) {
	var result []EnterpriseOrganization
	if err := c.apiGet(ctx, enterpriseOrganizationsPath, params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// AttachEnterpriseOrganization adds an organization to an enterprise. The
// organization ID can be the legacy ID or the UUID. An organization on the
// free plan moves to a paid plan.
func (c *Client) AttachEnterpriseOrganization(ctx context.Context, enterpriseID, organizationID string) (*EnterpriseOrganization, error) {
	var result EnterpriseOrganization
	body := enterpriseOrganizationRequest{EnterpriseID: enterpriseID, OrganizationID: organizationID}
	if err := c.apiPost(ctx, enterpriseOrganizationsPath, body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DetachEnterpriseOrganization removes an organization from its enterprise.
// The organization stays on a paid plan without enterprise features. When it
// is the last organization, the server also deletes the enterprise. A 404,
// for a relation that is already gone, is not an error.
func (c *Client) DetachEnterpriseOrganization(ctx context.Context, relationID string) error {
	err := c.apiSend(ctx, http.MethodDelete, enterpriseOrganizationsPath+"/"+url.PathEscape(relationID), nil, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// IsAlreadyInEnterprise reports the 400 that an attach gives for an
// organization that is in an enterprise.
func IsAlreadyInEnterprise(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest &&
		hasMessageContaining(apiErr.Messages, "already part of an enterprise")
}
