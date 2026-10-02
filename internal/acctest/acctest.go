// Package acctest holds helpers for the acceptance tests of every package.
package acctest

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
)

// KeyPrefix marks every organization that a test makes. SC-58198
// registers a sweeper that deletes what an interrupted run leaves behind, and
// the sweeper finds the organizations by this prefix.
const KeyPrefix = "tf-acc-test-"

// EnvTestOrganization names an organization that the token can read.
const EnvTestOrganization = "SONARQUBE_TEST_ORGANIZATION"

// The provider runs inside the test process.
var ProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"sonarqube": providerserver.NewProtocol6WithError(provider.New("test")()),
}

// resource.Test already skips every test when TF_ACC is unset. This adds the
// settings that this provider needs.
func PreCheck(t *testing.T) {
	t.Helper()

	PreCheckToken(t)
	if os.Getenv(EnvTestOrganization) == "" {
		t.Skipf("%s is not set, so the acceptance test has no organization to read", EnvTestOrganization)
	}
}

// PreCheckToken stops a test that has no credentials.
func PreCheckToken(t *testing.T) {
	t.Helper()

	if os.Getenv(provider.EnvToken) == "" {
		t.Skipf("%s is not set, so the acceptance test cannot reach an instance", provider.EnvToken)
	}
}

// PreCheckDestructive stops a test that makes and deletes an
// organization when the target is not safe.
//
// The provider defaults to production, so a token alone would be enough to
// make and delete organizations there. A deletion in production starts
// billing events (SC-45583) and can leave a binding behind (SC-48456).
func PreCheckDestructive(t *testing.T) {
	t.Helper()

	PreCheckToken(t)

	instanceURL := os.Getenv(provider.EnvURL)
	if instanceURL == "" {
		t.Skipf("%s is not set. A test that makes and deletes an organization must name "+
			"its instance, because the provider otherwise uses %s.", provider.EnvURL, client.CloudURL)
	}
	// A wrong target is a fault in the setup, not a test that cannot run, so
	// stop instead of skipping. A skip counts as a pass in Go. An empty
	// SONARQUBE_TEST_ALLOWED_HOSTS therefore stops the run as well, for every
	// instance but a local one.
	if err := RequireSafeTarget(instanceURL); err != nil {
		t.Fatal(err.Error())
	}
}

// productionHosts are the instances that hold the organizations of customers.
// They get their own message, because naming one is the mistake that this
// guard exists to catch. Each entry covers its sub-domains, which is what
// api.sonarcloud.io and www.sonarcloud.io are.
var productionHosts = []string{"sonarcloud.io", "sonarqube.us"}

// isProduction reports a host that reaches an instance of customers, itself
// or through a sub-domain.
func isProduction(host string) bool {
	for _, production := range productionHosts {
		if host == production || strings.HasSuffix(host, "."+production) {
			return true
		}
	}
	return false
}

// EnvAllowedHosts holds the hosts where a test may make and delete an
// organization, separated by commas. Whoever runs the destructive
// acceptance tests sets it; AGENTS.md says so too. Empty allows nothing, and
// the name of no instance then has to live in this public repository.
const EnvAllowedHosts = "SONARQUBE_TEST_ALLOWED_HOSTS"

// RequireSafeTarget reports an address that no destructive test may use.
//
// It allows what EnvAllowedHosts names and refuses the rest, because a list
// of dangerous hosts cannot know every address that reaches production, and
// the cost of being wrong there is a deletion in production.
func RequireSafeTarget(instanceURL string) error {
	parsed, err := url.Parse(instanceURL)
	if err != nil {
		return fmt.Errorf("%s holds %q, which is not an address: %w", provider.EnvURL, instanceURL, err)
	}

	host := normaliseHost(parsed.Hostname())

	if isProduction(host) {
		return fmt.Errorf("%s names the production instance %q. A test that makes and "+
			"deletes an organization must never run there: a deletion starts billing "+
			"events and can leave a binding behind. Use a development or a staging "+
			"instance.", provider.EnvURL, host)
	}

	// Nothing on the machine that runs the test can be production.
	if host == "localhost" {
		return nil
	}
	if address := net.ParseIP(host); address != nil && address.IsLoopback() {
		return nil
	}

	// The test above runs first, and it names the sub-domains too, so the
	// variable below can allow nothing that reaches production.
	for _, allowed := range allowedHosts() {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return nil
		}
	}

	return fmt.Errorf("%s names %q, which %s does not allow. A test that makes and "+
		"deletes an organization runs against an instance that %s names. Add the host "+
		"to that variable when it is safe.", provider.EnvURL, host, EnvAllowedHosts, EnvAllowedHosts)
}

func allowedHosts() []string {
	hosts := []string{}
	for _, entry := range strings.Split(os.Getenv(EnvAllowedHosts), ",") {
		if host := normaliseHost(entry); host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

// normaliseHost takes off a trailing dot, which marks a hostname as absolute:
// DNS and the check of a TLS name read "sonarcloud.io." as "sonarcloud.io".
func normaliseHost(host string) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(host)), ".")
}

// EnvTestInstallationID names an installation of the GitHub application of
// the instance that no organization is bound to. Nothing can make one, so a
// test that binds needs a person to give it.
const EnvTestInstallationID = "SONARQUBE_TEST_GITHUB_INSTALLATION_ID"

// PreCheckBinding stops a test that has no installation to bind.
func PreCheckBinding(t *testing.T) {
	t.Helper()

	PreCheckDestructive(t)

	if os.Getenv(EnvTestInstallationID) == "" {
		t.Skipf("%s is not set. A test that binds an organization needs an installation "+
			"of the GitHub application of the instance that no organization is bound to.",
			EnvTestInstallationID)
	}
}

// EnvTestRepository names a public GitHub repository that the installation of
// EnvTestInstallationID can see. The repository must be public, because the
// project that the test makes is public, and the server refuses to bind a
// public project to a private repository.
const EnvTestRepository = "SONARQUBE_TEST_GITHUB_REPOSITORY"
