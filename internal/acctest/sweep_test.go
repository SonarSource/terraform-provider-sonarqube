package acctest

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
)

const sweepOrganizationUUID = "00000000-0000-4000-8000-000000000001"

// sweepServer answers the reads of the sweepers with one item that has the
// prefix and one that does not, and records each deletion.
func sweepServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		r.ParseForm()
		switch {
		case r.URL.Path == "/api/organizations/search":
			fmt.Fprint(w, `{"organizations":[{"key":"tf-acc-test-1"},{"key":"customer-org"}],"paging":{"total":2}}`)
		case r.URL.Path == "/api/organizations/delete":
			deleted = append(deleted, "organization "+r.Form.Get("organization"))
		case r.URL.Path == "/organizations/organizations":
			fmt.Fprintf(w, `[{"key":"shared","uuidV4":%q,"defaultQualityGateUuid":"default-gate"}]`, sweepOrganizationUUID)
		case r.URL.Path == "/api/projects/search":
			fmt.Fprint(w, `{"components":[{"key":"tf-acc-test-1"},{"key":"customer_tf-acc-test-1"}],"paging":{"total":2}}`)
		case r.URL.Path == "/api/projects/delete":
			deleted = append(deleted, "project "+r.Form.Get("project"))
		case r.URL.Path == "/users/groups" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"groups":[{"id":"g1","name":"tf-acc-test-1"},{"id":"g2","name":"Owners"}],"page":{"total":2}}`)
		case strings.HasPrefix(r.URL.Path, "/users/groups/") && r.Method == http.MethodDelete:
			deleted = append(deleted, "group "+strings.TrimPrefix(r.URL.Path, "/users/groups/"))
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/permissions/search_templates":
			fmt.Fprint(w, `{"permissionTemplates":[{"id":"t1","name":"tf-acc-test-1"},{"id":"t2","name":"Default template"},`+
				`{"id":"t3","name":"tf-acc-test-default"}],"defaultTemplates":[{"templateId":"t3","qualifier":"TRK"}]}`)
		case r.URL.Path == "/api/permissions/delete_template":
			deleted = append(deleted, "template "+r.Form.Get("templateId"))
		case r.URL.Path == "/api/qualityprofiles/search":
			fmt.Fprint(w, `{"profiles":[{"key":"p1","name":"tf-acc-test-1"},{"key":"p2","name":"Sonar way core","isBuiltIn":true},`+
				`{"key":"p3","name":"customer profile"},{"key":"p4","name":"tf-acc-test-default","isDefault":true,"parentKey":"p5"},`+
				`{"key":"p5","name":"tf-acc-test-parent-of-default"},`+
				`{"key":"p6","name":"tf-acc-test-parent"},{"key":"p7","name":"customer child","parentKey":"p6"},`+
				`{"key":"p8","name":"tf-acc-test-grandparent"},{"key":"p9","name":"tf-acc-test-middle","parentKey":"p8"},`+
				`{"key":"p10","name":"customer grandchild","parentKey":"p9"}]}`)
		case r.URL.Path == "/api/qualityprofiles/delete":
			deleted = append(deleted, "profile "+r.Form.Get("key"))
		case r.URL.Path == "/quality-gates/quality-gates" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"qualityGates":[{"id":"q1","name":"tf-acc-test-1"},{"id":"q2","name":"Sonar way","builtIn":true},`+
				`{"id":"q3","name":"customer gate"},{"id":"default-gate","name":"tf-acc-test-default"}],"page":{"total":4}}`)
		case strings.HasPrefix(r.URL.Path, "/quality-gates/quality-gates/") && r.Method == http.MethodDelete:
			deleted = append(deleted, "gate "+strings.TrimPrefix(r.URL.Path, "/quality-gates/quality-gates/"))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(deleted)
	}
}

func setSweepEnvironment(t *testing.T, srv *httptest.Server) {
	t.Helper()
	t.Setenv(provider.EnvURL, srv.URL)
	t.Setenv(provider.EnvAPIURL, srv.URL)
	t.Setenv(provider.EnvToken, "test-token")
	t.Setenv(EnvTestOrganization, "shared")
	t.Setenv(EnvAllowedHosts, "")
}

