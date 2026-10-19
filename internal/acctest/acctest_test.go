package acctest

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// The variable holds the names of the instances, so this repository holds
// none.
func TestRequireSafeTarget(t *testing.T) {
	t.Setenv(EnvAllowedHosts, "test.example.com, another.example.net")

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
		"http://api.localhost:9000",
		"http://127.0.0.1:9000",
	}

	for _, instanceURL := range refused {
		if err := RequireSafeTarget(instanceURL); err == nil {
			t.Errorf("RequireSafeTarget(%q) allowed an instance that is not allowed", instanceURL)
		}
	}
	for _, instanceURL := range allowed {
		if err := RequireSafeTarget(instanceURL); err != nil {
			t.Errorf("RequireSafeTarget(%q) = %v, want it allowed", instanceURL, err)
		}
	}
}

// The variable cannot open production, not even through a sub-domain.
func TestRequireSafeTargetNeverAllowsProduction(t *testing.T) {
	t.Setenv(EnvAllowedHosts, "sonarcloud.io, sonarqube.us")

	for _, instanceURL := range []string{
		"https://sonarcloud.io",
		"https://api.sonarcloud.io",
		"https://eu.sonarcloud.io",
		"https://www.sonarcloud.io.",
		"https://api.sonarqube.us",
	} {
		if err := RequireSafeTarget(instanceURL); err == nil {
			t.Errorf("RequireSafeTarget(%q) allowed production", instanceURL)
		}
	}
}

// A run that forgets the variable writes nowhere.
func TestRequireSafeTargetWithNoAllowedHosts(t *testing.T) {
	t.Setenv(EnvAllowedHosts, "")

	if err := RequireSafeTarget("https://test.example.com"); err == nil {
		t.Error("an instance was allowed although the variable is empty")
	}
	if err := RequireSafeTarget("http://localhost:9000"); err != nil {
		t.Errorf("a local instance was refused: %v", err)
	}
}

// The client derives the Web API v2 address api.localhost from a local
// instance, and that address is local too.
func TestRequireSafeClientAllowsALocalInstance(t *testing.T) {
	t.Setenv(EnvAllowedHosts, "")
	c := client.New(client.Config{URL: "http://localhost:9000", Product: client.ProductCloud})

	if err := requireSafeClient(c); err != nil {
		t.Errorf("a local instance was refused: %v", err)
	}
}

func TestWaitForReturnsWhenCheckIsDone(t *testing.T) {
	calls := 0
	err := WaitFor(func() (bool, error) {
		calls++
		return calls == 2, nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("calls = %d, err = %v", calls, err)
	}
}

func TestWaitForReturnsTheErrorOfCheck(t *testing.T) {
	failure := errors.New("refused")
	if err := WaitFor(func() (bool, error) { return false, failure }); !errors.Is(err, failure) {
		t.Fatalf("err = %v", err)
	}
}

func TestWaitForNeedsTrueResultsInSequence(t *testing.T) {
	results := []bool{true, false, true, true, true}
	calls := 0
	check := func() (bool, error) {
		calls++
		if calls > len(results) {
			t.Fatalf("calls = %d, want %d", calls, len(results))
		}
		return results[calls-1], nil
	}
	err := waitFor(check, 3, time.Second, time.Millisecond)
	if err != nil || calls != len(results) {
		t.Fatalf("calls = %d, err = %v", calls, err)
	}
}

// The results flap between the old and the new state, so they never show the
// new state three times in sequence.
func TestWaitForStopsAfterTimeoutWhenResultsFlap(t *testing.T) {
	results := []bool{true, true, false}
	calls := 0
	check := func() (bool, error) {
		calls++
		return results[(calls-1)%len(results)], nil
	}
	err := waitFor(check, 3, 20*time.Millisecond, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "did not show the expected state") {
		t.Fatalf("calls = %d, err = %v", calls, err)
	}
}

func TestWaitForSteadyReturnsTheErrorOfCheck(t *testing.T) {
	failure := errors.New("refused")
	if err := WaitForSteady(func() (bool, error) { return false, failure }, 2); !errors.Is(err, failure) {
		t.Fatalf("err = %v", err)
	}
}

func TestWaitForStopsAfterTimeout(t *testing.T) {
	err := waitFor(func() (bool, error) { return false, nil }, 1, time.Millisecond, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "did not show the expected state") {
		t.Fatalf("err = %v", err)
	}
}
