package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const gitlabConfigurationAnswer = `{
  "id": "configuration-id",
  "organizationId": "00000000-0000-4000-8000-000000000001",
  "apiUrl": "https://gitlab.example.com/api/v4",
  "webUrl": "https://gitlab.example.com"
}`

func TestCreateGitLabConfiguration(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(gitlabConfigurationAnswer))
	defer srv.Close()

	configuration, err := newTestClient(srv).CreateGitLabConfiguration(t.Context(), GitLabConfigurationRequest{
		OrganizationID:      "00000000-0000-4000-8000-000000000001",
		APIURL:              "https://gitlab.example.com/api/v4",
		WebURL:              "https://gitlab.example.com",
		PersonalAccessToken: "test-token",
		Group:               "my-group",
	})
	if err != nil {
		t.Fatalf("CreateGitLabConfiguration() returned %v", err)
	}

	if rec.method != http.MethodPost || rec.path != "/gitlab-configurations" {
		t.Errorf("request = %s %s, want POST /gitlab-configurations", rec.method, rec.path)
	}
	for field, want := range map[string]string{
		"organizationId":      "00000000-0000-4000-8000-000000000001",
		"personalAccessToken": "test-token",
		"group":               "my-group",
	} {
		if got := rec.body[field]; got != want {
			t.Errorf("body[%q] = %v, want %q", field, got, want)
		}
	}
	if got, want := configuration.ID, "configuration-id"; got != want {
		t.Errorf("ID = %q, want %q", got, want)
	}
}

func TestFindGitLabConfiguration(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(`{"gitlabConfigurations":[` + gitlabConfigurationAnswer + `]}`))
	defer srv.Close()

	configuration, err := newTestClient(srv).FindGitLabConfiguration(t.Context(), "00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatalf("FindGitLabConfiguration() returned %v", err)
	}
	if want := "organizationId=00000000-0000-4000-8000-000000000001"; rec.query != want {
		t.Errorf("query = %q, want %q", rec.query, want)
	}
	if got, want := configuration.WebURL, "https://gitlab.example.com"; got != want {
		t.Errorf("WebURL = %q, want %q", got, want)
	}
}

// The search answers 200 with an empty collection for an organization that
// has no configuration.
func TestFindGitLabConfigurationOfAnOrganizationWithoutOne(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer((&recorder{}).serve(`{"gitlabConfigurations":[]}`))
	defer srv.Close()

	_, err := newTestClient(srv).FindGitLabConfiguration(t.Context(), "00000000-0000-4000-8000-000000000001")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("FindGitLabConfiguration() returned %v, want ErrNotFound", err)
	}
}

// The change call accepts a JSON merge patch only, and a field that holds
// nothing stays out of the body.
func TestUpdateGitLabConfiguration(t *testing.T) {
	t.Parallel()

	var contentType string
	rec := &recorder{}
	serve := rec.serve(gitlabConfigurationAnswer)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		serve(w, r)
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).UpdateGitLabConfiguration(t.Context(), "configuration-id",
		GitLabConfigurationPatch{WebURL: "https://gitlab.example.com"}); err != nil {
		t.Fatalf("UpdateGitLabConfiguration() returned %v", err)
	}

	if rec.method != http.MethodPatch || rec.path != "/gitlab-configurations/configuration-id" {
		t.Errorf("request = %s %s, want PATCH /gitlab-configurations/configuration-id", rec.method, rec.path)
	}
	if want := "application/merge-patch+json"; contentType != want {
		t.Errorf("Content-Type = %q, want %q", contentType, want)
	}
	if _, present := rec.body["personalAccessToken"]; present {
		t.Errorf("body = %v, want no personalAccessToken", rec.body)
	}
}

// A refusal names the field at fault, so that the provider can show the
// error on the matching attribute.
func TestGitLabConfigurationRefusalNamesTheField(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"apiUrl: must not point to a non-public network address","relatedField":"apiUrl"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).CreateGitLabConfiguration(t.Context(), GitLabConfigurationRequest{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("CreateGitLabConfiguration() returned %v, want an APIError", err)
	}
	if got, want := apiErr.RelatedField, "apiUrl"; got != want {
		t.Errorf("RelatedField = %q, want %q", got, want)
	}
}

