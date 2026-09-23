package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// envTestOrganization names an organization that the token can read.
const envTestOrganization = "SONARQUBE_TEST_ORGANIZATION"

// The provider runs inside the test process.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"sonarqube": providerserver.NewProtocol6WithError(New("test")()),
}

// resource.Test already skips every test when TF_ACC is unset. This adds the
// settings that this provider needs.
func testAccPreCheck(t *testing.T) {
	t.Helper()

	for _, name := range []string{envToken, envTestOrganization} {
		if os.Getenv(name) == "" {
			t.Skipf("%s is not set, so the acceptance test cannot reach an instance", name)
		}
	}
}

func TestAccOrganizationDataSource(t *testing.T) {
	organization := os.Getenv(envTestOrganization)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "sonarqube_organization" "test" {
  key = %[1]q
}
`, organization),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.sonarqube_organization.test", "key", organization),
					resource.TestCheckResourceAttrSet("data.sonarqube_organization.test", "name"),
				),
			},
		},
	})
}
