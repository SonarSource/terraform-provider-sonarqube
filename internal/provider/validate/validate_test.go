package validate_test

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/validate"
)

func hasError(t *testing.T, validators []validator.String, value string) bool {
	t.Helper()
	for _, v := range validators {
		req := validator.StringRequest{ConfigValue: types.StringValue(value)}
		resp := &validator.StringResponse{}
		v.ValidateString(t.Context(), req, resp)
		if resp.Diagnostics.HasError() {
			return true
		}
	}
	return false
}

func TestOrganizationKey(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"my-org", "a", "org1", "my_org", "a+b", "_"} {
		if hasError(t, validate.OrganizationKey(), key) {
			t.Errorf("OrganizationKey() refused %q", key)
		}
	}
	for _, key := range []string{"", "My_Org", "-org", "org-", "my--org", "-", "my org", "my.org", strings.Repeat("a", 256)} {
		if !hasError(t, validate.OrganizationKey(), key) {
			t.Errorf("OrganizationKey() accepted %q", key)
		}
	}
}

func TestProjectKey(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"my_project", "org:repo.name-1", "a", "123a", strings.Repeat("a", 400)} {
		if hasError(t, validate.ProjectKey(), key) {
			t.Errorf("ProjectKey() refused %q", key)
		}
	}
	for _, key := range []string{"", "12345", "bad key", "bad/key", strings.Repeat("a", 401)} {
		if !hasError(t, validate.ProjectKey(), key) {
			t.Errorf("ProjectKey() accepted %q", key)
		}
	}
}
