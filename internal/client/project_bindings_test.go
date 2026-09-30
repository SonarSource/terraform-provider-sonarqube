package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const projectBindingAnswer = `{
  "id": "binding-id",
  "projectId": "project-legacy-id",
  "devOpsPlatform": "github",
  "repositoryId": "1001",
  "slug": "my-github-org/my-repo",
  "url": "https://github.com/my-github-org/my-repo"
}`

// theProject is what GetProject returns for the project of these tests.
var theProject = &Project{ID: "project-uuid", LegacyID: "project-legacy-id", Key: "my-project"}

func TestCreateProjectBinding(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(projectBindingAnswer))
	defer srv.Close()

	binding, err := newTestClient(srv).CreateProjectBinding(t.Context(), theProject, "my-github-org/my-repo")
	if err != nil {
		t.Fatalf("CreateProjectBinding() returned %v", err)
	}

	if rec.method != http.MethodPost {
		t.Errorf("method = %q, want %q", rec.method, http.MethodPost)
	}
	if want := "/dop-translation/project-bindings"; rec.path != want {
		t.Errorf("path = %q, want %q", rec.path, want)
	}
	// The legacy identifier, not the UUID: the bindings API refuses the UUID.
	if got, want := rec.body["projectId"], "project-legacy-id"; got != want {
		t.Errorf("projectId = %v, want %q", got, want)
	}
	// The slug goes out in repositoryId, and the number comes back in it.
	if got, want := rec.body["repositoryId"], "my-github-org/my-repo"; got != want {
		t.Errorf("repositoryId = %v, want %q", got, want)
	}
	if len(rec.body) != 2 {
		t.Errorf("body = %v, want only projectId and repositoryId", rec.body)
	}

	if binding.ID != "binding-id" || binding.RepositoryID != "1001" || binding.Slug != "my-github-org/my-repo" {
		t.Errorf("binding = %+v, want the fields of the answer", binding)
	}
}

func TestCreateProjectBindingReportsFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"The repository is already bound"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).CreateProjectBinding(t.Context(), theProject, "my-github-org/my-repo")
	if err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Errorf("error = %v, want the API failure", err)
	}
}

func TestFindProjectBinding(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(`{"bindings":[` + projectBindingAnswer + `],"page":{"pageIndex":1,"pageSize":50,"total":1}}`))
	defer srv.Close()

	binding, err := newTestClient(srv).FindProjectBinding(t.Context(), theProject)
	if err != nil {
		t.Fatalf("FindProjectBinding() returned %v", err)
	}

	if rec.method != http.MethodGet {
		t.Errorf("method = %q, want %q", rec.method, http.MethodGet)
	}
	if want := "/dop-translation/project-bindings"; rec.path != want {
		t.Errorf("path = %q, want %q", rec.path, want)
	}
	if want := "projectId=project-legacy-id"; rec.query != want {
		t.Errorf("query = %q, want %q", rec.query, want)
	}
	if binding.ID != "binding-id" {
		t.Errorf("ID = %q, want binding-id", binding.ID)
	}
}

// The server answers a project that is not bound with a 200 and an empty
// collection.
func TestFindProjectBindingOfAProjectThatIsNotBound(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(`{"bindings":[],"page":{"pageIndex":1,"pageSize":50,"total":0}}`))
	defer srv.Close()

	_, err := newTestClient(srv).FindProjectBinding(t.Context(), theProject)
	if !errors.Is(err, ErrNotBound) || errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotBound only", err)
	}
}

// A project that was deleted after GetProject read it.
func TestFindProjectBindingOfAProjectThatDoesNotExist(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Project does not exist"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).FindProjectBinding(t.Context(), theProject)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestFindProjectBindingRefusesMoreThanOne(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(rec.serve(`{"bindings":[` + projectBindingAnswer + `,` + projectBindingAnswer + `]}`))
	defer srv.Close()

	_, err := newTestClient(srv).FindProjectBinding(t.Context(), theProject)
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotBound) {
		t.Errorf("error = %v, want a failure that is neither ErrNotFound nor ErrNotBound", err)
	}
}

func TestUpdateProjectBinding(t *testing.T) {
	t.Parallel()

	var contentType string
	rec := &recorder{}
	serve := rec.serve(projectBindingAnswer)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		serve(w, r)
	}))
	defer srv.Close()

	binding, err := newTestClient(srv).UpdateProjectBinding(t.Context(), "binding-id", "my-github-org/my-repo")
	if err != nil {
		t.Fatalf("UpdateProjectBinding() returned %v", err)
	}

	if rec.method != http.MethodPatch {
		t.Errorf("method = %q, want %q", rec.method, http.MethodPatch)
	}
	if want := "/dop-translation/project-bindings/binding-id"; rec.path != want {
		t.Errorf("path = %q, want %q", rec.path, want)
	}
	if want := "application/merge-patch+json"; contentType != want {
		t.Errorf("Content-Type = %q, want %q", contentType, want)
	}
	if got, want := rec.body["repositoryId"], "my-github-org/my-repo"; got != want {
		t.Errorf("repositoryId = %v, want %q", got, want)
	}
	if binding.Slug != "my-github-org/my-repo" {
		t.Errorf("Slug = %q, want my-github-org/my-repo", binding.Slug)
	}
}
