package provider

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
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

	testAccPreCheckToken(t)
	if os.Getenv(envTestOrganization) == "" {
		t.Skipf("%s is not set, so the acceptance test has no organization to read", envTestOrganization)
	}
}

// testAccPreCheckToken stops a test that has no credentials.
func testAccPreCheckToken(t *testing.T) {
	t.Helper()

	if os.Getenv(envToken) == "" {
		t.Skipf("%s is not set, so the acceptance test cannot reach an instance", envToken)
	}
}

// testAccPreCheckDestructive stops a test that makes and deletes an
// organization when the target is not safe.
//
// The provider defaults to production, so a token alone would be enough to
// make and delete organizations there. A deletion in production starts
// billing events (SC-45583) and can leave a binding behind (SC-48456).
func testAccPreCheckDestructive(t *testing.T) {
	t.Helper()

	testAccPreCheckToken(t)

	instanceURL := os.Getenv(envURL)
	if instanceURL == "" {
		t.Skipf("%s is not set. A test that makes and deletes an organization must name "+
			"its instance, because the provider otherwise uses %s.", envURL, client.CloudURL)
	}

	// A wrong target is a fault in the setup, not a test that cannot run, so
	// stop instead of skipping. A skip counts as a pass in Go.
	if err := requireSafeTarget(instanceURL); err != nil {
		t.Fatal(err.Error())
	}
}

// productionHosts are the instances that hold the organizations of customers.
// They get their own message, because naming one is the mistake that this
// guard exists to catch.
var productionHosts = map[string]bool{
	"sonarcloud.io":     true,
	"www.sonarcloud.io": true,
	"sonarqube.us":      true,
	"www.sonarqube.us":  true,
}

// safeHostPattern matches the instances where a test may make and delete an
// organization: the staging instance and a development instance.
var safeHostPattern = regexp.MustCompile(`^([a-z0-9-]+\.)*sc-(staging|dev[0-9]+)\.io$`)

// requireSafeTarget reports an address that no destructive test may use.
//
// This allows the hosts it knows rather than refusing the hosts it knows to be
// dangerous. A list of dangerous hosts cannot know about an address that
// reaches production another way, and the cost of being wrong is a deletion in
// production. The cost of being wrong the other way is one line to add here,
// and a message that says so.
func requireSafeTarget(instanceURL string) error {
	parsed, err := url.Parse(instanceURL)
	if err != nil {
		return fmt.Errorf("%s holds %q, which is not an address: %w", envURL, instanceURL, err)
	}

	// Take off every trailing dot before the test. A hostname that ends in a
	// dot is absolute, and DNS and the check of a TLS name both read
	// "sonarcloud.io." as "sonarcloud.io", so the two reach the same instance.
	host := strings.TrimRight(strings.ToLower(parsed.Hostname()), ".")

	if productionHosts[host] {
		return fmt.Errorf("%s names the production instance %q. A test that makes and "+
			"deletes an organization must never run there: a deletion starts billing "+
			"events and can leave a binding behind. Use a development or a staging "+
			"instance.", envURL, host)
	}

	if safeHostPattern.MatchString(host) {
		return nil
	}
	if address := net.ParseIP(host); address != nil && address.IsLoopback() {
		return nil
	}
	if host == "localhost" {
		return nil
	}

	return fmt.Errorf("%s names %q, which is no instance that this test knows. A test "+
		"that makes and deletes an organization runs against a staging instance, a "+
		"development instance, or a local one. Add the host to safeHostPattern when it "+
		"is safe.", envURL, host)
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
