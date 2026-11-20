package client

import "testing"

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
