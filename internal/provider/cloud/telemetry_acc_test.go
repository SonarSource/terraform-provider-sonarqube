package cloud_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
)

// The provider works when the attribute turns off the optional telemetry.
func TestAccProviderWithTelemetryAttributeOff(t *testing.T) {
	testAccProviderWithoutTelemetry(t, "disable_telemetry = true")
}

// The provider works when DO_NOT_TRACK turns off the optional telemetry.
func TestAccProviderWithDoNotTrack(t *testing.T) {
	t.Setenv("DO_NOT_TRACK", "1")
	testAccProviderWithoutTelemetry(t, "")
}

func testAccProviderWithoutTelemetry(t *testing.T, providerBody string) {
	organization := os.Getenv(acctest.EnvTestOrganization)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "sonarqube" {
  %[2]s
}

data "sonarqube_cloud_organization" "test" {
  key = %[1]q
}
`, organization, providerBody),
				Check: resource.TestCheckResourceAttr("data.sonarqube_cloud_organization.test", "key", organization),
			},
		},
	})
}
