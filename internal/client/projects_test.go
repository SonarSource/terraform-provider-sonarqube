package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestCreateProject(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/projects/create" {
			t.Errorf("request = %s %s, want POST /api/projects/create", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string]string{
			"organization": "my-org", "project": "my-project", "name": "My Project",
		} {
			if got := r.PostForm.Get(name); got != want {
				t.Errorf("%s = %q, want %q", name, got, want)
			}
		}
		if len(r.PostForm) != 3 {
			t.Errorf("form = %v, want only the three project fields", r.PostForm)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := newTestClient(srv).CreateProject(t.Context(), "my-org", "my-project", "My Project"); err != nil {
		t.Fatal(err)
	}
}

const projectOrganizationAnswer = `[{"id":"organization-id","uuidV4":"00000000-0000-4000-8000-000000000001","key":"my-org","name":"My Organization"}]`

// projectServer answers the organization read and the project list. It hands
// the project list request to check.
func projectServer(t *testing.T, projects string, check func(*http.Request)) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/organizations/organizations":
			w.Write([]byte(projectOrganizationAnswer))
		case "/projects/projects":
			if check != nil {
				check(r)
			}
			w.Write([]byte(projects))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestGetProject(t *testing.T) {
	t.Parallel()

	srv := projectServer(t,
		`{"projects":[`+
			`{"id":"u0","key":"another","name":"Another"},`+
			`{"id":"u1","legacyId":"project-legacy-id","key":"my-project","name":"My Project","visibility":"private",`+
			`"organizationId":"00000000-0000-4000-8000-000000000001"}]}`,
		func(r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("method = %s, want GET", r.Method)
			}
			if got := r.URL.Query().Get("keys"); got != "my-project" {
				t.Errorf("keys = %q, want my-project", got)
			}
			if got, want := r.URL.Query().Get("organizationIds"), "00000000-0000-4000-8000-000000000001"; got != want {
				t.Errorf("organizationIds = %q, want %q", got, want)
			}
		})
	defer srv.Close()

	project, err := newTestClient(srv).GetProject(t.Context(), "my-org", "my-project")
	if err != nil {
		t.Fatal(err)
	}
	want := Project{
		ID: "u1", LegacyID: "project-legacy-id", Key: "my-project", Name: "My Project", Visibility: "private",
		OrganizationID: "00000000-0000-4000-8000-000000000001",
	}
	if *project != want {
		t.Errorf("project = %+v, want %+v", *project, want)
	}
}

