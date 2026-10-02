package cloud

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// lookupOrganization reads the organization that carries a key, and turns a
// failure into a diagnostic. It reports whether the caller may go on.
//
// The organization data source, the binding resource and the binding data
// source all start here, so that one wording serves them all. The binding
// needs it for a second reason: the bindings API names an organization by an
// internal identifier, and a read of the organization is the only place that
// reports it.
//
// attribute names the configuration attribute that holds the key, so that the
// message points at the line that must change.
//
// The Read method of the organization resource does not call this. It answers
// a missing organization by dropping the resource from the state, not by a
// diagnostic.
func lookupOrganization(
	ctx context.Context,
	c *client.Client,
	organizationKey string,
	attribute path.Path,
	diagnostics *diag.Diagnostics,
) (*client.Organization, bool) {
	org, err := c.GetOrganization(ctx, organizationKey)
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			// An organization that the token may not see answers 404 as well.
			diagnostics.AddAttributeError(
				attribute,
				"Organization "+organizationKey+" not found",
				"No organization with this key was found at "+c.APIURL()+
					". Check the key, and check that the token can read the organization.",
			)
			return nil, false
		}
		diagnostics.AddError("Cannot read the organization "+organizationKey, err.Error())
		return nil, false
	}
	return org, true
}
