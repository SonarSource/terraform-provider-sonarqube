package cloud

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// NewOrganizationDefaultPermissionTemplateResource returns the
// sonarqube_cloud_organization_default_permission_template resource.
func NewOrganizationDefaultPermissionTemplateResource() resource.Resource {
	return &organizationDefaultResource{organizationDefault: defaultPermissionTemplate}
}

var defaultPermissionTemplate = organizationDefault{
	typeSuffix: "_cloud_organization_default_permission_template",
	attribute:  "template_id",
	noun:       "permission template",
	description: "Sets the default permission template of a SonarQube Cloud organization. A new project " +
		"whose key matches no project key pattern of a template gets the permissions of the default.\n\n" +
		"The template can be one that Terraform does not manage, such as the template that SonarQube " +
		"Cloud made with the organization.",
	attributeDescription: "Identifier of the permission template to set as the default. The template must " +
		"belong to the organization.",
	get: func(ctx context.Context, c *client.Client, organization string) (string, error) {
		return c.GetDefaultPermissionTemplateID(ctx, organization)
	},
	set: setDefaultPermissionTemplate,
}

func setDefaultPermissionTemplate(ctx context.Context, c *client.Client, organization, templateID string) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	err := c.SetDefaultPermissionTemplate(ctx, organization, templateID)
	switch {
	case errors.Is(err, client.ErrOrganizationNotFound):
		diagnostics.AddAttributeError(path.Root("organization"), "Organization "+organization+" not found",
			"Check the key, and check that the token can read the organization.")
	case errors.Is(err, client.ErrNotFound):
		diagnostics.AddAttributeError(path.Root("template_id"), "Permission template "+templateID+" not found",
			"The organization "+organization+" has no permission template with this identifier. Check the "+
				"identifier, and check that the template belongs to the organization.")
	case err != nil:
		diagnostics.AddError("Cannot set the default permission template of the organization "+organization, err.Error())
	}
	return diagnostics
}
