package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTelemetryAllowsOnlyTheGivenTypesAndKnownOperations(t *testing.T) {
	c := New(Config{URL: "https://dev.example.io", Telemetry: true, TelemetryTypes: []string{"sonarqube_project"}})

	for _, op := range []Operation{OperationCreate, OperationRead, OperationUpdate, OperationDelete, OperationImport} {
		if !c.telemetryAllows("sonarqube_project", op) {
			t.Errorf("telemetryAllows(sonarqube_project, %q) = false, want true", op)
		}
	}

	refused := []struct {
		typeName  string
		operation Operation
	}{
		{"sonarqube_group", OperationCreate},
		{"my-organization", OperationRead},
		{"", OperationRead},
		{"sonarqube_project", "destroy"},
		{"sonarqube_project", "my-project-key"},
		{"sonarqube_project", ""},
	}
	for _, tc := range refused {
		if c.telemetryAllows(tc.typeName, tc.operation) {
			t.Errorf("telemetryAllows(%q, %q) = true, want false", tc.typeName, tc.operation)
		}
	}
}

func TestTelemetryAllowsNothingWhenTurnedOff(t *testing.T) {
	c := New(Config{URL: "https://dev.example.io", TelemetryTypes: []string{"sonarqube_project"}})

	if c.Telemetry() {
		t.Error("Telemetry() = true, want false")
	}
	if c.telemetryAllows("sonarqube_project", OperationCreate) {
		t.Error("telemetryAllows() = true with the optional telemetry turned off, want false")
	}
}

func TestUserAgentNamesTheOperation(t *testing.T) {
	const base = "Terraform/1.9.8 (+https://www.terraform.io) sonarqube-terraform/1.2.3"
	on := Config{URL: "https://dev.example.io", UserAgent: base, AppendedUserAgent: "my-ci/2.0",
		Telemetry: true, TelemetryTypes: []string{"sonarqube_project"}}
	off := on
	off.Telemetry = false

	tests := []struct {
		name string
		cfg  Config
		ctx  context.Context
		want string
	}{
		{"operation", on, WithOperation(context.Background(), "sonarqube_project", OperationCreate),
			base + " (sonarqube_project; create) my-ci/2.0"},
		{"no operation", on, context.Background(), base + " my-ci/2.0"},
		{"type not on the allowlist", on, WithOperation(context.Background(), "my-project-key", OperationCreate),
			base + " my-ci/2.0"},
		{"operation not on the allowlist", on, WithOperation(context.Background(), "sonarqube_project", "plan"),
			base + " my-ci/2.0"},
		{"telemetry off", off, WithOperation(context.Background(), "sonarqube_project", OperationCreate),
			base + " my-ci/2.0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := New(tc.cfg).userAgentFor(tc.ctx); got != tc.want {
				t.Errorf("userAgentFor() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The request gets the operation from its context.
func TestRequestSendsTheOperation(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.UserAgent()
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, APIURL: srv.URL, Token: "test-token", UserAgent: "sonarqube-terraform/1.2.3",
		HTTPClient: srv.Client(), Telemetry: true, TelemetryTypes: []string{"sonarqube_cloud_enterprise"}})
	ctx := WithOperation(context.Background(), "sonarqube_cloud_enterprise", OperationRead)
	if _, err := c.ListEnterprises(ctx); err != nil {
		t.Fatalf("ListEnterprises() error = %v", err)
	}

	if want := "sonarqube-terraform/1.2.3 (sonarqube_cloud_enterprise; read)"; got != want {
		t.Errorf("User-Agent = %q, want %q", got, want)
	}
}
