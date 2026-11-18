// Package configure gives the resources and data sources of every product the
// client that the provider built.
package configure

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// CloudClient reads the client that the provider built. It refuses an
// instance that is not SonarQube Cloud, and a scoped organization token.
//
// Every resource and data source of this provider needs the same answers, so
// they all call this rather than keeping a copy each: the framework calls
// Configure with no data while it validates a configuration, the data must
// hold a client, and the resource works on SonarQube Cloud only in this
// release.
//
// The framework calls Configure before each read, plan, import and apply, so
// a refusal here stops each of them, and Terraform keeps the state.
//
// A nil client with no diagnostic means that the provider is not configured
// yet, which is not a failure.
//
// subject names the thing that needs SonarQube Cloud, for example
// "The sonarqube_cloud_organization resource manages an organization".
func CloudClient(providerData any, subject string) (*client.Client, diag.Diagnostics) {
	c, diagnostics := CloudClientAcceptingScopedToken(providerData, subject)
	if c != nil && c.ScopedToken() {
		diagnostics.AddError(
			"This needs a user token",
			subject+", and it does not support a scoped organization token. Set a user token in the "+
				"provider. The provider docs list the resources and data sources that support scoped "+
				"organization tokens.",
		)
		return nil, diagnostics
	}
	return c, diagnostics
}

// CloudClientAcceptingScopedToken is CloudClient for the resources and data
// sources that support a scoped organization token.
func CloudClientAcceptingScopedToken(providerData any, subject string) (*client.Client, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	if providerData == nil {
		return nil, diagnostics
	}

	c, ok := providerData.(*client.Client)
	if !ok {
		diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *client.Client, got %T. This is a fault in the provider.", providerData),
		)
		return nil, diagnostics
	}

	if !c.IsCloud() {
		diagnostics.AddError(
			"This needs SonarQube Cloud",
			subject+", which exists in SonarQube Cloud only.",
		)
		return nil, diagnostics
	}

	return c, diagnostics
}
