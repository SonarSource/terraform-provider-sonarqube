package client

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

var (
	testGroup = PermissionPrincipal{Group: true, Name: "Developers"}
	testUser  = PermissionPrincipal{Name: "alice"}
)

func TestGetPermissionsReadsAllPagesAndMatchesExactName(t *testing.T) {
	t.Parallel()
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/permissions/groups" || q.Get("organization") != "my-org" || q.Get("q") != "Developers" || q.Get("ps") != "100" {
			t.Errorf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		pages = append(pages, q.Get("p"))
		if q.Get("p") == "1" {
			fmt.Fprint(w, `{"paging":{"total":2},"groups":[{"id":"group-1","name":"Developers-old","permissions":["scan"]}]}`)
			return
		}
		fmt.Fprint(w, `{"paging":{"total":2},"groups":[{"id":"group-2","name":"Developers","permissions":["gateadmin"]}]}`)
	}))
	t.Cleanup(srv.Close)
	permissions, err := newTestClient(srv).GetPermissions(t.Context(), PermissionScope{Organization: "my-org"}, testGroup)
	if err != nil || !reflect.DeepEqual(permissions, []string{"gateadmin"}) {
		t.Fatalf("permissions = %v, err = %v", permissions, err)
	}
	if !reflect.DeepEqual(pages, []string{"1", "2"}) {
		t.Errorf("pages = %v", pages)
	}
}

func TestGetPermissionsMatchesUserLogin(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/permissions/users" {
			t.Errorf("path = %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"paging":{"total":2},"users":[{"login":"alice-2","name":"alice","permissions":["admin"]},{"login":"alice","permissions":[]}]}`)
	}))
	t.Cleanup(srv.Close)
	permissions, err := newTestClient(srv).GetPermissions(t.Context(), PermissionScope{Organization: "my-org"}, testUser)
	if err != nil || len(permissions) != 0 {
		t.Fatalf("permissions = %v, err = %v", permissions, err)
	}
}

func TestGetPermissionsOmitsShortQuery(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("q") {
			t.Errorf("q = %q, the server refuses fewer than 3 characters", r.URL.Query().Get("q"))
		}
		fmt.Fprint(w, `{"paging":{"total":1},"groups":[{"name":"QA","permissions":["scan"]}]}`)
	}))
	t.Cleanup(srv.Close)
	permissions, err := newTestClient(srv).GetPermissions(t.Context(), PermissionScope{Organization: "my-org"}, PermissionPrincipal{Group: true, Name: "QA"})
	if err != nil || !reflect.DeepEqual(permissions, []string{"scan"}) {
		t.Fatalf("permissions = %v, err = %v", permissions, err)
	}
}

func TestGetPermissionsErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
		reject error
	}{
		{"missing principal", http.StatusOK, `{"paging":{"total":1},"groups":[{"name":"Other"}]}`, ErrNotFound, ErrOrganizationNotFound},
		{"missing organization", http.StatusNotFound, `{"errors":[{"msg":"No organization with key 'my-org'"}]}`, ErrOrganizationNotFound, ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			t.Cleanup(srv.Close)
			_, err := newTestClient(srv).GetPermissions(t.Context(), PermissionScope{Organization: "my-org"}, testGroup)
			if !errors.Is(err, tc.want) || errors.Is(err, tc.reject) {
				t.Errorf("error = %v", err)
			}
		})
	}
}

func TestGetPermissionsProjectErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		body   string
		want   error
		key    string
		reject []error
	}{
		{"missing project", `{"errors":[{"msg":"Project key 'my-project' not found"}]}`, ErrProjectNotFound, "my-project", []error{ErrNotFound, ErrOrganizationNotFound}},
		{"missing organization", `{"errors":[{"msg":"No organization with key 'my-org'"}]}`, ErrOrganizationNotFound, "my-org", []error{ErrNotFound, ErrProjectNotFound}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("projectKey") != "my-project" {
					t.Errorf("query = %s", r.URL.RawQuery)
				}
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, tc.body)
			}))
			t.Cleanup(srv.Close)
			scope := PermissionScope{Organization: "my-org", ProjectKey: "my-project"}
			_, err := newTestClient(srv).GetPermissions(t.Context(), scope, testGroup)
			assertScopeNotFound(t, err, tc.want, tc.key, tc.reject)
		})
	}
}

