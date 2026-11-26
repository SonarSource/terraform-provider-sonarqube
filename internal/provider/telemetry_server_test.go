package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// recordingServer sends one request in each call, so that the test reads the
// User-Agent header that the context of the call gives.
type recordingServer struct {
	tfprotov6.ProviderServer
	client *client.Client
}

func (s recordingServer) send(ctx context.Context) {
	_, _ = s.client.ListEnterprises(ctx)
}

func (s recordingServer) ReadResource(ctx context.Context, _ *tfprotov6.ReadResourceRequest) (*tfprotov6.ReadResourceResponse, error) {
	s.send(ctx)
	return &tfprotov6.ReadResourceResponse{}, nil
}

func (s recordingServer) ReadDataSource(ctx context.Context, _ *tfprotov6.ReadDataSourceRequest) (*tfprotov6.ReadDataSourceResponse, error) {
	s.send(ctx)
	return &tfprotov6.ReadDataSourceResponse{}, nil
}

func (s recordingServer) ImportResourceState(ctx context.Context, _ *tfprotov6.ImportResourceStateRequest) (*tfprotov6.ImportResourceStateResponse, error) {
	s.send(ctx)
	return &tfprotov6.ImportResourceStateResponse{}, nil
}

func (s recordingServer) ApplyResourceChange(ctx context.Context, _ *tfprotov6.ApplyResourceChangeRequest) (*tfprotov6.ApplyResourceChangeResponse, error) {
	s.send(ctx)
	return &tfprotov6.ApplyResourceChangeResponse{}, nil
}

func (s recordingServer) PlanResourceChange(ctx context.Context, _ *tfprotov6.PlanResourceChangeRequest) (*tfprotov6.PlanResourceChangeResponse, error) {
	s.send(ctx)
	return &tfprotov6.PlanResourceChangeResponse{}, nil
}

func TestTelemetryServerNamesTheOperation(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.UserAgent()
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := client.New(client.Config{URL: srv.URL, APIURL: srv.URL, Token: "test-token", HTTPClient: srv.Client(),
		UserAgent: "sonarqube-terraform/1.2.3", Telemetry: true, TelemetryTypes: []string{"sonarqube_project"}})
	s := telemetryServer{ProviderServer: recordingServer{client: c}}
	ctx := context.Background()

	state := dynamicValue(t, "my-project-key")
	null := dynamicValue(t, nil)
	const typeName = "sonarqube_project"

	tests := []struct {
		name string
		call func()
		want string
	}{
		{"refresh", func() { _, _ = s.ReadResource(ctx, &tfprotov6.ReadResourceRequest{TypeName: typeName}) }, "read"},
		{"data source", func() { _, _ = s.ReadDataSource(ctx, &tfprotov6.ReadDataSourceRequest{TypeName: typeName}) }, "read"},
		{"import", func() { _, _ = s.ImportResourceState(ctx, &tfprotov6.ImportResourceStateRequest{TypeName: typeName}) }, "import"},
		{"create", func() {
			_, _ = s.ApplyResourceChange(ctx, &tfprotov6.ApplyResourceChangeRequest{TypeName: typeName, PriorState: null, PlannedState: state})
		}, "create"},
		{"update", func() {
			_, _ = s.ApplyResourceChange(ctx, &tfprotov6.ApplyResourceChangeRequest{TypeName: typeName, PriorState: state, PlannedState: state})
		}, "update"},
		{"delete", func() {
			_, _ = s.ApplyResourceChange(ctx, &tfprotov6.ApplyResourceChangeRequest{TypeName: typeName, PriorState: state, PlannedState: null})
		}, "delete"},
		{"plan", func() { _, _ = s.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: typeName}) }, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got = ""
			tc.call()

			want := "sonarqube-terraform/1.2.3"
			if tc.want != "" {
				want += " (" + typeName + "; " + tc.want + ")"
			}
			if got != want {
				t.Errorf("User-Agent = %q, want %q", got, want)
			}
		})
	}
}

func TestIsNull(t *testing.T) {
	tests := []struct {
		name  string
		value *tfprotov6.DynamicValue
		want  bool
	}{
		{"missing", nil, true},
		{"null", dynamicValue(t, nil), true},
		{"object", dynamicValue(t, "my-project-key"), false},
		{"empty", &tfprotov6.DynamicValue{}, false},
	}

	for _, tc := range tests {
		if got := isNull(tc.value); got != tc.want {
			t.Errorf("isNull(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// telemetryServer embeds the ProviderServer interface, so it hides the
// optional interfaces that the framework server finds on the provider. When
// the provider gets one of these features, telemetryServer must forward it.
func TestProviderNeedsNoHiddenServerInterface(t *testing.T) {
	p := New("test")()

	if _, ok := p.(provider.ProviderWithListResources); ok {
		t.Error("the provider has list resources: forward tfprotov6.ProviderServerWithListResource in telemetryServer")
	}
	if _, ok := p.(provider.ProviderWithActions); ok {
		t.Error("the provider has actions: forward tfprotov6.ProviderServerWithActions in telemetryServer")
	}
	if _, ok := p.(provider.ProviderWithStateStores); ok {
		t.Error("the provider has state stores: forward tfprotov6.ProviderServerWithStateStores in telemetryServer")
	}
}

// dynamicValue encodes an object with one key attribute as Terraform does. A
// nil key gives a null object.
func dynamicValue(t *testing.T, key any) *tfprotov6.DynamicValue {
	t.Helper()

	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"key": tftypes.String}}
	value := tftypes.NewValue(objectType, nil)
	if key != nil {
		value = tftypes.NewValue(objectType, map[string]tftypes.Value{"key": tftypes.NewValue(tftypes.String, key)})
	}

	dv, err := tfprotov6.NewDynamicValue(objectType, value)
	if err != nil {
		t.Fatalf("NewDynamicValue() error = %v", err)
	}
	return &dv
}

// Configure gives the client the type names of the provider, so a request of
// a resource names its operation.
func TestConfigureGivesTheClientTheTypeNames(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.UserAgent()
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	resp := configure(t, map[string]string{EnvToken: "a-token", EnvURL: srv.URL, EnvAPIURL: srv.URL}, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	ctx := client.WithOperation(context.Background(), "sonarqube_project", client.OperationRead)
	if _, err := configuredClient(t, resp).ListEnterprises(ctx); err != nil {
		t.Fatalf("ListEnterprises() error = %v", err)
	}

	want := "Terraform/" + testTerraformVersion + " (+https://www.terraform.io) sonarqube-terraform/test (sonarqube_project; read)"
	if got != want {
		t.Errorf("User-Agent = %q, want %q", got, want)
	}
}
