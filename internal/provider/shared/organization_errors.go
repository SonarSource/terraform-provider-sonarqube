package shared

import (
	"errors"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

func organizationNotFound(organization string) string {
	return "The organization " + organization + " was not found"
}

// organizationErrorDetail explains a missing organization. The client already
// adds hints to the other errors.
func organizationErrorDetail(err error, organization string) string {
	if errors.Is(err, client.ErrOrganizationNotFound) {
		return organizationNotFound(organization) + ". Check the key, and check that the token can read the organization."
	}
	return err.Error()
}
