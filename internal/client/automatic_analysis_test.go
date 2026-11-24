package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// settingsServer answers the settings read of my-project with body.
func settingsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/api/settings/values" ||
			query.Get("component") != "my-project" || query.Get("keys") != "sonar.autoscan.enabled" {
			t.Errorf("request = %s %s", r.Method, r.URL)
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGetProjectAutomaticAnalysis(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{name: "project value", body: `{"settings":[{"key":"sonar.autoscan.enabled","value":"false","parentValue":"true"}]}`, want: false},
		{name: "inherited", body: `{"settings":[{"key":"sonar.autoscan.enabled","value":"true","inherited":true}]}`, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := newTestClient(settingsServer(t, tc.body)).GetProjectAutomaticAnalysis(t.Context(), "my-project")
			if err != nil || got != tc.want {
				t.Errorf("enabled = %t, %v, want %t", got, err, tc.want)
			}
		})
	}
}

func TestGetProjectAutomaticAnalysisBadAnswer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, body, want string }{
		{name: "missing", body: `{"settings":[]}`, want: "do not contain"},
		{name: "not a boolean", body: `{"settings":[{"key":"sonar.autoscan.enabled","value":"maybe"}]}`, want: "not a boolean"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := newTestClient(settingsServer(t, tc.body)).GetProjectAutomaticAnalysis(t.Context(), "my-project")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestGetProjectAutomaticAnalysisNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errors":[{"msg":"Component key 'my-project' not found"}]}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetProjectAutomaticAnalysis(t.Context(), "my-project")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestSetProjectAutomaticAnalysis(t *testing.T) {
	t.Parallel()

	for _, enabled := range []bool{true, false} {
		var form string
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/api/autoscan/activation" {
				t.Errorf("request = %s %s", r.Method, r.URL.Path)
			}
			_ = r.ParseForm()
			form = r.PostForm.Encode()
		}))
		err := newTestClient(srv).SetProjectAutomaticAnalysis(t.Context(), "my-project", enabled)
		srv.Close()
		want := "enable=" + strconv.FormatBool(enabled) + "&projectKey=my-project"
		if err != nil || form != want {
			t.Errorf("enabled %t: form %q, %v, want %q", enabled, form, err, want)
		}
	}
}

func TestSetProjectAutomaticAnalysisRefused(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errors":[{"msg":"Project with key 'my-project' has no ALM binding"}]}`))
	}))
	defer srv.Close()

	err := newTestClient(srv).SetProjectAutomaticAnalysis(t.Context(), "my-project", true)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest || !strings.Contains(err.Error(), "no ALM binding") {
		t.Errorf("error = %v", err)
	}
}
