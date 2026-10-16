package acctest

import (
	"cmp"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
)

// sharedOrganization is what its server answers. failing names a path that
// answers 403.
type sharedOrganization struct {
	plan, permissions, failing string
}

// server answers the reads of checkSharedOrganization with the plan and the
// direct permissions of the token user.
func (o sharedOrganization) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == o.failing {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/organizations/organizations":
			fmt.Fprint(w, `[{"key":"shared","uuidV4":"org-uuid"}]`)
		case "/billing/current-plan":
			fmt.Fprintf(w, `{"planKey":%q}`, o.plan)
		case "/api/users/current":
			fmt.Fprint(w, `{"login":"token-user"}`)
		case "/api/permissions/users":
			fmt.Fprintf(w, `{"users":[{"login":"token-user","permissions":%s}],"paging":{"total":1}}`, o.permissions)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (o sharedOrganization) client(t *testing.T) *client.Client {
	t.Helper()
	srv := o.server(t)
	return client.New(client.Config{URL: srv.URL, APIURL: srv.URL, Token: "test-token",
		Product: client.ProductCloud, HTTPClient: srv.Client()})
}

func TestCheckSharedOrganization(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		organization sharedOrganization
		key          string
		wantErr      string
	}{
		"enterprise with a direct admin": {organization: sharedOrganization{plan: "Enterprise", permissions: `["admin","scan"]`}},
		"team with a direct admin":       {organization: sharedOrganization{plan: "TEAM", permissions: `["admin"]`}},
		"free plan": {organization: sharedOrganization{plan: "FREE_V2", permissions: `["admin"]`},
			wantErr: `plan "free_v2"`},
		"no direct admin": {organization: sharedOrganization{plan: "Enterprise", permissions: `["scan"]`},
			wantErr: "no direct admin grant"},
		"no direct permission": {organization: sharedOrganization{plan: "Enterprise", permissions: `[]`},
			wantErr: "no direct admin grant"},
		"organization not readable": {organization: sharedOrganization{failing: "/organizations/organizations"},
			wantErr: "cannot read the organization shared"},
		"plan not readable": {organization: sharedOrganization{failing: "/billing/current-plan"},
			wantErr: "cannot read the plan"},
		"token user not readable": {organization: sharedOrganization{plan: "Enterprise", failing: "/api/users/current"},
			wantErr: "cannot read the token user"},
		"permissions not readable": {organization: sharedOrganization{plan: "Enterprise", failing: "/api/permissions/users"},
			wantErr: "cannot read the permissions of token-user"},
		"key with the test prefix": {organization: sharedOrganization{plan: "Enterprise", permissions: `["admin"]`},
			key: KeyPrefix + "shared", wantErr: "starts with " + KeyPrefix},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			key := cmp.Or(test.key, "shared")
			err := checkSharedOrganization(t.Context(), test.organization.client(t), key)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("err = %v, want one that holds %q", err, test.wantErr)
			}
		})
	}
}

// setSharedOrganizationEnvironment points the acceptance test settings at srv.
// The server is local, so PreCheckDestructive allows it.
func setSharedOrganizationEnvironment(t *testing.T, srv *httptest.Server, organization string) {
	t.Helper()
	t.Setenv(provider.EnvURL, srv.URL)
	t.Setenv(provider.EnvAPIURL, srv.URL)
	t.Setenv(provider.EnvToken, "test-token")
	t.Setenv(EnvTestOrganization, organization)
}

func TestPreCheckSharedOrganizationReturnsTheKey(t *testing.T) {
	srv := sharedOrganization{plan: "Enterprise", permissions: `["admin"]`}.server(t)
	setSharedOrganizationEnvironment(t, srv, "shared")

	if got := PreCheckSharedOrganization(t); got != "shared" {
		t.Errorf("organization = %q, want shared", got)
	}
}

func TestPreCheckSharedOrganizationSkipsWithoutTheVariable(t *testing.T) {
	srv := sharedOrganization{}.server(t)
	setSharedOrganizationEnvironment(t, srv, "")

	reached := false
	t.Run("precheck", func(t *testing.T) {
		PreCheckSharedOrganization(t)
		reached = true
	})
	if reached {
		t.Error("the precheck did not skip without the organization")
	}
}