func TestGetProjectAbsent(t *testing.T) {
	t.Parallel()

	srv := projectServer(t, `{"projects":[]}`, nil)
	defer srv.Close()

	_, err := newTestClient(srv).GetProject(t.Context(), "my-org", "my-project")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestGetProjectOrganizationAbsent(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/organizations/organizations" {
			t.Errorf("path = %q, want no project request", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Organization with key my-org is not found"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetProject(t.Context(), "my-org", "my-project")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestGetProjectReportsFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/organizations/organizations" {
			w.Write([]byte(projectOrganizationAnswer))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Insufficient privileges"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetProject(t.Context(), "my-org", "my-project")
	if err == nil || !strings.Contains(err.Error(), "Insufficient privileges") {
		t.Errorf("error = %v, want the API failure", err)
	}
}

func TestDeleteProjectAlreadyGone(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects/delete" {
			t.Errorf("path = %q, want /api/projects/delete", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("project"); got != "my-project" {
			t.Errorf("project = %q, want my-project", got)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if err := newTestClient(srv).DeleteProject(t.Context(), "my-project"); err != nil {
		t.Errorf("DeleteProject() returned %v, want success", err)
	}
}

// branchListServer answers the branch list of my-project with answer.
func branchListServer(t *testing.T, answer string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/project_branches/list" {
			t.Errorf("request = %s %s, want GET /api/project_branches/list", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("project"); got != "my-project" {
			t.Errorf("project = %q, want my-project", got)
		}
		w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGetMainBranch(t *testing.T) {
	t.Parallel()

	srv := branchListServer(t, `{"branches":[{"name":"feature","isMain":false},{"name":"main","isMain":true}]}`)

	got, err := newTestClient(srv).GetMainBranch(t.Context(), "my-project")
	if err != nil || got != "main" {
		t.Errorf("GetMainBranch() = %q, %v, want main", got, err)
	}
}

func TestGetMainBranchRefusesAnswersWithoutAMainBranch(t *testing.T) {
	t.Parallel()

	for name, answer := range map[string]string{
		"only other branches": `{"branches":[{"name":"feature","isMain":false}]}`,
		"no branches":         `{"branches":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := newTestClient(branchListServer(t, answer)).GetMainBranch(t.Context(), "my-project")
			if err == nil || !strings.Contains(err.Error(), "has no main branch") {
				t.Errorf("error = %v, want one about the missing main branch", err)
			}
		})
	}
}

func TestGetMainBranchReportsFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errors":[{"msg":"Project 'my-project' not found"}]}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetMainBranch(t.Context(), "my-project")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestRenameMainBranch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/project_branches/rename" {
			t.Errorf("request = %s %s, want POST /api/project_branches/rename", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("project"); got != "my-project" {
			t.Errorf("project = %q, want my-project", got)
		}
		if got := r.PostForm.Get("name"); got != "release/1.x" {
			t.Errorf("name = %q, want release/1.x", got)
		}
		if len(r.PostForm) != 2 {
			t.Errorf("form = %v, want only the project and the name", r.PostForm)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := newTestClient(srv).RenameMainBranch(t.Context(), "my-project", "release/1.x"); err != nil {
		t.Fatal(err)
	}
}

func TestRenameMainBranchReportsFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errors":[{"msg":"a branch with name \"main\" already exists in the project"}]}`))
	}))
	defer srv.Close()

	err := newTestClient(srv).RenameMainBranch(t.Context(), "my-project", "main")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %v, want the API failure", err)
	}
}

func TestSearchProjectKeysReadsEveryPage(t *testing.T) {
	t.Parallel()
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/api/projects/search" || query.Get("organization") != "my-org" ||
			query.Get("q") != "tf-acc-test-" || query.Get("ps") != "500" {
			t.Errorf("unexpected project search: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		pages = append(pages, query.Get("p"))
		if query.Get("p") == "1" {
			w.Write([]byte(`{"components":[{"key":"tf-acc-test-1"}],"paging":{"total":2}}`))
			return
		}
		w.Write([]byte(`{"components":[{"key":"my-org_tf-acc-test-2"}],"paging":{"total":2}}`))
	}))
	t.Cleanup(srv.Close)

	keys, err := newTestClient(srv).SearchProjectKeys(t.Context(), "my-org", "tf-acc-test-")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(keys, ",") != "tf-acc-test-1,my-org_tf-acc-test-2" || strings.Join(pages, ",") != "1,2" {
		t.Errorf("keys=%v, pages=%v", keys, pages)
	}
}

func TestFindProjects(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var filters []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/projects/projects" || query.Get("organizationIds") != "org" || len(query["ids"]) > 1 {
			t.Errorf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		mu.Lock()
		filters = append(filters, query.Get("ids"))
		mu.Unlock()
		var projects []string
		for _, id := range strings.Split(query.Get("ids"), ",") {
			if id != "missing" {
				projects = append(projects, `{"id":"`+id+`","key":"key-`+id+`","organizationId":"org"}`)
			}
		}
		w.Write([]byte(`{"projects":[` + strings.Join(projects, ",") + `],"page":{"total":` + strconv.Itoa(len(projects)) + `}}`))
	}))
	defer srv.Close()

	ids := []string{"missing"}
	for i := range projectFilterChunk {
		ids = append(ids, "p"+strconv.Itoa(i))
	}
	projects, err := newTestClient(srv).FindProjectsByID(t.Context(), "org", ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != projectFilterChunk || len(filters) != 2 || !strings.HasPrefix(filters[0], "missing,p0,p1") {
		t.Fatalf("projects=%d, filters=%v", len(projects), filters)
	}
}

func TestFindProjectsRefusesInvalidAnswers(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"missing page":       `{"projects":[]}`,
		"missing collection": `{"page":{"total":0}}`,
		"negative total":     `{"projects":[],"page":{"total":-1}}`,
		"other organization": `{"projects":[{"id":"p","key":"k","organizationId":"other"}],"page":{"total":1}}`,
		"missing key":        `{"projects":[{"id":"p","organizationId":"org"}],"page":{"total":1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(body)) }))
			defer srv.Close()
			if _, err := newTestClient(srv).FindProjectsByKey(t.Context(), "org", []string{"k"}); err == nil {
				t.Fatal("invalid answer was accepted")
			}
		})
	}
}

func TestFilterChunks(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("k", 400)
	for _, tc := range []struct {
		name   string
		values []string
		want   []int
	}{
		{"empty", nil, nil},
		{"count limit", slices.Repeat([]string{"p"}, projectFilterChunk+1), []int{projectFilterChunk, 1}},
		{"byte limit", slices.Repeat([]string{long}, 20), []int{9, 9, 2}},
		{"value above the byte limit", []string{strings.Repeat("k", projectFilterBytes+1), "p"}, []int{1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []int
			for _, chunk := range filterChunks(tc.values) {
				if len(chunk) > projectFilterBytes && strings.Contains(chunk, ",") {
					t.Errorf("chunk of %d bytes has more than one value", len(chunk))
				}
				got = append(got, len(strings.Split(chunk, ",")))
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("chunk sizes = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUpdateProjectKey(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/projects/update_key" {
			t.Errorf("request = %s %s, want POST /api/projects/update_key", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if from, to := r.PostForm.Get("from"), r.PostForm.Get("to"); from != "old-key" || to != "new-key" {
			t.Errorf("from = %q, to = %q", from, to)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := newTestClient(srv).UpdateProjectKey(t.Context(), "old-key", "new-key"); err != nil {
		t.Fatal(err)
	}
}
