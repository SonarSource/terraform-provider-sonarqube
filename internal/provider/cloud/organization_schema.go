package cloud

import (
	"maps"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/validate"
)

// organizationSettingAttributes adds the "id" and "organization" attributes to
// the other attributes of a resource that holds one setting of an
// organization. The ID of such a resource is the organization key, and a
// different key replaces the resource.
func organizationSettingAttributes(others map[string]schema.Attribute) map[string]schema.Attribute {
	attributes := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:      true,
			Description:   "Key of the organization.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"organization": schema.StringAttribute{
			Required:      true,
			Description:   "Key of the organization. A change replaces this resource.",
			Validators:    validate.OrganizationKey(),
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
		},
	}
	maps.Copy(attributes, others)
	return attributes
}
