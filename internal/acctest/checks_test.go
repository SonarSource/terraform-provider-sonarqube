package acctest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

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

func TestImportErrorBuildsAnImportStep(t *testing.T) {
	t.Parallel()
	step := ImportError("sonarqube_project.test", "my-organization/"+MissingKey, MissingImport)
	if !step.ImportState || step.ResourceName != "sonarqube_project.test" ||
		step.ImportStateId != "my-organization/"+MissingKey || step.ExpectError != MissingImport {
		t.Errorf("ImportError() = %+v", step)
	}
}

func TestPlanActionExpectsOneAction(t *testing.T) {
	t.Parallel()
	checks := PlanAction("sonarqube_project.test", plancheck.ResourceActionReplace)
	if len(checks.PreApply) != 1 || len(checks.PostApplyPreRefresh) != 0 || len(checks.PostApplyPostRefresh) != 0 {
		t.Errorf("PlanAction() = %+v, want one pre-apply check", checks)
	}
}

func TestDeletedOutsideDeletesAndWaits(t *testing.T) {
	t.Parallel()
	deleted, reads := false, 0
	step := DeletedOutside(t, "sonarqube_project.test", "config", nil,
		func() error { deleted = true; return nil },
		func() (string, error) { reads++; return "", client.ErrNotFound })

	step.PreConfig()
	if !deleted || reads != 1 {
		t.Errorf("PreConfig deleted = %t after %d reads, want a delete and one read", deleted, reads)
	}
	if step.Config != "config" || len(step.ConfigPlanChecks.PreApply) != 1 {
		t.Errorf("DeletedOutside() = %+v, want the config and a plan check", step)
	}
}

func TestDeleteAndWaitReportsEachFailure(t *testing.T) {
	t.Parallel()
	failure := errors.New("forbidden")
	gone := func() (string, error) { return "", client.ErrNotFound }
	there := func() (string, error) { return "", failure }

	if err := deleteAndWait("address", func() error { return failure }, gone); !errors.Is(err, failure) ||
		!strings.Contains(err.Error(), "cannot delete address") {
		t.Errorf("failed delete: deleteAndWait() = %v", err)
	}
	if err := deleteAndWait("address", func() error { return nil }, there); !errors.Is(err, failure) ||
		!strings.Contains(err.Error(), "address is not deleted") {
		t.Errorf("failed read: deleteAndWait() = %v", err)
	}
}
