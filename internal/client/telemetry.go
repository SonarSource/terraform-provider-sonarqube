package client

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

// telemetryAllows reports if the optional telemetry can name the type and the
// operation. It refuses each value that is not on the allowlist, so that a
// user value, such as a key or an ID, never goes into the telemetry. Check it
// where the request gets the telemetry, not in each resource.
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
