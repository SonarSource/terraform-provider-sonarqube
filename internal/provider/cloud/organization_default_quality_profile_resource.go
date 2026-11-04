package cloud

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// NewOrganizationDefaultQualityProfileResource returns the
// sonarqube_cloud_organization_default_quality_profile resource.
func NewOrganizationDefaultQualityProfileResource() resource.Resource {
	return &organizationDefaultResource{organizationDefault: defaultQualityProfile}
}

var defaultQualityProfile = organizationDefault{
	typeSuffix: "_cloud_organization_default_quality_profile",
	attribute:  "quality_profile_key",
	noun:       "quality profile",
	description: "Sets the default quality profile of a language in a SonarQube Cloud organization. Each " +
		"project that has no quality profile of its own for the language uses the default.\n\n" +
		"The profile can be one that Terraform does not manage, such as a built-in profile, which the " +
		"`sonarqube_quality_profile` data source finds.\n\n" +
		"A custom profile needs the Team or the Enterprise plan, and a MISRA built-in profile needs the " +
		"MISRA entitlement.",
	attributeDescription: "Key of the quality profile to set as the default. The profile must belong to " +
		"the organization and be of the language.",
	scope: &organizationDefaultScope{
		attribute:   "language",
		description: "Key of the language, such as `java` or `py`.",
	},
	get: func(ctx context.Context, c *client.Client, organization, language string) (string, error) {
		profile, err := c.GetDefaultQualityProfile(ctx, organization, language)
		if errors.Is(err, client.ErrNotFound) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return profile.Key, nil
	},
	set: setDefaultQualityProfile,
}

// setDefaultQualityProfile checks the language of the profile first, because
// set_default makes the profile the default of its own language and ignores
// the language of the configuration.
func setDefaultQualityProfile(ctx context.Context, c *client.Client, organization, language, key string) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	summary := "Cannot set the default quality profile of the language " + language + " of the organization " + organization

	profile, err := c.GetQualityProfile(ctx, organization, "", key)
	switch {
	case errors.Is(err, client.ErrOrganizationNotFound):
		diagnostics.AddAttributeError(path.Root("organization"), "Organization "+organization+" not found",
			"Check the key, and check that the token can read the organization.")
		return diagnostics
	case errors.Is(err, client.ErrNotFound):
		diagnostics.AddAttributeError(path.Root("quality_profile_key"), "Quality profile "+key+" not found",
			"The organization "+organization+" has no quality profile with this key. Check the key.")
		return diagnostics
	case err != nil:
		diagnostics.AddError(summary, err.Error())
		return diagnostics
	case profile.Language != language:
		diagnostics.AddAttributeError(path.Root("quality_profile_key"), "Quality profile of a different language",
			"The quality profile "+key+" ("+profile.Name+") is a profile of the language "+profile.Language+
				", not of the language "+language+". Use a profile of the language "+language+".")
		return diagnostics
	}

	if err := c.SetDefaultQualityProfile(ctx, key); err != nil {
		diagnostics.AddError(summary, err.Error())
	}
	return diagnostics
}
