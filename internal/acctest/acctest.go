// Package acctest holds helpers for the acceptance tests of every package.
package acctest

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
)

// KeyPrefix marks every item that a test makes: the organizations, and the
// projects, groups, permission templates and quality gates in the shared
// organization. The sweepers in sweep.go find what an interrupted run leaves
// behind by this prefix.
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

// paidPlans can change the defaults of an organization and make permission
// templates. The free plans cannot.
var paidPlans = []string{"team", "enterprise", "old_paid"}

// PreCheckSharedOrganization stops a test that changes the organization of
// EnvTestOrganization when the target is not safe, as PreCheckDestructive
// does, or when that organization cannot hold the test. It skips when the
// variable is not set. It stops the run when the organization is not on a
// paid plan, when the token user has no direct admin grant on it, or when its
// key starts with KeyPrefix, which marks what a test makes. These are faults
// in the setup, and a skip would count as a pass. It returns the key of the
// organization.
func PreCheckSharedOrganization(t *testing.T) string {
	t.Helper()

	PreCheckDestructive(t)
	organization := os.Getenv(EnvTestOrganization)
	if organization == "" {
		t.Skipf("%s is not set, so the acceptance test has no organization to change", EnvTestOrganization)
	}
	if err := checkSharedOrganization(t.Context(), Client(), organization); err != nil {
		t.Fatal(err.Error())
	}
	return organization
}

func checkSharedOrganization(ctx context.Context, c *client.Client, organization string) error {
	if strings.HasPrefix(organization, KeyPrefix) {
		return fmt.Errorf("the key %s of %s starts with %s, which marks the organizations that a test "+
			"makes. Use an organization with a different key", organization, EnvTestOrganization, KeyPrefix)
	}
	org, err := c.GetOrganization(ctx, organization)
	if err != nil {
		return fmt.Errorf("cannot read the organization %s of %s: %w", organization, EnvTestOrganization, err)
	}
	plan, err := c.GetCurrentPlan(ctx, org.UUIDV4)
	if err != nil {
		return fmt.Errorf("cannot read the plan of the organization %s: %w", organization, err)
	}
	if !slices.Contains(paidPlans, plan) {
		return fmt.Errorf("the organization %s of %s is on the plan %q. The tests need the Team or the "+
			"Enterprise plan, which can change the defaults and make permission templates",
			organization, EnvTestOrganization, plan)
	}
	login, err := c.CurrentUserLogin(ctx)
	if err != nil {
		return fmt.Errorf("cannot read the token user: %w", err)
	}
	permissions, err := c.GetPermissions(ctx, client.PermissionScope{Organization: organization},
		client.PermissionPrincipal{Name: login})
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		return fmt.Errorf("cannot read the permissions of %s on %s: %w", login, organization, err)
	}
	if !slices.Contains(permissions, "admin") {
		return fmt.Errorf("the token user %s has no direct admin grant on the organization %s of %s. "+
			"Give it one. Admin through a group is not sufficient: when no other user is an "+
			"administrator, SonarQube Cloud refuses to remove the user from a group, and the tests "+
			"cannot clean up", login, organization, EnvTestOrganization)
	}
	return nil
}

// Client returns a client for the instance that the environment names, as the
// provider configures it. A test uses it to change or read the instance
// outside of Terraform.
func Client() *client.Client {
	return client.New(client.Config{
		URL:     cmp.Or(os.Getenv(provider.EnvURL), client.CloudURL),
		APIURL:  os.Getenv(provider.EnvAPIURL),
		Token:   os.Getenv(provider.EnvToken),
		Product: client.ProductCloud,
	})
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
	if err := requireSafeClient(Client()); err != nil {
		t.Fatal(err.Error())
	}
}

// requireSafeClient checks both addresses that the client sends to. Web API
// v2 answers at its own address, which SONARQUBE_API_URL can set to any
// host, and v2 makes deletes too.
func requireSafeClient(c *client.Client) error {
	if err := RequireSafeTarget(c.URL()); err != nil {
		return err
	}
	if err := RequireSafeTarget(c.APIURL()); err != nil {
		return fmt.Errorf("the Web API v2 address %s, which %s sets or %s gives: %w",
			c.APIURL(), provider.EnvAPIURL, provider.EnvURL, err)
	}
	return nil
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

	// Nothing on the machine that runs the test can be production. RFC 6761
	// keeps the sub-domains of localhost for loopback too, and the Web API v2
	// address of a local instance is api.localhost.
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
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

// WaitFor calls check each second until it reports true, an error, or 60
// seconds pass. Some lists of SonarQube Cloud show a change only after some
// seconds.
func WaitFor(check func() (bool, error)) error {
	return waitFor(check, 1, 60*time.Second, time.Second)
}

// WaitForSteady is WaitFor, but check must report true on times calls in
// sequence. Some lists of SonarQube Cloud show the new state and then the old
// state again for some seconds, so one true result is not sufficient before a
// step that reads the list again.
func WaitForSteady(check func() (bool, error), times int) error {
	return waitFor(check, times, 60*time.Second, time.Second)
}

func waitFor(check func() (bool, error), times int, timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)
	for seen := 0; ; {
		ok, err := check()
		if err != nil {
			return err
		}
		if ok {
			seen++
		} else {
			seen = 0
		}
		if seen >= times {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the API did not show the expected state in %s", timeout)
		}
		time.Sleep(interval)
	}
}
