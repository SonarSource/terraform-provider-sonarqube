package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const organizationAnswer = `{
  "organizations": [
    {
      "key": "my-org",
      "name": "My Organization",
      "description": "Managed by Terraform",
      "url": "https://example.com",
      "avatar": "https://example.com/avatar.png"
    }
  ]
}`

func TestGetOrganization(t *testing.T) {
	t.Parallel()

	var gotPath, gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("organizations")
		w.Write([]byte(organizationAnswer))
	}))
	defer srv.Close()

	org, err := newTestClient(srv).GetOrganization(t.Context(), "my-org")
	if err != nil {
		t.Fatalf("GetOrganization() returned %v", err)
	}

	if want := "/api/organizations/search"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if want := "my-org"; gotQuery != want {
		t.Errorf("organizations parameter = %q, want %q", gotQuery, want)
	}

	if got, want := org.Name, "My Organization"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	if got, want := org.Avatar, "https://example.com/avatar.png"; got != want {
		t.Errorf("Avatar = %q, want %q", got, want)
	}
}

// TestGetOrganizationEmptyList pins the behaviour that matters most here. The
// web service answers a search that finds nothing, and a search that the token
// may not make, with status 200 and an empty list. Both must become
// ErrNotFound rather than an organization with empty fields.
func TestGetOrganizationEmptyList(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"organizations":[]}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetOrganization(t.Context(), "my-org")

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetOrganization() returned %v, want an error that matches ErrNotFound", err)
	}
}

// TestGetOrganizationIgnoresAnotherKey guards against a future change of the
// web service that answers with more than the organization that was asked for.
func TestGetOrganizationIgnoresAnotherKey(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"organizations":[{"key":"another-org","name":"Another"}]}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetOrganization(t.Context(), "my-org")

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetOrganization() returned %v, want an error that matches ErrNotFound", err)
	}
}
