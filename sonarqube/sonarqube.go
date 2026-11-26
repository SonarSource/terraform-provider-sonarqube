// Package sonarqube gives the provider constructor to Go programs outside
// this module, for example Crossplane Upjet. The API is experimental until
// version 1.0.0. See the README for how to configure the provider.
package sonarqube

import (
	"github.com/hashicorp/terraform-plugin-framework/provider"

	internalprovider "github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
)

// New returns the factory of the provider. The version goes in the
// User-Agent header of each request. The requests of this provider send no
// optional telemetry.
func New(version string) func() provider.Provider {
	return internalprovider.New(version)
}
