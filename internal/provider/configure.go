package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// requireCloudClient reads the client that the provider built, and refuses an
// instance that is not SonarQube Cloud.
//
// Every resource and data source of this provider needs the same three
// answers, so they all call this rather than keeping a copy each: the
// framework calls Configure with no data while it validates a configuration,
// the data must hold a client, and an organization exists in SonarQube Cloud
// only.
//
// A nil client with no diagnostic means that the provider is not configured
// yet, which is not a failure.
//
// subject names the thing that needs SonarQube Cloud, for example
// "The sonarqube_organization resource manages an organization".
func requireCloudClient(providerData any, subject string) (*client.Client, diag.Diagnostics) {
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
			"Organizations need SonarQube Cloud",
			subject+", which exists in SonarQube Cloud only.",
		)
		return nil, diagnostics
	}

	return c, diagnostics
}
