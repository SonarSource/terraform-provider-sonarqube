package acctest

import "testing"

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
