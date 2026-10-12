// Package validate holds the rules that the server applies to keys, for the
// resources and data sources of every product.
package validate

import (
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// OrganizationKeyPattern is the rule the server applies: lower-case letters,
// digits and dashes, with no leading and no trailing dash.
var OrganizationKeyPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ProjectKeyPattern is the rule the server applies: letters, digits, dashes,
// underscores, dots and colons, with at least one character that is not a
// digit.
var ProjectKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]*[A-Za-z_.:-][A-Za-z0-9_.:-]*$`)

// OrganizationKey is the schema validation for an organization key: the length
// that the server accepts and OrganizationKeyPattern.
func OrganizationKey() []validator.String {
	return []validator.String{
		stringvalidator.LengthBetween(1, 255),
		stringvalidator.RegexMatches(OrganizationKeyPattern,
			"must hold lower-case letters, digits and dashes only, with no leading and no trailing dash"),
	}
}
