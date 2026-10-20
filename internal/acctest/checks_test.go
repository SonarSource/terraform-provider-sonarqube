package acctest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
)

func TestWaitForGoneReturnsWhenNotFound(t *testing.T) {
	t.Parallel()
	err := WaitForGone(func() (string, error) { return "", client.ErrNotFound })
	if err != nil {
		t.Errorf("WaitForGone() = %v, want nil", err)
	}
}

func TestWaitForGoneStopsAtAnotherError(t *testing.T) {
	t.Parallel()
	failure := errors.New("forbidden")
	err := WaitForGone(func() (string, error) { return "", failure })
	if !errors.Is(err, failure) {
		t.Errorf("WaitForGone() = %v, want %v", err, failure)
	}
}

func TestCheckGoneNamesTheObject(t *testing.T) {
	t.Parallel()
	err := CheckGone("project my-project", func() (string, error) { return "", errors.New("forbidden") })(nil)
	if err == nil || !strings.Contains(err.Error(), "project my-project is not deleted") {
		t.Errorf("CheckGone() = %v, want an error that names the project", err)
	}
}

// setStatusServer points Client at a server that answers each request with
// status.
func setStatusServer(t *testing.T, status int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(provider.EnvURL, srv.URL)
	t.Setenv(provider.EnvAPIURL, srv.URL)
	t.Setenv(provider.EnvToken, "test-token")
}

func TestDeletedChecksAcceptNotFound(t *testing.T) {
	setStatusServer(t, http.StatusNotFound)

	checks := map[string]resource.TestCheckFunc{
		"organizations": CheckOrganizationsDeleted(t, "first", "second"),
		"project":       CheckProjectDeleted(t, "my-organization", "my-project"),
	}
	for name, check := range checks {
		if err := check(nil); err != nil {
			t.Errorf("%s: check = %v, want nil", name, err)
		}
	}
}

func TestDeletedChecksReportEachObject(t *testing.T) {
	setStatusServer(t, http.StatusForbidden)

	err := CheckOrganizationsDeleted(t, "first", "second")(nil)
	for _, key := range []string{"organization first", "organization second"} {
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("CheckOrganizationsDeleted() = %v, want an error that names %s", err, key)
		}
	}
	if err := CheckProjectDeleted(t, "my-organization", "my-project")(nil); err == nil {
		t.Error("CheckProjectDeleted() = nil, want the error of the read")
	}
}

func TestPreCheckAcceptanceSkipsWithoutTFAcc(t *testing.T) {
	t.Setenv(resource.EnvTfAcc, "")

	reached := false
	t.Run("precheck", func(t *testing.T) {
		PreCheckAcceptance(t)
		reached = true
	})
	if reached {
		t.Errorf("the precheck did not skip without %s", resource.EnvTfAcc)
	}
}

func TestPreCheckAcceptanceRunsWithTFAcc(t *testing.T) {
	t.Setenv(resource.EnvTfAcc, "1")

	reached := false
	t.Run("precheck", func(t *testing.T) {
		PreCheckAcceptance(t)
		reached = true
	})
	if !reached {
		t.Errorf("the precheck skipped with %s set", resource.EnvTfAcc)
	}
}
