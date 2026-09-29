package client

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const bindingAnswer = `{
  "id": "0206a6e1-15dc-4481-888b-de0877286b27",
  "organizationId": "AZcwYwExlol79EFABiuM",
  "organizationUuidV4": "3ddd1f8f-2ab4-443f-a3be-a19ca418ca75",
  "devOpsPlatform": "github",
  "bindingType": "integration-dop",
  "installationId": "65381777",
  "devOpsPlatformUrl": "https://github.com/my-github-org",
  "repoAutoImportEnabled": false
}`

// recorder keeps what one call carried, so a test can assert the method, the
// path, the query and the body of a request in one place.
type recorder struct {
	method string
	path   string
	query  string
	body   map[string]any
}

func (rec *recorder) serve(answer string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.RawQuery

		if raw, err := io.ReadAll(r.Body); err == nil && len(raw) > 0 {
			_ = json.Unmarshal(raw, &rec.body)
		}
		w.Write([]byte(answer))
	}
}

func TestCreateOrganizationBinding(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(bindingAnswer))
	defer srv.Close()

	autoImport := true
	binding, err := newTestClient(srv).CreateOrganizationBinding(t.Context(), CreateBindingRequest{
		OrganizationID:        "AZcwYwExlol79EFABiuM",
		DevOpsPlatform:        PlatformGitHub,
		InstallationID:        "65381777",
		RepoAutoImportEnabled: &autoImport,
	})
	if err != nil {
		t.Fatalf("CreateOrganizationBinding() returned %v", err)
	}

	if rec.method != http.MethodPost {
		t.Errorf("method = %q, want %q", rec.method, http.MethodPost)
	}
	if want := "/dop-translation/organization-bindings"; rec.path != want {
		t.Errorf("path = %q, want %q", rec.path, want)
	}
	if got, want := rec.body["organizationId"], "AZcwYwExlol79EFABiuM"; got != want {
		t.Errorf("organizationId = %v, want %q", got, want)
	}
	if got, want := rec.body["installationId"], "65381777"; got != want {
		t.Errorf("installationId = %v, want %q", got, want)
	}
	if got := rec.body["repoAutoImportEnabled"]; got != true {
		t.Errorf("repoAutoImportEnabled = %v, want true", got)
	}

	if got, want := binding.ID, "0206a6e1-15dc-4481-888b-de0877286b27"; got != want {
		t.Errorf("ID = %q, want %q", got, want)
	}
	if binding.RepoAutoImportEnabled == nil || *binding.RepoAutoImportEnabled {
		t.Error("RepoAutoImportEnabled = nil or true, want the false that the server reported")
	}
}

// A field that holds nothing stays out of the body, because the API reads an
// absent field as "keep the current value".
func TestCreateOrganizationBindingLeavesOutTheAutoImport(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(bindingAnswer))
	defer srv.Close()

	_, err := newTestClient(srv).CreateOrganizationBinding(t.Context(), CreateBindingRequest{
		OrganizationID: "AZcwYwExlol79EFABiuM",
		DevOpsPlatform: PlatformGitHub,
		InstallationID: "65381777",
	})
	if err != nil {
		t.Fatalf("CreateOrganizationBinding() returned %v", err)
	}

	if _, sent := rec.body["repoAutoImportEnabled"]; sent {
		t.Error("repoAutoImportEnabled went with the request although it holds no value")
	}
}

func TestGetOrganizationBinding(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(bindingAnswer))
	defer srv.Close()

	binding, err := newTestClient(srv).GetOrganizationBinding(t.Context(), "0206a6e1-15dc-4481-888b-de0877286b27")
	if err != nil {
		t.Fatalf("GetOrganizationBinding() returned %v", err)
	}

	if want := "/dop-translation/organization-bindings/0206a6e1-15dc-4481-888b-de0877286b27"; rec.path != want {
		t.Errorf("path = %q, want %q", rec.path, want)
	}
	if got, want := binding.InstallationID, "65381777"; got != want {
		t.Errorf("InstallationID = %q, want %q", got, want)
	}
}

func TestGetOrganizationBindingNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetOrganizationBinding(t.Context(), "gone")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetOrganizationBinding() returned %v, want ErrNotFound", err)
	}
}

func TestFindOrganizationBinding(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(`{"organizationBindings":[` + bindingAnswer + `]}`))
	defer srv.Close()

	binding, err := newTestClient(srv).FindOrganizationBinding(t.Context(), "AZcwYwExlol79EFABiuM")
	if err != nil {
		t.Fatalf("FindOrganizationBinding() returned %v", err)
	}

	if want := "/dop-translation/organization-bindings"; rec.path != want {
		t.Errorf("path = %q, want %q", rec.path, want)
	}
	if want := "organizationId=AZcwYwExlol79EFABiuM"; rec.query != want {
		t.Errorf("query = %q, want %q", rec.query, want)
	}
	if got, want := binding.OrganizationID, "AZcwYwExlol79EFABiuM"; got != want {
		t.Errorf("OrganizationID = %q, want %q", got, want)
	}
}

// The server answers a search for an organization that is not bound with a
// 404 and an empty body. Verified against dev11.
func TestFindOrganizationBindingOfAnUnboundOrganization(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).FindOrganizationBinding(t.Context(), "AZcwYwExlol79EFABiuM")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("FindOrganizationBinding() returned %v, want ErrNotFound", err)
	}
}

// A collection that reports nothing found with a 200 must not become a
// binding with no fields.
func TestFindOrganizationBindingWithAnEmptyList(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"organizationBindings":[]}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).FindOrganizationBinding(t.Context(), "AZcwYwExlol79EFABiuM")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("FindOrganizationBinding() returned %v, want ErrNotFound", err)
	}
}

// A collection with more than one entry is unexpected. The call must not
// silently pick one of them.
func TestFindOrganizationBindingWithMoreThanOneEntry(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"organizationBindings":[` + bindingAnswer + `,` + bindingAnswer + `]}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).FindOrganizationBinding(t.Context(), "AZcwYwExlol79EFABiuM")
	if err == nil {
		t.Fatal("FindOrganizationBinding() returned no error, want one")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("FindOrganizationBinding() returned ErrNotFound, want a distinct error")
	}
}

func TestUpdateOrganizationBinding(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(bindingAnswer))
	defer srv.Close()

	autoImport := false
	_, err := newTestClient(srv).UpdateOrganizationBinding(t.Context(),
		"0206a6e1-15dc-4481-888b-de0877286b27",
		PatchBindingRequest{RepoAutoImportEnabled: &autoImport})
	if err != nil {
		t.Fatalf("UpdateOrganizationBinding() returned %v", err)
	}

	if rec.method != http.MethodPatch {
		t.Errorf("method = %q, want %q", rec.method, http.MethodPatch)
	}
	if want := "/dop-translation/organization-bindings/0206a6e1-15dc-4481-888b-de0877286b27"; rec.path != want {
		t.Errorf("path = %q, want %q", rec.path, want)
	}
	if got := rec.body["repoAutoImportEnabled"]; got != false {
		t.Errorf("repoAutoImportEnabled = %v, want false", got)
	}
	// The installation cannot change, so it must never go with the request.
	if _, sent := rec.body["installationId"]; sent {
		t.Error("installationId went with the change request")
	}
}
