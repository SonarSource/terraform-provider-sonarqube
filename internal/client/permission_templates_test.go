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

const testTemplateList = `{
	"permissionTemplates":[
		{"id":"template-1","name":"Default template","permissions":[{"key":"admin","withProjectCreator":false}]},
		{"id":"template-2","name":"Backend","description":"Backend projects","projectKeyPattern":"backend-.*",
		 "permissions":[{"key":"admin","withProjectCreator":true},{"key":"scan","withProjectCreator":false}]}
	],
	"defaultTemplates":[{"templateId":"template-1","qualifier":"TRK"}]
}`

// templateServer serves search_templates, and two pages of template_groups
// and one of template_users for template-2. The lists hold principals
// without permissions, as the server does. It records the path and page of
// each call.
func templateServer(t *testing.T, calls *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("organization") != "my-org" {
			t.Errorf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		if calls != nil {
			*calls = append(*calls, r.URL.Path+"?p="+q.Get("p"))
		}
		switch {
		case r.URL.Path == "/api/permissions/search_templates":
			fmt.Fprint(w, testTemplateList)
		case q.Get("templateId") != "template-2" || q.Get("ps") != "100":
			t.Errorf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
		case r.URL.Path == "/api/permissions/template_groups" && q.Get("p") == "1":
			fmt.Fprint(w, `{"paging":{"total":3},"groups":[{"name":"Anyone","permissions":[]},{"id":"1","name":"Developers","permissions":["user","codeviewer"]}]}`)
		case r.URL.Path == "/api/permissions/template_groups":
			fmt.Fprint(w, `{"paging":{"total":3},"groups":[{"id":"2","name":"Owners","permissions":["admin"]}]}`)
		default:
			fmt.Fprint(w, `{"paging":{"total":2},"users":[{"login":"alice","permissions":["admin"]},{"login":"bob","permissions":[]}]}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCreatePermissionTemplateSendsFormAndReturnsID(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		want := url.Values{"organization": {"my-org"}, "name": {"Backend"}, "projectKeyPattern": {"backend-.*"}}
		if r.Method != http.MethodPost || r.URL.Path != "/api/permissions/create_template" || !reflect.DeepEqual(r.PostForm, want) {
			t.Errorf("unexpected request %s %s %v", r.Method, r.URL.Path, r.PostForm)
		}
		fmt.Fprint(w, `{"permissionTemplate":{"id":"template-2","name":"Backend","projectKeyPattern":"backend-.*"}}`)
	}))
	t.Cleanup(srv.Close)
	id, err := newTestClient(srv).CreatePermissionTemplate(t.Context(), "my-org",
		PermissionTemplate{Name: "Backend", ProjectKeyPattern: "backend-.*"})
	if err != nil || id != "template-2" {
		t.Fatalf("id = %q, err = %v", id, err)
	}
}

func TestCreatePermissionTemplateExplainsPlan(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"errors":[{"msg":"Organization 'my-org' is not allowed to use custom permission templates"}]}`)
	}))
	t.Cleanup(srv.Close)
	_, err := newTestClient(srv).CreatePermissionTemplate(t.Context(), "my-org", PermissionTemplate{Name: "Backend"})
	if err == nil || !strings.Contains(err.Error(), `the organization "my-org" cannot use custom permission templates`) {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdatePermissionTemplateSendsEmptyValues(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		want := url.Values{"id": {"template-2"}, "name": {"Backend"}, "description": {""}, "projectKeyPattern": {""}}
		if r.URL.Path != "/api/permissions/update_template" || !reflect.DeepEqual(r.PostForm, want) {
			t.Errorf("unexpected request %s %v", r.URL.Path, r.PostForm)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	if err := newTestClient(srv).UpdatePermissionTemplate(t.Context(), "my-org", PermissionTemplate{ID: "template-2", Name: "Backend"}); err != nil {
		t.Fatal(err)
	}
}

func deleteTemplateServer(t *testing.T, status int, message string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.URL.Path != "/api/permissions/delete_template" || r.PostForm.Get("templateId") != "template-2" {
			t.Errorf("unexpected request %s %v", r.URL.Path, r.PostForm)
		}
		w.WriteHeader(status)
		if message != "" {
			fmt.Fprintf(w, `{"errors":[{"msg":%q}]}`, message)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDeletePermissionTemplate(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status  int
		message string
		want    string
	}{
		"deleted":      {status: http.StatusNoContent},
		"already gone": {status: http.StatusNotFound, message: "Permission template with id 'template-2' is not found"},
		"default": {status: http.StatusBadRequest, message: "It is not possible to delete the default permission template for projects",
			want: "Set a different default template first"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := newTestClient(deleteTemplateServer(t, tc.status, tc.message)).DeletePermissionTemplate(t.Context(), "my-org", "template-2")
			got := ""
			if err != nil {
				got = err.Error()
			}
			if (tc.want == "") != (err == nil) || !strings.Contains(got, tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestGetPermissionTemplateReadsAllGrants(t *testing.T) {
	t.Parallel()
	var calls []string
	template, err := newTestClient(templateServer(t, &calls)).GetPermissionTemplate(t.Context(), "my-org", "template-2")
	if err != nil {
		t.Fatal(err)
	}
	want := &PermissionTemplate{ID: "template-2", Name: "Backend", Description: "Backend projects", ProjectKeyPattern: "backend-.*",
		Grants: []TemplateGrant{
			{Kind: GranteeProjectCreator, Permission: "admin"},
			{Kind: GranteeGroup, Name: "Developers", Permission: "user"},
			{Kind: GranteeGroup, Name: "Developers", Permission: "codeviewer"},
			{Kind: GranteeGroup, Name: "Owners", Permission: "admin"},
			{Kind: GranteeUser, Name: "alice", Permission: "admin"},
		}}
	if !reflect.DeepEqual(template, want) {
		t.Errorf("template = %+v", template)
	}
	wantCalls := []string{"/api/permissions/search_templates?p=", "/api/permissions/template_groups?p=1",
		"/api/permissions/template_groups?p=2", "/api/permissions/template_users?p=1"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Errorf("calls = %v", calls)
	}
}

func TestGetPermissionTemplateMissing(t *testing.T) {
	t.Parallel()
	_, err := newTestClient(templateServer(t, nil)).GetPermissionTemplate(t.Context(), "my-org", "template-3")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestFindPermissionTemplateIDIgnoresCase(t *testing.T) {
	t.Parallel()
	c := newTestClient(templateServer(t, nil))
	id, err := c.FindPermissionTemplateID(t.Context(), "my-org", "backend")
	if err != nil || id != "template-2" {
		t.Fatalf("id = %q, err = %v", id, err)
	}
	if _, err := c.FindPermissionTemplateID(t.Context(), "my-org", "Frontend"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing template: err = %v", err)
	}
}

func TestPermissionTemplatesReportMissingOrganization(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"errors":[{"msg":"No organization with key 'my-org'"}]}`)
	}))
	t.Cleanup(srv.Close)
	_, err := newTestClient(srv).GetPermissionTemplate(t.Context(), "my-org", "template-2")
	if !errors.Is(err, ErrOrganizationNotFound) || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestChangePermissionTemplateGrantSendsGrantee(t *testing.T) {
	t.Parallel()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		form := r.PostForm
		if form.Get("organization") != "my-org" || form.Get("templateId") != "template-2" || form.Get("permission") != "admin" {
			t.Errorf("unexpected form %v", form)
		}
		calls = append(calls, r.URL.Path+" group="+form.Get("groupName")+" login="+form.Get("login"))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)
	for _, grant := range []TemplateGrant{
		{Kind: GranteeGroup, Name: "Developers", Permission: "admin"},
		{Kind: GranteeUser, Name: "alice", Permission: "admin"},
		{Kind: GranteeProjectCreator, Permission: "admin"},
	} {
		if err := c.AddPermissionTemplateGrant(t.Context(), "my-org", "template-2", grant); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.RemovePermissionTemplateGrant(t.Context(), "my-org", "template-2", TemplateGrant{Kind: GranteeProjectCreator, Permission: "admin"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/api/permissions/add_group_to_template group=Developers login=",
		"/api/permissions/add_user_to_template group= login=alice",
		"/api/permissions/add_project_creator_to_template group= login=",
		"/api/permissions/remove_project_creator_from_template group= login=",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v", calls)
	}
}

func TestAddPermissionTemplateGrantExplainsNonMember(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"errors":[{"msg":"User 'alice' is not member of organization 'my-org'"}]}`)
	}))
	t.Cleanup(srv.Close)
	err := newTestClient(srv).AddPermissionTemplateGrant(t.Context(), "my-org", "template-2",
		TemplateGrant{Kind: GranteeUser, Name: "alice", Permission: "admin"})
	if err == nil || !strings.Contains(err.Error(), "Add the user to the organization first") {
		t.Fatalf("err = %v", err)
	}
}

func TestGetDefaultPermissionTemplateID(t *testing.T) {
	t.Parallel()
	id, err := newTestClient(templateServer(t, nil)).GetDefaultPermissionTemplateID(t.Context(), "my-org")
	if err != nil || id != "template-1" {
		t.Fatalf("id = %q, err = %v", id, err)
	}
}

func TestSetDefaultPermissionTemplateChecksOrganization(t *testing.T) {
	t.Parallel()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, r.URL.Path+" "+r.Form.Get("templateId"))
		if r.URL.Path == "/api/permissions/search_templates" {
			fmt.Fprint(w, testTemplateList)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)
	if err := c.SetDefaultPermissionTemplate(t.Context(), "my-org", "template-2"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetDefaultPermissionTemplate(t.Context(), "my-org", "other-org-template"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a template of another organization: err = %v", err)
	}
	want := []string{"/api/permissions/search_templates ", "/api/permissions/set_default_template template-2", "/api/permissions/search_templates "}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v", calls)
	}
}

func TestGetDefaultPermissionTemplateIDWithoutProjectDefault(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"permissionTemplates":[],"defaultTemplates":[{"templateId":"template-9","qualifier":"VW"}]}`)
	}))
	t.Cleanup(srv.Close)
	id, err := newTestClient(srv).GetDefaultPermissionTemplateID(t.Context(), "my-org")
	if err != nil || id != "" {
		t.Fatalf("id = %q, err = %v", id, err)
	}
}

func TestDefaultPermissionTemplateReportsMissingOrganization(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"errors":[{"msg":"No organization with key 'my-org'"}]}`)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)
	if _, err := c.GetDefaultPermissionTemplateID(t.Context(), "my-org"); !errors.Is(err, ErrOrganizationNotFound) {
		t.Errorf("get: err = %v", err)
	}
	if err := c.SetDefaultPermissionTemplate(t.Context(), "my-org", "template-2"); !errors.Is(err, ErrOrganizationNotFound) {
		t.Errorf("set: err = %v", err)
	}
}

func TestListPermissionTemplateNames(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/permissions/search_templates" || r.URL.Query().Get("organization") != "my-org" {
			t.Errorf("unexpected template search: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"permissionTemplates":[{"id":"t1","name":"tf-acc-test-1"},{"id":"t2","name":"Default template"}]}`)
	}))
	t.Cleanup(srv.Close)

	templates, err := newTestClient(srv).ListPermissionTemplateNames(t.Context(), "my-org")
	if err != nil {
		t.Fatal(err)
	}
	want := []PermissionTemplate{{ID: "t1", Name: "tf-acc-test-1"}, {ID: "t2", Name: "Default template"}}
	if !reflect.DeepEqual(templates, want) {
		t.Errorf("templates=%+v", templates)
	}
}
