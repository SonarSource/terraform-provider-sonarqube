package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

const projectOrganizationAnswer = `[{"id":"AZcwYwExlol79EFABiuM","uuidV4":"3ddd1f8f-2ab4-443f-a3be-a19ca418ca75","key":"my-org","name":"My Organization"}]`

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
			`{"id":"u1","key":"my-project","name":"My Project","visibility":"private",`+
			`"organizationId":"3ddd1f8f-2ab4-443f-a3be-a19ca418ca75"}]}`,
		func(r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("method = %s, want GET", r.Method)
			}
			if got := r.URL.Query().Get("keys"); got != "my-project" {
				t.Errorf("keys = %q, want my-project", got)
			}
			if got, want := r.URL.Query().Get("organizationIds"), "3ddd1f8f-2ab4-443f-a3be-a19ca418ca75"; got != want {
				t.Errorf("organizationIds = %q, want %q", got, want)
			}
		})
	defer srv.Close()

	project, err := newTestClient(srv).GetProject(t.Context(), "my-org", "my-project")
	if err != nil {
		t.Fatal(err)
	}
	want := Project{
		ID: "u1", Key: "my-project", Name: "My Project", Visibility: "private",
		OrganizationID: "3ddd1f8f-2ab4-443f-a3be-a19ca418ca75",
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
