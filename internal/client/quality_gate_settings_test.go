package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetOrganizationQualityGateSettings(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != qualityGateSettingsPath {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("resourceId"); got != "organization-uuid" {
			t.Errorf("resourceId = %q", got)
		}
		if got := r.URL.Query().Get("resourceType"); got != "ORGANIZATION" {
			t.Errorf("resourceType = %q", got)
		}
		w.Write([]byte(`{"id":"settings-id","ignoreSmallChanges":true}`))
	}))
	defer srv.Close()

	settings, err := newTestClient(srv).GetOrganizationQualityGateSettings(t.Context(), "organization-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if settings.ID != "settings-id" || !settings.IgnoreSmallChanges {
		t.Errorf("settings = %+v", settings)
	}
}

func TestGetOrganizationQualityGateSettingsNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetOrganizationQualityGateSettings(t.Context(), "organization-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestGetOrganizationQualityGateSettingsWithoutID(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ignoreSmallChanges":true}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetOrganizationQualityGateSettings(t.Context(), "organization-uuid")
	if err == nil || !strings.Contains(err.Error(), "no settings identifier") {
		t.Errorf("error = %v, want a missing identifier error", err)
	}
}

func TestUpdateOrganizationQualityGateSettings(t *testing.T) {
	t.Parallel()

	for _, value := range []bool{true, false} {
		t.Run(map[bool]string{true: "on", false: "off"}[value], func(t *testing.T) {
			t.Parallel()
			testUpdateOrganizationQualityGateSettings(t, value)
		})
	}
}

func testUpdateOrganizationQualityGateSettings(t *testing.T, value bool) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != qualityGateSettingsPath+"/settings-id" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var body map[string]bool
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if got, ok := body["ignoreSmallChanges"]; !ok || got != value || len(body) != 1 {
			t.Errorf("body = %v", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := newTestClient(srv).UpdateOrganizationQualityGateSettings(t.Context(), "settings-id", value); err != nil {
		t.Fatal(err)
	}
}
