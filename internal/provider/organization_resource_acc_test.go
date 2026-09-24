package provider

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// acceptanceKeyPrefix marks every organization that a test makes. SC-58198
// registers a sweeper that deletes what an interrupted run leaves behind, and
// the sweeper finds the organizations by this prefix.
const acceptanceKeyPrefix = "tf-acc-test-"

// The test makes and deletes a real organization, so it needs a live instance
// and a token. resource.Test skips it when TF_ACC is unset.
func TestAccOrganizationResource(t *testing.T) {
	key := fmt.Sprintf("%s%d", acceptanceKeyPrefix, rand.Uint32())
	renamedKey := key + "-renamed"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckDestructive(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccOrganizationConfig(key, "First name", "First description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sonarqube_organization.test", "key", key),
					resource.TestCheckResourceAttr("sonarqube_organization.test", "id", key),
					resource.TestCheckResourceAttr("sonarqube_organization.test", "name", "First name"),
					resource.TestCheckResourceAttr("sonarqube_organization.test", "description", "First description"),
				),
			},
			{
				// A new name and description change the organization in place.
				Config: testAccOrganizationConfig(key, "Second name", "Second description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sonarqube_organization.test", "name", "Second name"),
					resource.TestCheckResourceAttr("sonarqube_organization.test", "description", "Second description"),
				),
			},
			{
				// A new key renames the organization. The id follows it.
				Config: testAccOrganizationConfig(renamedKey, "Second name", "Second description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("sonarqube_organization.test", "key", renamedKey),
					resource.TestCheckResourceAttr("sonarqube_organization.test", "id", renamedKey),
				),
			},
			{
				ResourceName:      "sonarqube_organization.test",
				ImportState:       true,
				ImportStateId:     renamedKey,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccOrganizationConfig(key, name, description string) string {
	return fmt.Sprintf(`
resource "sonarqube_organization" "test" {
  key         = %[1]q
  name        = %[2]q
  description = %[3]q
}
`, key, name, description)
}

// A test that makes and deletes an organization must reach a test instance
// only. The guard allows the hosts it knows, because a list of dangerous hosts
// cannot know about every address that reaches production.
func TestRequireSafeTarget(t *testing.T) {
	t.Parallel()

	refused := []string{
		"https://sonarcloud.io",
		"https://SonarCloud.io/",
		"https://www.sonarcloud.io",
		"https://sonarqube.us",
		// A hostname that ends in a dot is absolute, and it reaches the same
		// instance.
		"https://sonarcloud.io.",
		"https://SonarCloud.io./",
		"https://sonarqube.us.",
		// An address that the guard does not know, whatever it is.
		"https://18.66.147.51",
		"https://example.com",
		"https://sonarcloud.io.example.com",
		"https://sc-dev11.io.example.com",
	}
	allowed := []string{
		"https://dev11.sc-dev11.io",
		"https://sc-dev11.io",
		"https://sc-staging.io",
		"https://dev.sc-staging.io",
		"http://localhost:9000",
		"http://127.0.0.1:9000",
	}

	for _, instanceURL := range refused {
		if err := requireSafeTarget(instanceURL); err == nil {
			t.Errorf("requireSafeTarget(%q) allowed an instance that is not a test instance", instanceURL)
		}
	}
	for _, instanceURL := range allowed {
		if err := requireSafeTarget(instanceURL); err != nil {
			t.Errorf("requireSafeTarget(%q) = %v, want it allowed", instanceURL, err)
		}
	}
}