func TestCreateGitLabSelfManagedBinding(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(bindingAnswer))
	defer srv.Close()

	if _, err := newTestClient(srv).CreateGitLabSelfManagedBinding(t.Context(), CreateGitLabSelfManagedBindingRequest{
		OrganizationID: "organization-id",
		DevOpsPlatform: PlatformGitLab,
		DopFlavor:      FlavorGitLabSelfManaged,
		AccessToken:    "test-token",
		Group:          "my-group",
	}); err != nil {
		t.Fatalf("CreateGitLabSelfManagedBinding() returned %v", err)
	}

	if rec.method != http.MethodPost || rec.path != "/dop-translation/organization-bindings" {
		t.Errorf("request = %s %s, want POST /dop-translation/organization-bindings", rec.method, rec.path)
	}
	// The bind call takes the path of the group in the field that a read
	// fills with the numeric identifier.
	for field, want := range map[string]string{
		"devOpsPlatform":               PlatformGitLab,
		"dopFlavor":                    FlavorGitLabSelfManaged,
		"accessToken":                  "test-token",
		"devOpsPlatformOrganizationId": "my-group",
	} {
		if got := rec.body[field]; got != want {
			t.Errorf("body[%q] = %v, want %q", field, got, want)
		}
	}
	if _, present := rec.body["installationId"]; present {
		t.Errorf("body = %v, want no installationId", rec.body)
	}
}

func TestUpdateGitLabSelfManagedBinding(t *testing.T) {
	t.Parallel()

	var contentType string
	rec := &recorder{}
	serve := rec.serve(bindingAnswer)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		serve(w, r)
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).UpdateGitLabSelfManagedBinding(t.Context(), "binding-id",
		PatchGitLabSelfManagedBindingRequest{AccessToken: "test-token"}); err != nil {
		t.Fatalf("UpdateGitLabSelfManagedBinding() returned %v", err)
	}

	if want := "application/merge-patch+json"; contentType != want {
		t.Errorf("Content-Type = %q, want %q", contentType, want)
	}
	if len(rec.body) != 1 || rec.body["accessToken"] != "test-token" {
		t.Errorf("body = %v, want only accessToken", rec.body)
	}
}

// A failed call returns the error and no value, for each call of this
// domain.
func TestGitLabCallsReturnTheError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message":"the instance is unwell"}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	ctx := t.Context()

	for name, call := range map[string]func() (any, error){
		"CreateGitLabConfiguration": func() (any, error) {
			return c.CreateGitLabConfiguration(ctx, GitLabConfigurationRequest{})
		},
		"FindGitLabConfiguration": func() (any, error) { return c.FindGitLabConfiguration(ctx, "uuid") },
		"UpdateGitLabConfiguration": func() (any, error) {
			return c.UpdateGitLabConfiguration(ctx, "id", GitLabConfigurationPatch{})
		},
		"CreateGitLabSelfManagedBinding": func() (any, error) {
			return c.CreateGitLabSelfManagedBinding(ctx, CreateGitLabSelfManagedBindingRequest{})
		},
		"UpdateGitLabSelfManagedBinding": func() (any, error) {
			return c.UpdateGitLabSelfManagedBinding(ctx, "id", PatchGitLabSelfManagedBindingRequest{})
		},
	} {
		got, err := call()
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
			t.Errorf("%s() returned error %v, want the 500", name, err)
		}
		if got == nil {
			continue
		}
		// A typed nil pointer in the interface is still no value.
		switch v := got.(type) {
		case *GitLabConfiguration:
			if v != nil {
				t.Errorf("%s() returned %v with the error", name, v)
			}
		case *OrganizationBinding:
			if v != nil {
				t.Errorf("%s() returned %v with the error", name, v)
			}
		}
	}
}

// The API allows one configuration for each organization. More than one is
// an answer that the provider cannot use.
func TestFindGitLabConfigurationRefusesTwo(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer((&recorder{}).serve(
		`{"gitlabConfigurations":[` + gitlabConfigurationAnswer + `,` + gitlabConfigurationAnswer + `]}`))
	defer srv.Close()

	if _, err := newTestClient(srv).FindGitLabConfiguration(t.Context(), "uuid"); err == nil {
		t.Error("FindGitLabConfiguration() accepted two configurations")
	}
}
