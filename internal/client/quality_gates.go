package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// QualityGate is a SonarQube Cloud quality gate.
type QualityGate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// LegacyID is the numeric identifier that the older web services take.
	// json.Number accepts it as a number and as a string.
	LegacyID    json.Number `json:"legacyId"`
	AIQualified bool        `json:"aiQualified"`
	BuiltIn     bool        `json:"builtIn"`
}

// QualityGateCondition is one condition of a Cloud quality gate.
type QualityGateCondition struct {
	ID             string `json:"id"`
	QualityGateID  string `json:"qualityGateId"`
	LegacyMetricID int    `json:"legacyMetricId"`
	Operator       string `json:"operator"`
	Threshold      string `json:"threshold"`
}

// QualityGateConditionRequest contains the fields accepted for a condition write.
type QualityGateConditionRequest struct {
	QualityGateID  string `json:"qualityGateId,omitempty"`
	LegacyMetricID int    `json:"legacyMetricId,omitempty"`
	Operator       string `json:"operator"`
	Threshold      string `json:"threshold"`
}

const qualityGatePath = "/quality-gates/quality-gates/"

// ListQualityGates reads every page of the gates in an organization.
func (c *Client) ListQualityGates(ctx context.Context, organizationID string) ([]QualityGate, error) {
	const pageSize = 100
	var gates []QualityGate
	for page := 1; ; page++ {
		params := url.Values{
			"organizationId": {organizationID},
			"pageIndex":      {strconv.Itoa(page)},
			"pageSize":       {strconv.Itoa(pageSize)},
		}
		var result struct {
			QualityGates []QualityGate `json:"qualityGates"`
			Page         struct {
				Total int `json:"total"`
			} `json:"page"`
		}
		if err := c.apiGet(ctx, "/quality-gates/quality-gates", params, &result); err != nil {
			return nil, err
		}
		gates = append(gates, result.QualityGates...)
		if len(gates) >= result.Page.Total {
			return gates, nil
		}
		if len(result.QualityGates) == 0 {
			return nil, fmt.Errorf("quality gate page %d was empty before the reported total", page)
		}
	}
}

// GetQualityGate reads a gate by its UUID.
func (c *Client) GetQualityGate(ctx context.Context, id string) (*QualityGate, error) {
	var gate QualityGate
	if err := c.apiGet(ctx, qualityGatePath+url.PathEscape(id), nil, &gate); err != nil {
		return nil, err
	}
	return &gate, nil
}

// FindQualityGate finds a gate by exact name in one organization.
func (c *Client) FindQualityGate(ctx context.Context, organizationID, name string) (*QualityGate, error) {
	gates, err := c.ListQualityGates(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	return findByKey(gates, name, func(gate QualityGate) string { return gate.Name })
}

// CreateQualityGate creates a gate without conditions.
func (c *Client) CreateQualityGate(ctx context.Context, organizationID, name string) (*QualityGate, error) {
	var gate QualityGate
	if err := c.apiPost(ctx, "/quality-gates/quality-gates", map[string]string{
		"organizationId": organizationID, "name": name,
	}, &gate); err != nil {
		return nil, err
	}
	return &gate, nil
}

// UpdateQualityGate sends only changed fields to the API.
func (c *Client) UpdateQualityGate(ctx context.Context, id string, changes map[string]any) error {
	return c.apiPatch(ctx, qualityGatePath+url.PathEscape(id), changes, nil)
}

// DeleteQualityGate deletes a gate. A missing gate is already deleted.
func (c *Client) DeleteQualityGate(ctx context.Context, id string) error {
	err := c.apiSend(ctx, http.MethodDelete, qualityGatePath+url.PathEscape(id), nil, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// SetDefaultQualityGate makes a gate the default of an organization. Web API
// v2 has no operation for this, and no API removes a default: only a different
// gate can take its place.
//
// The older web service names a gate by its numeric legacy identifier only,
// and a read of the gate is the only place that reports it. A gate that does
// not exist gives ErrNotFound.
func (c *Client) SetDefaultQualityGate(ctx context.Context, organization, gateID string) error {
	gate, err := c.GetQualityGate(ctx, gateID)
	if err != nil {
		return err
	}
	if gate.LegacyID == "" {
		return fmt.Errorf("the API gave no legacy identifier for the quality gate %s", gateID)
	}

	params := url.Values{}
	params.Set("organization", organization)
	params.Set("id", gate.LegacyID.String())

	return c.post(ctx, "/api/qualitygates/set_as_default", params)
}

// ListQualityGateConditions reads all conditions of a gate.
func (c *Client) ListQualityGateConditions(ctx context.Context, gateID string) ([]QualityGateCondition, error) {
	params := url.Values{"qualityGateId": {gateID}}
	var result struct {
		Conditions []QualityGateCondition `json:"conditions"`
	}
	if err := c.apiGet(ctx, "/quality-gates/conditions", params, &result); err != nil {
		return nil, err
	}
	return result.Conditions, nil
}

// CreateQualityGateCondition adds one condition.
func (c *Client) CreateQualityGateCondition(ctx context.Context, req QualityGateConditionRequest) error {
	return c.apiPost(ctx, "/quality-gates/conditions", req, nil)
}

// UpdateQualityGateCondition changes one condition in place.
func (c *Client) UpdateQualityGateCondition(ctx context.Context, id string, req QualityGateConditionRequest) error {
	return c.apiPatch(ctx, "/quality-gates/conditions/"+url.PathEscape(id), req, nil)
}

// DeleteQualityGateCondition removes one condition.
func (c *Client) DeleteQualityGateCondition(ctx context.Context, id string) error {
	err := c.apiSend(ctx, http.MethodDelete, "/quality-gates/conditions/"+url.PathEscape(id), nil, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

type metric struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

// ListMetrics maps legacy metric identifiers to keys for reads and writes.
func (c *Client) ListMetrics(ctx context.Context) (map[int]string, error) {
	const pageSize = 500
	byID := map[int]string{}
	for page := 1; ; page++ {
		params := url.Values{"p": {strconv.Itoa(page)}, "ps": {strconv.Itoa(pageSize)}}
		var result struct {
			Metrics []metric `json:"metrics"`
			Total   int      `json:"total"`
		}
		if err := c.get(ctx, "/api/metrics/search", params, &result); err != nil {
			return nil, err
		}
		for _, item := range result.Metrics {
			id, err := strconv.Atoi(item.ID)
			if err != nil {
				return nil, fmt.Errorf("metric %q has invalid identifier: %w", item.Key, err)
			}
			byID[id] = item.Key
		}
		if len(byID) >= result.Total {
			return byID, nil
		}
		if len(result.Metrics) == 0 {
			return nil, fmt.Errorf("metric page %d was empty before the reported total", page)
		}
	}
}
