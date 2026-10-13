// Package validate holds the rules that the server applies to keys, for the
// resources and data sources of every product.
package validate

import (
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// OrganizationKeyPattern is the rule the server applies: the key must equal its
// own slug. Thus it holds lower-case letters, digits, underscores, plus signs
// and dashes, with no leading dash, no trailing dash, and no two dashes in a
// row.
var OrganizationKeyPattern = regexp.MustCompile(`^([a-z0-9_+]+-)*[a-z0-9_+]+$`)

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
			"must hold lower-case letters, digits, underscores, plus signs and dashes only, "+
				"with no leading dash, no trailing dash and no two dashes in a row"),
	}
}

// ProjectKey is the schema validation for a project key: the length that the
// server accepts and ProjectKeyPattern.
func ProjectKey() []validator.String {
	return []validator.String{
		stringvalidator.LengthBetween(1, 400),
		stringvalidator.RegexMatches(ProjectKeyPattern,
			"must hold letters, digits, dashes, underscores, dots and colons only, "+
				"with at least one character that is not a digit"),
	}
}
