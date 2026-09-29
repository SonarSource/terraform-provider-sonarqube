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

// The variable holds the names of the instances, so this repository holds
// none.
func TestRequireSafeTarget(t *testing.T) {
	t.Setenv(envAllowedHosts, "test.example.com, another.example.net")

	refused := []string{
		"https://sonarcloud.io",
		"https://SonarCloud.io/",
		"https://www.sonarcloud.io",
		"https://sonarqube.us",
		"https://sonarcloud.io.",
		"https://SonarCloud.io./",
		"https://sonarqube.us.",
		"https://18.66.147.51",
		"https://example.com",
		"https://not-listed.example.org",
		// An allowed name inside another name allows nothing.
		"https://test.example.com.evil.example.org",
	}
	allowed := []string{
		"https://test.example.com",
		"https://TEST.example.com/",
		"https://test.example.com.",
		// An entry allows its sub-domains.
		"https://eu.test.example.com",
		"https://another.example.net",
		"http://localhost:9000",
		"http://127.0.0.1:9000",
	}

	for _, instanceURL := range refused {
		if err := requireSafeTarget(instanceURL); err == nil {
			t.Errorf("requireSafeTarget(%q) allowed an instance that is not allowed", instanceURL)
		}
	}
	for _, instanceURL := range allowed {
		if err := requireSafeTarget(instanceURL); err != nil {
			t.Errorf("requireSafeTarget(%q) = %v, want it allowed", instanceURL, err)
		}
	}
}

// The variable cannot open production, not even through a sub-domain.
func TestRequireSafeTargetNeverAllowsProduction(t *testing.T) {
	t.Setenv(envAllowedHosts, "sonarcloud.io, sonarqube.us")

	for _, instanceURL := range []string{
		"https://sonarcloud.io",
		"https://api.sonarcloud.io",
		"https://eu.sonarcloud.io",
		"https://www.sonarcloud.io.",
		"https://api.sonarqube.us",
	} {
		if err := requireSafeTarget(instanceURL); err == nil {
			t.Errorf("requireSafeTarget(%q) allowed production", instanceURL)
		}
	}
}

// A run that forgets the variable writes nowhere.
func TestRequireSafeTargetWithNoAllowedHosts(t *testing.T) {
	t.Setenv(envAllowedHosts, "")

	if err := requireSafeTarget("https://test.example.com"); err == nil {
		t.Error("an instance was allowed although the variable is empty")
	}
	if err := requireSafeTarget("http://localhost:9000"); err != nil {
		t.Errorf("a local instance was refused: %v", err)
	}
}
