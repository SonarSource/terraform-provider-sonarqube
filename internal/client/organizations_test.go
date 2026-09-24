package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const organizationAnswer = `[
  {
    "id": "AZcwYwExlol79EFABiuM",
    "uuidV4": "3ddd1f8f-2ab4-443f-a3be-a19ca418ca75",
    "key": "my-org",
    "name": "My Organization",
    "description": "Managed by Terraform",
    "url": "https://example.com",
    "avatarUrl": "https://example.com/avatar.png"
  }
]`

func TestGetOrganization(t *testing.T) {
	t.Parallel()

	var gotPath, gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("organizationKey")
		w.Write([]byte(organizationAnswer))
	}))
	defer srv.Close()

	org, err := newTestClient(srv).GetOrganization(t.Context(), "my-org")
	if err != nil {
		t.Fatalf("GetOrganization() returned %v", err)
	}

	if want := "/organizations/organizations"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if want := "my-org"; gotQuery != want {
		t.Errorf("organizationKey parameter = %q, want %q", gotQuery, want)
	}

	if got, want := org.Name, "My Organization"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	if got, want := org.AvatarURL, "https://example.com/avatar.png"; got != want {
		t.Errorf("AvatarURL = %q, want %q", got, want)
	}
}

// Web API v2 answers 404 for a key that names no organization, and for an
// organization that the token may not see.
func TestGetOrganizationNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Organization with key my-org is not found"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetOrganization(t.Context(), "my-org")

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetOrganization() returned %v, want an error that matches ErrNotFound", err)
	}
}

// An empty array is not a shape the API uses today, but it must not become an
// organization with empty fields if that ever changes.
func TestGetOrganizationEmptyAnswer(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetOrganization(t.Context(), "my-org")

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetOrganization() returned %v, want an error that matches ErrNotFound", err)
	}
}

// Guards against an answer that carries more than the organization that was
// asked for.
func TestGetOrganizationIgnoresAnotherKey(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[{"key":"another-org","name":"Another"}]`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetOrganization(t.Context(), "my-org")

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetOrganization() returned %v, want an error that matches ErrNotFound", err)
	}
}

// stringPtr is the shortest way to give an OrganizationRequest a field that is
// set, as opposed to one that is absent.
func stringPtr(s string) *string { return &s }

// formServer answers every request and records the path and the form it
// carried.
func formServer(t *testing.T, path *string, form *url.Values) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("cannot read the form: %v", err)
		}
		*path = r.URL.Path
		*form = r.PostForm
		w.WriteHeader(http.StatusNoContent)
	}))
}

// A create leaves out every field the caller did not set, so that the server
// applies its own default.
func TestCreateOrganization(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotForm url.Values

	srv := formServer(t, &gotPath, &gotForm)
	defer srv.Close()

	err := newTestClient(srv).CreateOrganization(t.Context(), OrganizationRequest{
		Key:  "my-org",
		Name: stringPtr("My Organization"),
	})
	if err != nil {
		t.Fatalf("CreateOrganization() returned %v", err)
	}

	if want := "/api/organizations/create"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if got, want := gotForm.Get("key"), "my-org"; got != want {
		t.Errorf("key = %q, want %q", got, want)
	}
	if got, want := gotForm.Get("name"), "My Organization"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
	if _, present := gotForm["description"]; present {
		t.Error("description was sent although the caller left it out")
	}
}

// An update sends an empty parameter for a field with no value, because the
// server keeps the current value of a parameter that is absent.
func TestUpdateOrganizationClearsAField(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotForm url.Values

	srv := formServer(t, &gotPath, &gotForm)
	defer srv.Close()

	err := newTestClient(srv).UpdateOrganization(t.Context(), OrganizationRequest{
		Key:         "my-org",
		Name:        stringPtr("My Organization"),
		Description: stringPtr(""),
	})
	if err != nil {
		t.Fatalf("UpdateOrganization() returned %v", err)
	}

	if want := "/api/organizations/update"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	value, present := gotForm["description"]
	if !present {
		t.Fatal("description was not sent, so the field cannot be cleared")
	}
	if got := value[0]; got != "" {
		t.Errorf("description = %q, want an empty value", got)
	}
}

func TestUpdateOrganizationKey(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotForm url.Values

	srv := formServer(t, &gotPath, &gotForm)
	defer srv.Close()

	if err := newTestClient(srv).UpdateOrganizationKey(t.Context(), "old-key", "new-key"); err != nil {
		t.Fatalf("UpdateOrganizationKey() returned %v", err)
	}

	if want := "/api/organizations/update_key"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if got, want := gotForm.Get("key"), "old-key"; got != want {
		t.Errorf("key = %q, want %q", got, want)
	}
	if got, want := gotForm.Get("newKey"), "new-key"; got != want {
		t.Errorf("newKey = %q, want %q", got, want)
	}
}

func TestDeleteOrganization(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotForm url.Values

	srv := formServer(t, &gotPath, &gotForm)
	defer srv.Close()

	if err := newTestClient(srv).DeleteOrganization(t.Context(), "my-org"); err != nil {
		t.Fatalf("DeleteOrganization() returned %v", err)
	}

	if want := "/api/organizations/delete"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if got, want := gotForm.Get("organization"), "my-org"; got != want {
		t.Errorf("organization = %q, want %q", got, want)
	}
}

// A delete of an organization that is already gone succeeds, so that a second
// destroy, or a destroy after a deletion made elsewhere, does not fail.
func TestDeleteOrganizationAlreadyGone(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errors":[{"msg":"No organization with key 'my-org'"}]}`))
	}))
	defer srv.Close()

	if err := newTestClient(srv).DeleteOrganization(t.Context(), "my-org"); err != nil {
		t.Errorf("DeleteOrganization() returned %v, want success", err)
	}
}

// Every other failure of a delete reaches the caller.
func TestDeleteOrganizationReportsOtherFailures(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":[{"msg":"Insufficient privileges"}]}`))
	}))
	defer srv.Close()

	err := newTestClient(srv).DeleteOrganization(t.Context(), "my-org")

	if err == nil {
		t.Fatal("DeleteOrganization() succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "Insufficient privileges") {
		t.Errorf("error = %v, want one that mentions the privileges", err)
	}
}
