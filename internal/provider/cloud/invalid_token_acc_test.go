package cloud_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
)

// Each API answers a wrong token in a different way, so the provider checks
// the token before any resource or data source calls an API.
func TestAccInvalidToken(t *testing.T) {
	for name, token := range map[string]string{
		"not a token":    "not-a-real-token",
		"unknown token":  "squ_0000000000000000000000000000000000000000",
		"unknown scoped": "sqco_0000000000000000000000000000000000000000",
	} {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { acctest.PreCheck(t) },
				ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config: fmt.Sprintf(`provider "sonarqube" {
  token = %q
}
data "sonarqube_cloud_organization" "test" {
  key = %q
}
`, token, os.Getenv(acctest.EnvTestOrganization)),
					ExpectError: regexp.MustCompile(`Invalid token[\s\S]*wrong,\s+expired\s+or\s+revoked`),
				}},
			})
		})
	}
}
