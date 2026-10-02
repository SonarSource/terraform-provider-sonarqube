package provider

import (
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// envTestInstallationID names an installation of the GitHub application of
// the instance that no organization is bound to. Nothing can make one, so a
// test that binds needs a person to give it.
const envTestInstallationID = "SONARQUBE_TEST_GITHUB_INSTALLATION_ID"

// testAccPreCheckBinding stops a test that has no installation to bind.
func testAccPreCheckBinding(t *testing.T) {
	t.Helper()

	testAccPreCheckDestructive(t)

	if os.Getenv(envTestInstallationID) == "" {
		t.Skipf("%s is not set. A test that binds an organization needs an installation "+
			"of the GitHub application of the instance that no organization is bound to.",
			envTestInstallationID)
	}
}

// The test makes an organization, binds it, and deletes the organization,
// which is the only thing that removes a binding. The installation is free
// again afterwards, so the test can run twice.
//
// It covers what a test of the methods cannot see: that a second plan is
// empty, that an import gives a state that answers the configuration, and
// what the plan does with an attribute that cannot change.
func TestAccCloudOrganizationBindingResource(t *testing.T) {
	key := fmt.Sprintf("%s%d", acceptanceKeyPrefix, rand.Uint32())
	installationID := os.Getenv(envTestInstallationID)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckBinding(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// resource.Test plans again after this apply and fails when
				// that plan is not empty, which is what UseStateForUnknown on
				// the computed attributes has to deliver.
				Config: testAccBindingConfig(key, installationID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(bindingResource, "organization_key", key),
					resource.TestCheckResourceAttr(bindingResource, "installation_id", installationID),
					// The default of the schema, which no configuration above sets.
					resource.TestCheckResourceAttr(bindingResource, "dev_ops_platform", client.PlatformGitHub),
					resource.TestCheckResourceAttrSet(bindingResource, "id"),
					resource.TestCheckResourceAttrSet(bindingResource, "organization_id"),
					resource.TestCheckResourceAttrSet(bindingResource, "binding_type"),
					// The data source reads the same binding back through a
					// search by organization, which is another path than the
					// read of the resource by identifier.
					resource.TestCheckResourceAttrPair(
						bindingDataSource, "id", bindingResource, "id"),
					resource.TestCheckResourceAttrPair(
						bindingDataSource, "installation_id", bindingResource, "installation_id"),
					resource.TestCheckResourceAttrPair(
						bindingDataSource, "organization_id", bindingResource, "organization_id"),
				),
			},
			{
				// An import takes the key of the organization, and Read fills
				// in the rest. ImportStateVerify then compares every attribute
				// with the state that the apply wrote, which is how the
				// default of dev_ops_platform after an import is checked.
				ResourceName:      bindingResource,
				ImportState:       true,
				ImportStateId:     key,
				ImportStateVerify: true,
			},
			{
				// The installation cannot change. The plan must say so,
				// because an apply cannot: the delete of a binding removes
				// nothing, so the organization stays bound and the new bind
				// is refused.
				//
				// The expectation names the summary of the diagnostic, and
				// not a word of it. PlanOnly reports a plan that is not empty
				// as an error that carries the whole rendered plan, and that
				// plan names installation_id and its two values. A shorter
				// expectation would therefore also pass when the modifier
				// stops firing, which is the failure that this step exists to
				// catch.
				Config:      testAccBindingConfig(key, installationID+"0"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`The installation of a binding cannot change`),
			},
		},
	})
}

// The addresses in the configuration below.
const (
	bindingResource   = "sonarqube_cloud_organization_binding.test"
	bindingDataSource = "data.sonarqube_cloud_organization_binding.test"
)

// testAccBindingConfig makes the organization and binds it. The two go
// together, because a destroy of the organization is what removes the
// binding.
func testAccBindingConfig(key, installationID string) string {
	return fmt.Sprintf(`
resource "sonarqube_cloud_organization" "test" {
  key  = %[1]q
  name = "Terraform acceptance test"
}

resource "sonarqube_cloud_organization_binding" "test" {
  organization_key = sonarqube_cloud_organization.test.key
  installation_id  = %[2]q
}

# Reads the binding that the resource above made. The reference gives the
# order: the data source reads after the bind.
data "sonarqube_cloud_organization_binding" "test" {
  organization_key = sonarqube_cloud_organization_binding.test.organization_key
}
`, key, installationID)
}
