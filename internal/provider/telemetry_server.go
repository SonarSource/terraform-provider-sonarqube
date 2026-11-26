package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// NewProtocol6 returns the factory of the provider server that main and the
// acceptance tests serve. Its requests name the resource or data source type
// and the Terraform operation in the optional telemetry.
func NewProtocol6(version string) func() tfprotov6.ProviderServer {
	newServer := providerserver.NewProtocol6(New(version)())
	return func() tfprotov6.ProviderServer {
		return telemetryServer{ProviderServer: newServer()}
	}
}

// telemetryServer marks the context of each call that runs an operation, so
// that the client can name it. Other calls, such as the plan of a change, send
// no operation. The embedded interface hides the optional list, action and
// state store interfaces of the framework server.
// TestProviderNeedsNoHiddenServerInterface fails when the provider starts to
// use them.
type telemetryServer struct {
	tfprotov6.ProviderServer
}

func (s telemetryServer) ReadResource(ctx context.Context, req *tfprotov6.ReadResourceRequest) (*tfprotov6.ReadResourceResponse, error) {
	return s.ProviderServer.ReadResource(client.WithOperation(ctx, req.TypeName, client.OperationRead), req)
}

func (s telemetryServer) ReadDataSource(ctx context.Context, req *tfprotov6.ReadDataSourceRequest) (*tfprotov6.ReadDataSourceResponse, error) {
	return s.ProviderServer.ReadDataSource(client.WithOperation(ctx, req.TypeName, client.OperationRead), req)
}

func (s telemetryServer) ImportResourceState(ctx context.Context, req *tfprotov6.ImportResourceStateRequest) (*tfprotov6.ImportResourceStateResponse, error) {
	return s.ProviderServer.ImportResourceState(client.WithOperation(ctx, req.TypeName, client.OperationImport), req)
}

func (s telemetryServer) ApplyResourceChange(ctx context.Context, req *tfprotov6.ApplyResourceChangeRequest) (*tfprotov6.ApplyResourceChangeResponse, error) {
	op := client.OperationUpdate
	switch {
	case isNull(req.PriorState):
		op = client.OperationCreate
	case isNull(req.PlannedState):
		op = client.OperationDelete
	}
	return s.ProviderServer.ApplyResourceChange(client.WithOperation(ctx, req.TypeName, op), req)
}

// isNull reports if Terraform sent a null state: no state before a create, or
// after a delete. A value that cannot be read counts as not null.
func isNull(value *tfprotov6.DynamicValue) bool {
	if value == nil {
		return true
	}
	null, err := value.IsNull()
	return err == nil && null
}