func assertScopeNotFound(t *testing.T, err, want error, key string, reject []error) {
	t.Helper()
	var missing *ScopeNotFoundError
	if !errors.Is(err, want) || !errors.As(err, &missing) || missing.Key != key {
		t.Errorf("error = %v, want %v for the key %q", err, want, key)
	}
	for _, other := range reject {
		if errors.Is(err, other) {
			t.Errorf("error = %v also matches %v", err, other)
		}
	}
}

func TestPermissionWriteReportsMissingProject(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"errors":[{"msg":"Project key 'my-project' not found"}]}`)
	}))
	t.Cleanup(srv.Close)
	scope := PermissionScope{Organization: "my-org", ProjectKey: "my-project"}
	err := newTestClient(srv).AddPermission(t.Context(), scope, testGroup, "scan")
	if !errors.Is(err, ErrProjectNotFound) || errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v", err)
	}
}

func TestGetPermissionsRefusesEarlyEmptyPage(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"paging":{"total":2},"groups":[]}`)
	}))
	t.Cleanup(srv.Close)
	_, err := newTestClient(srv).GetPermissions(t.Context(), PermissionScope{Organization: "my-org"}, testGroup)
	if err == nil || !strings.Contains(err.Error(), "empty before") {
		t.Errorf("error = %v", err)
	}
}

func TestPermissionWritesUseFormAndScope(t *testing.T) {
	t.Parallel()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("request = %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("organization") != "my-org" || r.Form.Get("projectKey") != "my-project" || r.Form.Get("permission") != "scan" {
			t.Errorf("form = %v", r.Form)
		}
		calls = append(calls, r.URL.Path+" "+r.Form.Get("groupName")+r.Form.Get("login"))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)
	scope := PermissionScope{Organization: "my-org", ProjectKey: "my-project"}
	for _, call := range []func() error{
		func() error { return c.AddPermission(t.Context(), scope, testGroup, "scan") },
		func() error { return c.RemovePermission(t.Context(), scope, testGroup, "scan") },
		func() error { return c.AddPermission(t.Context(), scope, testUser, "scan") },
		func() error { return c.RemovePermission(t.Context(), scope, testUser, "scan") },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"/api/permissions/add_group Developers", "/api/permissions/remove_group Developers",
		"/api/permissions/add_user alice", "/api/permissions/remove_user alice",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
}

func TestRemoveLastAdminExplainsError(t *testing.T) {
	t.Parallel()
	for _, message := range []string{
		"Last group with permission 'admin'. Permission cannot be removed.",
		"Last user with permission 'admin'. Permission cannot be removed.",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"errors":[{"msg":%q}]}`, message)
		}))
		err := newTestClient(srv).RemovePermission(t.Context(), PermissionScope{Organization: "my-org"}, testGroup, "admin")
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), "last administrator") {
			t.Errorf("error = %v", err)
		}
	}
}

func TestPermissionWriteReportsMissingPrincipal(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	err := newTestClient(srv).AddPermission(t.Context(), PermissionScope{Organization: "my-org"}, testUser, "scan")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), `user "alice"`) {
		t.Errorf("error = %v", err)
	}
}

func TestPermissionScopeParameters(t *testing.T) {
	t.Parallel()
	got := (PermissionScope{Organization: "my-org"}).parameters()
	if !reflect.DeepEqual(got, url.Values{"organization": {"my-org"}}) {
		t.Errorf("parameters = %v", got)
	}
}

func TestCurrentUserLogin(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/users/current" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"login":"alice"}`)
	}))
	t.Cleanup(srv.Close)
	login, err := newTestClient(srv).CurrentUserLogin(t.Context())
	if err != nil || login != "alice" {
		t.Errorf("login = %q, error = %v", login, err)
	}
}
