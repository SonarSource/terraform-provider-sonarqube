package provider

import (
	"context"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	// EnvDisableTelemetry turns off the optional telemetry.
	EnvDisableTelemetry = "SONARQUBE_DISABLE_TELEMETRY"
	// envDoNotTrack is the convention of many developer tools to turn off
	// telemetry. See https://consoledonottrack.com.
	envDoNotTrack = "DO_NOT_TRACK"
)

// telemetryEnabled is false when one of the attribute, EnvDisableTelemetry or
// DO_NOT_TRACK turns off the optional telemetry.
func telemetryEnabled(attribute types.Bool, getenv func(string) string) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	disabled := attribute.ValueBool()

	if value := strings.TrimSpace(getenv(EnvDisableTelemetry)); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			diags.AddError(
				"Invalid value in "+EnvDisableTelemetry,
				"The "+EnvDisableTelemetry+" environment variable must be a boolean, for example true, false, 1 or 0, not "+
					strconv.Quote(value)+".",
			)
			return false, diags
		}
		disabled = disabled || parsed
	}

	// Other tools read DO_NOT_TRACK too, so a value that is not a boolean
	// does not stop the provider. Each value other than false turns off the
	// optional telemetry.
	if value := strings.TrimSpace(getenv(envDoNotTrack)); value != "" {
		if parsed, err := strconv.ParseBool(value); err != nil || parsed {
			disabled = true
		}
	}

	return !disabled, diags
}

// telemetryTypes returns the type names of all resources and data sources of
// the provider: the only type names that the optional telemetry can send.
func (p *sonarqubeProvider) telemetryTypes(ctx context.Context) []string {
	var names []string
	for _, newResource := range p.Resources(ctx) {
		resp := &resource.MetadataResponse{}
		newResource().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: providerTypeName}, resp)
		names = append(names, resp.TypeName)
	}
	for _, newDataSource := range p.DataSources(ctx) {
		resp := &datasource.MetadataResponse{}
		newDataSource().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: providerTypeName}, resp)
		names = append(names, resp.TypeName)
	}
	return names
}
