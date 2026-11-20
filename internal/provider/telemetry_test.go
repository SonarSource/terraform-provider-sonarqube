package provider

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestTelemetryEnabled(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		attribute types.Bool
		want      bool
	}{
		{name: "nothing set", attribute: types.BoolNull(), want: true},
		{name: "attribute false", attribute: types.BoolValue(false), want: true},
		{name: "attribute true", attribute: types.BoolValue(true)},
		{name: "env true", env: map[string]string{EnvDisableTelemetry: "true"}},
		{name: "env 1", env: map[string]string{EnvDisableTelemetry: " 1 "}},
		{name: "env false", env: map[string]string{EnvDisableTelemetry: "false"}, want: true},
		{name: "env false does not override attribute true", env: map[string]string{EnvDisableTelemetry: "false"}, attribute: types.BoolValue(true)},
		{name: "do not track 1", env: map[string]string{envDoNotTrack: "1"}},
		{name: "do not track true", env: map[string]string{envDoNotTrack: "true"}},
		{name: "do not track not a boolean", env: map[string]string{envDoNotTrack: "yes"}},
		{name: "do not track 0", env: map[string]string{envDoNotTrack: "0"}, want: true},
		{name: "do not track wins over attribute false", env: map[string]string{envDoNotTrack: "1"}, attribute: types.BoolValue(false)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, diags := telemetryEnabled(tc.attribute, func(name string) string { return tc.env[name] })
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if got != tc.want {
				t.Errorf("telemetryEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTelemetryEnabledRefusesAnInvalidVariable(t *testing.T) {
	_, diags := telemetryEnabled(types.BoolNull(), func(name string) string {
		if name == EnvDisableTelemetry {
			return "maybe"
		}
		return ""
	})

	if !diags.HasError() || diags[0].Summary() != "Invalid value in "+EnvDisableTelemetry {
		t.Errorf("diagnostics = %v, want an error for %s", diags, EnvDisableTelemetry)
	}
}

func TestConfigureSetsTheTelemetry(t *testing.T) {
	for _, tc := range []struct {
		env  map[string]string
		want bool
	}{
		{env: map[string]string{EnvToken: "a-token"}, want: true},
		{env: map[string]string{EnvToken: "a-token", envDoNotTrack: "1"}, want: false},
	} {
		resp := configure(t, tc.env, nil)
		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
		}
		if got := configuredClient(t, resp).Telemetry(); got != tc.want {
			t.Errorf("Telemetry() with %v = %v, want %v", tc.env, got, tc.want)
		}
	}
}

// The optional telemetry never changes the base User-Agent.
func TestConfigureKeepsTheUserAgentWithoutTelemetry(t *testing.T) {
	resp := configure(t, map[string]string{EnvToken: "a-token", envDoNotTrack: "1", envAppendUserAgent: "my-ci/2.0"}, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	want := "Terraform/" + testTerraformVersion + " (+https://www.terraform.io) sonarqube-terraform/test my-ci/2.0"
	if got := configuredClient(t, resp).UserAgent(); got != want {
		t.Errorf("UserAgent() = %q, want %q", got, want)
	}
}

func TestConfigureRefusesAnInvalidDisableTelemetryVariable(t *testing.T) {
	resp := configure(t, map[string]string{EnvToken: "a-token", EnvDisableTelemetry: "maybe"}, nil)

	assertErrorContains(t, resp, "Invalid value in "+EnvDisableTelemetry)
}

func TestConfigureRefusesAnUnknownDisableTelemetry(t *testing.T) {
	resp := configure(t,
		map[string]string{EnvToken: "a-token"},
		map[string]tftypes.Value{
			"disable_telemetry": tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue),
		})

	assertErrorContains(t, resp, "Unknown value for disable_telemetry")
}

// The allowlist has the full type names of the resources and the data
// sources, with the provider prefix.
func TestTelemetryTypes(t *testing.T) {
	p := &sonarqubeProvider{version: "test"}
	names := p.telemetryTypes(context.Background())

	for _, want := range []string{"sonarqube_project", "sonarqube_cloud_organization", "sonarqube_cloud_dop_applications"} {
		if !slices.Contains(names, want) {
			t.Errorf("telemetryTypes() has no %q", want)
		}
	}
	for _, name := range names {
		if rest, ok := strings.CutPrefix(name, providerTypeName+"_"); !ok || rest == "" {
			t.Errorf("telemetryTypes() has %q, want a %s_ prefix and a name", name, providerTypeName)
		}
	}
}
