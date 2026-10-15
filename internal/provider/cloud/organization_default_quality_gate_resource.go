package cloud

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// NewOrganizationDefaultQualityGateResource returns the
// sonarqube_cloud_organization_default_quality_gate resource.
func NewOrganizationDefaultQualityGateResource() resource.Resource {
	return &organizationDefaultResource{organizationDefault: defaultQualityGate}
}

var defaultQualityGate = organizationDefault{
	typeSuffix: "_cloud_organization_default_quality_gate",
	attribute:  "quality_gate_id",
	noun:       "quality gate",
	description: "Sets the default quality gate of a SonarQube Cloud organization. Each project " +
		"that has no quality gate of its own uses the default.\n\n" +
		"The gate can be one that Terraform does not manage, such as the built-in Sonar way " +
		"gate, which the `sonarqube_cloud_quality_gate` data source finds.\n\n" +
		"The organization must be on the Team or the Enterprise plan. On the Free plan, " +
		"SonarQube Cloud refuses to change the default.",
	attributeDescription: "UUID of the quality gate to set as the default. The gate must belong " +
		"to the organization.",
	get: getDefaultQualityGate,
	set: setDefaultQualityGate,
}

func getDefaultQualityGate(ctx context.Context, c *client.Client, organization string) (string, error) {
	org, err := c.GetOrganization(ctx, organization)
	if errors.Is(err, client.ErrNotFound) {
		return "", fmt.Errorf("%w: %w", client.ErrOrganizationNotFound, err)
	}
	if err != nil {
		return "", err
	}
	return org.DefaultQualityGateUUID, nil
}

// setDefaultQualityGate needs the UUID of the organization, because the
// quality gate API does not take its key.
func setDefaultQualityGate(ctx context.Context, c *client.Client, organization, gateID string) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	org, ok := lookupOrganization(ctx, c, organization, path.Root("organization"), &diagnostics)
	if !ok {
		return diagnostics
	}

	err := c.SetDefaultQualityGate(ctx, org.UUIDV4, gateID)
	var apiErr *client.APIError
	switch {
	case errors.Is(err, client.ErrNotFound):
		diagnostics.AddAttributeError(path.Root("quality_gate_id"),
			"Quality gate "+gateID+" not found",
			"No quality gate has this UUID. Check the UUID, and check that the token can read the gate.")
	case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden:
		diagnostics.AddError("Cannot set the default quality gate of the organization "+organization,
			"SonarQube Cloud refused the change: "+err.Error()+"\n\n"+
				"The organization must be on the Team or the Enterprise plan to change its default "+
				"quality gate, and the token must have the Administer Quality Gates permission.")
	case err != nil:
		diagnostics.AddError("Cannot set the default quality gate of the organization "+organization, err.Error())
	}
	return diagnostics
}
