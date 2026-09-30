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
  "id": "00000000-0000-4000-8000-000000000002",
  "organizationId": "organization-id",
  "organizationUuidV4": "00000000-0000-4000-8000-000000000001",
  "devOpsPlatform": "github",
  "bindingType": "integration-dop",
  "installationId": "12345678",
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
		OrganizationID:        "organization-id",
		DevOpsPlatform:        PlatformGitHub,
		InstallationID:        "12345678",
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
	if got, want := rec.body["organizationId"], "organization-id"; got != want {
		t.Errorf("organizationId = %v, want %q", got, want)
	}
	if got, want := rec.body["installationId"], "12345678"; got != want {
		t.Errorf("installationId = %v, want %q", got, want)
	}
	if got := rec.body["repoAutoImportEnabled"]; got != true {
		t.Errorf("repoAutoImportEnabled = %v, want true", got)
	}

	if got, want := binding.ID, "00000000-0000-4000-8000-000000000002"; got != want {
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
		OrganizationID: "organization-id",
		DevOpsPlatform: PlatformGitHub,
		InstallationID: "12345678",
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

	binding, err := newTestClient(srv).GetOrganizationBinding(t.Context(), "00000000-0000-4000-8000-000000000002")
	if err != nil {
		t.Fatalf("GetOrganizationBinding() returned %v", err)
	}

	if want := "/dop-translation/organization-bindings/00000000-0000-4000-8000-000000000002"; rec.path != want {
		t.Errorf("path = %q, want %q", rec.path, want)
	}
	if got, want := binding.InstallationID, "12345678"; got != want {
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

	binding, err := newTestClient(srv).FindOrganizationBinding(t.Context(), "organization-id")
	if err != nil {
		t.Fatalf("FindOrganizationBinding() returned %v", err)
	}

	if want := "/dop-translation/organization-bindings"; rec.path != want {
		t.Errorf("path = %q, want %q", rec.path, want)
	}
	if want := "organizationId=organization-id"; rec.query != want {
		t.Errorf("query = %q, want %q", rec.query, want)
	}
	if got, want := binding.OrganizationID, "organization-id"; got != want {
		t.Errorf("OrganizationID = %q, want %q", got, want)
	}
}

// The server answers a search for an organization that is not bound with a
// 404 and an empty body. Verified against a development instance.
func TestFindOrganizationBindingOfAnUnboundOrganization(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).FindOrganizationBinding(t.Context(), "organization-id")
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

	_, err := newTestClient(srv).FindOrganizationBinding(t.Context(), "organization-id")
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

	_, err := newTestClient(srv).FindOrganizationBinding(t.Context(), "organization-id")
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
		"00000000-0000-4000-8000-000000000002",
		PatchBindingRequest{RepoAutoImportEnabled: &autoImport})
	if err != nil {
		t.Fatalf("UpdateOrganizationBinding() returned %v", err)
	}

	if rec.method != http.MethodPatch {
		t.Errorf("method = %q, want %q", rec.method, http.MethodPatch)
	}
	if want := "/dop-translation/organization-bindings/00000000-0000-4000-8000-000000000002"; rec.path != want {
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
