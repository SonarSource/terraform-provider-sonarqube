package client

import (
	"context"
	"strings"
)

// Operation is a Terraform operation that the optional telemetry can name.
type Operation string

const (
	OperationCreate Operation = "create"
	OperationRead   Operation = "read"
	OperationUpdate Operation = "update"
	OperationDelete Operation = "delete"
	OperationImport Operation = "import"
)

var telemetryOperations = map[Operation]bool{
	OperationCreate: true,
	OperationRead:   true,
	OperationUpdate: true,
	OperationDelete: true,
	OperationImport: true,
}

// Telemetry reports if the client can send optional telemetry.
func (c *Client) Telemetry() bool {
	return c.telemetry
}

type operationKey struct{}

type operation struct {
	typeName string
	name     Operation
}

// WithOperation marks the requests that use ctx as part of an operation on the
// resource or data source typeName.
func WithOperation(ctx context.Context, typeName string, op Operation) context.Context {
	return context.WithValue(ctx, operationKey{}, operation{typeName: typeName, name: op})
}

// userAgentFor puts the operation of ctx in a comment after the product token,
// for example "sonarqube-terraform/1.2.3 (sonarqube_project; create)". The
// appended text comes last, so that the comment stays with the product token.
func (c *Client) userAgentFor(ctx context.Context) string {
	parts := []string{c.userAgent}
	if op, ok := ctx.Value(operationKey{}).(operation); ok && c.telemetryAllows(op.typeName, op.name) {
		parts = append(parts, "("+op.typeName+"; "+string(op.name)+")")
	}
	if c.appendedUserAgent != "" {
		parts = append(parts, c.appendedUserAgent)
	}
	return strings.Join(parts, " ")
}

// telemetryAllows reports if the optional telemetry can name the type and the
// operation. It refuses each value that is not on the allowlist, so that a
// user value, such as a key or an ID, never goes into the telemetry.
func (c *Client) telemetryAllows(typeName string, operation Operation) bool {
	return c.telemetry && c.telemetryTypes[typeName] && telemetryOperations[operation]
}

func telemetryTypeSet(typeNames []string) map[string]bool {
	set := make(map[string]bool, len(typeNames))
	for _, name := range typeNames {
		set[name] = true
	}
	return set
}
