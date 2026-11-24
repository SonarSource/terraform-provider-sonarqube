package cloud

import (
	"context"
	"maps"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/validate"
)

// projectScopedAttributes adds the "id", "organization" and "project_key"
// attributes to the other attributes of a resource that belongs to one
// project. The ID of such a resource is "organization/project_key", and a
// different organization or key replaces the resource.
func projectScopedAttributes(others map[string]schema.Attribute) map[string]schema.Attribute {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	attributes := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:      true,
			Description:   "Organization key and project key, separated by a slash.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"organization": schema.StringAttribute{
			Required:      true,
			Description:   "Key of the organization that owns the project. A change replaces this resource.",
			Validators:    validate.OrganizationKey(),
			PlanModifiers: replace,
		},
		"project_key": schema.StringAttribute{
			Required:      true,
			Description:   "Key of the project. A change replaces this resource.",
			Validators:    validate.ProjectKey(),
			PlanModifiers: replace,
		},
	}
	maps.Copy(attributes, others)
	return attributes
}

// importProjectScoped takes "organization/project_key" and writes the two
// keys to the state. Read fills in the rest. subject names the resource in
// the error.
func importProjectScoped(ctx context.Context, subject string, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organization, key, separated := strings.Cut(req.ID, "/")
	if !separated || organization == "" || key == "" {
		resp.Diagnostics.AddError(
			"Cannot import the "+subject,
			"The identifier must hold the organization and the project key, separated "+
				"by a slash, as in \"my-organization/my-project\". It holds "+req.ID+".")
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_key"), key)...)
}
