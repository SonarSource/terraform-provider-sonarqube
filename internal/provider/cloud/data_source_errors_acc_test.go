package cloud_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
)

// Each step reads an object that does not exist, and the read must fail. A
// data source that gives an empty result instead would hide a wrong key in
// the configuration.
func TestAccDataSourcesMissingObject(t *testing.T) {
	organization := os.Getenv(acctest.EnvTestOrganization)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "sonarqube_cloud_organization" "test" {
  key = %q
}
`, acctest.MissingKey),
				ExpectError: regexp.MustCompile(`Organization ` + acctest.MissingKey + ` not found`),
			},
			{
				Config: fmt.Sprintf(`
data "sonarqube_cloud_organization_binding" "test" {
  organization_key = %q
}
`, acctest.MissingKey),
				ExpectError: regexp.MustCompile(`Organization ` + acctest.MissingKey + ` not found`),
			},
			{
				Config: fmt.Sprintf(`
data "sonarqube_cloud_quality_gate" "test" {
  organization = %q
  name         = %q
}
`, organization, acctest.MissingKey),
				ExpectError: regexp.MustCompile(`Cannot find the quality gate`),
			},
			{
				Config: fmt.Sprintf(`
data "sonarqube_cloud_project_binding" "test" {
  organization = %q
  project_key  = %q
}
`, organization, acctest.MissingKey),
				ExpectError: regexp.MustCompile(`Project ` + acctest.MissingKey + ` not found`),
			},
		},
	})
}