// The defaults have the prefix too, but the server cannot delete them. A key
// that holds the prefix but does not start with it stays. A quality profile
// with the prefix stays when a default profile or a profile without the
// prefix extends it, also through a profile between them, because its delete
// deletes its descendants.
func TestSweepersDeleteOnlyWhatHasThePrefix(t *testing.T) {
	srv, deleted := sweepServer(t)
	setSweepEnvironment(t, srv)

	for _, s := range sweepers {
		if err := s.run(t.Context()); err != nil {
			t.Errorf("%s: %v", s.name, err)
		}
	}

	got := deleted()
	slices.Sort(got)
	want := []string{"gate q1", "group g1", "organization tf-acc-test-1", "profile p1", "project tf-acc-test-1", "template t1"}
	if !slices.Equal(got, want) {
		t.Errorf("deleted %v, want %v", got, want)
	}
}

func TestSweepersRefuseAnUnsafeTarget(t *testing.T) {
	srv, deleted := sweepServer(t)
	setSweepEnvironment(t, srv)
	t.Setenv(provider.EnvURL, "https://sonarcloud.io")

	if err := sweeperNamed(t, "sonarqube_cloud_organization").run(t.Context()); err == nil || !strings.Contains(err.Error(), "production") {
		t.Errorf("err = %v, want the production refusal", err)
	}
	if got := deleted(); len(got) != 0 {
		t.Errorf("deleted %v", got)
	}
}

func TestSweepersRefuseAnUnsafeAPIAddress(t *testing.T) {
	srv, deleted := sweepServer(t)
	setSweepEnvironment(t, srv)
	t.Setenv(provider.EnvAPIURL, "https://api.sonarcloud.io")

	for _, s := range sweepers {
		if err := s.run(t.Context()); err == nil || !strings.Contains(err.Error(), provider.EnvAPIURL) {
			t.Errorf("%s: err = %v, want the refusal of the v2 address", s.name, err)
		}
	}
	if got := deleted(); len(got) != 0 {
		t.Errorf("deleted %v", got)
	}
}

func TestOrganizationSweeperKeepsTheSharedOrganization(t *testing.T) {
	srv, deleted := sweepServer(t)
	setSweepEnvironment(t, srv)
	t.Setenv(EnvTestOrganization, "tf-acc-test-1")

	if err := sweeperNamed(t, "sonarqube_cloud_organization").run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := deleted(); len(got) != 0 {
		t.Errorf("deleted %v, want nothing", got)
	}
}

func TestSweepersNeedTheSharedOrganization(t *testing.T) {
	srv, _ := sweepServer(t)
	setSweepEnvironment(t, srv)
	t.Setenv(EnvTestOrganization, "")

	if err := sweeperNamed(t, "sonarqube_project").run(t.Context()); err == nil || !strings.Contains(err.Error(), EnvTestOrganization) {
		t.Errorf("err = %v, want a message that names %s", err, EnvTestOrganization)
	}
}

func sweeperNamed(t *testing.T, name string) sweeper {
	t.Helper()
	i := slices.IndexFunc(sweepers, func(s sweeper) bool { return s.name == name })
	if i < 0 {
		t.Fatalf("no sweeper %s", name)
	}
	return sweepers[i]
}

func TestAncestorsOfWalksUpTheParents(t *testing.T) {
	t.Parallel()
	got := ancestorsOf([]client.QualityProfile{
		{Key: "root"}, {Key: "middle", ParentKey: "root"}, {Key: "kept", ParentKey: "middle", IsDefault: true},
		{Key: "other", ParentKey: "root"}, {Key: "unrelated"},
	}, func(profile client.QualityProfile) bool { return profile.IsDefault })
	if want := map[string]bool{"middle": true, "root": true}; !maps.Equal(got, want) {
		t.Errorf("ancestors = %v, want %v", got, want)
	}
}
