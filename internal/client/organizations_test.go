package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
