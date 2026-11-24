package cloud

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// lookupProject is lookupOrganization for a resource inside a project. It
// reads the organization once, then the project, and turns a failure into a
// diagnostic on the attribute that must change.
func lookupProject(
	ctx context.Context,
	c *client.Client,
	organizationKey, projectKey string,
	diagnostics *diag.Diagnostics,
) (*client.Organization, *client.Project, bool) {
	org, ok := lookupOrganization(ctx, c, organizationKey, path.Root("organization"), diagnostics)
	if !ok {
		return nil, nil, false
	}
	project, err := c.GetProjectInOrganization(ctx, org, projectKey)
	if errors.Is(err, client.ErrNotFound) {
		addProjectNotFound(diagnostics, organizationKey, projectKey)
		return nil, nil, false
	}
	if err != nil {
		diagnostics.AddError("Cannot read the project "+projectKey, err.Error())
		return nil, nil, false
	}
	return org, project, true
}

// refreshProject is refreshOrganization for a resource inside a project. A
// missing organization or project removes the resource from the state with a
// warning. subject names what the state loses.
func refreshProject(
	ctx context.Context,
	c *client.Client,
	organizationKey, projectKey, subject string,
	resp *resource.ReadResponse,
) (*client.Organization, *client.Project, bool) {
	org, ok := refreshOrganization(ctx, c, organizationKey, subject, resp)
	if !ok {
		return nil, nil, false
	}
	project, err := c.GetProjectInOrganization(ctx, org, projectKey)
	if errors.Is(err, client.ErrNotFound) {
		forgetMissingProject(ctx, projectKey, subject, resp)
		return nil, nil, false
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the project "+projectKey, err.Error())
		return nil, nil, false
	}
	return org, project, true
}

// forgetMissingProject removes the resource from the state with a warning,
// because its project cannot be read.
func forgetMissingProject(ctx context.Context, projectKey, subject string, resp *resource.ReadResponse) {
	resp.Diagnostics.AddWarning(
		"The project "+projectKey+" was not found",
		"Terraform removes "+subject+" from the state. The project was deleted, or the token "+
			"cannot read it any more. In the second case, the next apply fails, because the "+
			"project cannot be found.",
	)
	resp.State.RemoveResource(ctx)
}
