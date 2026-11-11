package acctest

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
)

// setGitLabSelfManagedEnvironment sets every variable of
// PreCheckGitLabSelfManaged, against a local instance that the safety check
// allows.
func setGitLabSelfManagedEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv(resource.EnvTfAcc, "1")
	t.Setenv(provider.EnvURL, "http://127.0.0.1:1")
	t.Setenv(provider.EnvAPIURL, "http://127.0.0.1:1")
	t.Setenv(provider.EnvToken, "test-token")
	t.Setenv(EnvTestGitLabURL, "https://gitlab.example.com")
	t.Setenv(EnvTestGitLabGroup, "my-group")
	t.Setenv(EnvTestGitLabToken, "first-token")
	t.Setenv(EnvTestGitLabSecondToken, "second-token")
	t.Setenv(EnvTestEnterpriseKey, "my-enterprise")
}

func TestPreCheckGitLabSelfManagedReturnsTheValues(t *testing.T) {
	setGitLabSelfManagedEnvironment(t)

	got := PreCheckGitLabSelfManaged(t)
	want := GitLabSelfManaged{
		URL:           "https://gitlab.example.com",
		Group:         "my-group",
		Token:         "first-token",
		SecondToken:   "second-token",
		EnterpriseKey: "my-enterprise",
	}
	if got != want {
		t.Errorf("PreCheckGitLabSelfManaged() = %+v, want %+v", got, want)
	}
}

// Each variable is necessary. A test without one of them skips, and
// make testacc-gitlab then fails the run.
func TestPreCheckGitLabSelfManagedSkipsWithoutAVariable(t *testing.T) {
	for _, name := range []string{
		EnvTestGitLabURL, EnvTestGitLabGroup, EnvTestGitLabToken, EnvTestGitLabSecondToken, EnvTestEnterpriseKey,
	} {
		t.Run(name, func(t *testing.T) {
			setGitLabSelfManagedEnvironment(t)
			t.Setenv(name, "")

			reached := false
			t.Run("precheck", func(t *testing.T) {
				PreCheckGitLabSelfManaged(t)
				reached = true
			})
			if reached {
				t.Errorf("the precheck did not skip without %s", name)
			}
		})
	}
}
