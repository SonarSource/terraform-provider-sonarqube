package client

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

var editorProfile = QualityProfile{Key: "profile-1", Name: "Back/end", Language: "java"}

func TestQualityProfileEditorWritesSendNameAndLanguage(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusNoContent, "", &requests)
	for _, editor := range []PermissionPrincipal{{Group: true, Name: "devs"}, {Name: "alice@github"}} {
		if err := c.AddQualityProfileEditor(t.Context(), "my-org", editorProfile, editor); err != nil {
			t.Fatal(err)
		}
		if err := c.RemoveQualityProfileEditor(t.Context(), "my-org", editorProfile, editor); err != nil {
			t.Fatal(err)
		}
	}
	profile := url.Values{"organization": {"my-org"}, "qualityProfile": {"Back/end"}, "language": {"java"}}
	group, user := url.Values{"group": {"devs"}}, url.Values{"login": {"alice@github"}}
	for _, values := range []url.Values{group, user} {
		for key, value := range profile {
			values[key] = value
		}
	}
	want := []string{
		"POST /api/qualityprofiles/add_group " + group.Encode(), "POST /api/qualityprofiles/remove_group " + group.Encode(),
		"POST /api/qualityprofiles/add_user " + user.Encode(), "POST /api/qualityprofiles/remove_user " + user.Encode(),
	}
	if !reflect.DeepEqual(requests, want) {
		t.Errorf("requests = %v", requests)
	}
}

// The q filter matches parts of names, so the search can give other groups
// first, on more than one page. Only the last page has the group.
func TestHasQualityProfileEditorReadsAllPagesAndIgnoresPartialMatches(t *testing.T) {
	t.Parallel()
	names := []string{"devs-old", "devs-team", "devs"}
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		requests = append(requests, r.URL.Path+" "+r.Form.Encode())
		page, _ := strconv.Atoi(r.Form.Get("p"))
		fmt.Fprintf(w, `{"paging":{"total":%d},"groups":[{"name":%q,"selected":true}]}`, len(names), names[page-1])
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)
	found, err := c.HasQualityProfileEditor(t.Context(), "my-org", editorProfile, PermissionPrincipal{Group: true, Name: "devs"})
	if err != nil || !found {
		t.Fatalf("found = %v, err = %v", found, err)
	}
	if len(requests) != 3 || !strings.Contains(requests[0], "selected=selected") || !strings.Contains(requests[0], "q=devs") {
		t.Errorf("requests = %v", requests)
	}

	names = []string{"devs-old"}
	if found, err := c.HasQualityProfileEditor(t.Context(), "my-org", editorProfile, PermissionPrincipal{Group: true, Name: "devs"}); err != nil || found {
		t.Errorf("found = %v, err = %v, want a missing editor", found, err)
	}
}

func TestHasQualityProfileEditorMatchesUserLogin(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusOK,
		`{"paging":{"total":2},"users":[{"login":"alice@github-2","name":"Alice"},{"login":"alice@github","name":"Alice"}]}`, &requests)
	found, err := c.HasQualityProfileEditor(t.Context(), "my-org", editorProfile, PermissionPrincipal{Name: "alice@github"})
	if err != nil || !found {
		t.Fatalf("found = %v, err = %v", found, err)
	}
	if !strings.HasPrefix(requests[0], "GET /api/qualityprofiles/search_users ") {
		t.Errorf("request = %s", requests[0])
	}
}

func TestQualityProfileEditorErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		status   int
		body     string
		want     string
		sentinel error
	}{
		"missing organization": {http.StatusNotFound, `{"errors":[{"msg":"No organization with key 'my-org'"}]}`,
			"my-org", ErrOrganizationNotFound},
		"missing group": {http.StatusNotFound, `{"errors":[{"msg":"No group with name 'devs' in organization 'my-org'"}]}`,
			"No group with name", ErrEditorNotFound},
		"missing profile": {http.StatusNotFound,
			`{"errors":[{"msg":"Quality Profile for language 'java' and name 'Back/end' does not exist in organization 'my-org'"}]}`,
			"does not exist", ErrNotFound},
		"built-in profile": {http.StatusBadRequest,
			`{"errors":[{"msg":"Operation forbidden for built-in Quality Profile 'Sonar way' with language 'java'"}]}`,
			"cannot have editors", nil},
		"not a member": {http.StatusBadRequest, `{"errors":[{"msg":"User 'alice' is not member of organization 'my-org'"}]}`,
			"Add the user to the organization", ErrEditorNotFound},
		"plan": {http.StatusForbidden, `{"errors":[{"msg":"This action is not allowed for your current subscription plan"}]}`,
			"Team or the Enterprise plan", nil},
		"permission": {http.StatusForbidden, `{"errors":[{"msg":"Insufficient privileges"}]}`,
			"Administer Quality Profiles", nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requests []string
			err := profileServer(t, tt.status, tt.body, &requests).
				AddQualityProfileEditor(t.Context(), "my-org", editorProfile, PermissionPrincipal{Group: true, Name: "devs"})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if tt.sentinel != nil && !errors.Is(err, tt.sentinel) {
				t.Errorf("err = %v, want %v", err, tt.sentinel)
			}
		})
	}
}

func TestRemoveQualityProfileEditorAcceptsMissingGroupOrUserOnly(t *testing.T) {
	t.Parallel()
	for body, wantErr := range map[string]bool{
		`{"errors":[{"msg":"No group with name 'devs' in organization 'my-org'"}]}`:  false,
		`{"errors":[{"msg":"User with login 'alice' is not found'"}]}`:               false,
		`{"errors":[{"msg":"User 'alice' is not member of organization 'my-org'"}]}`: false,
		// The name can be out of date after a rename, so the right can stay.
		`{"errors":[{"msg":"Quality Profile for language 'java' and name 'Back/end' does not exist in organization 'my-org'"}]}`: true,
	} {
		status := http.StatusNotFound
		if strings.Contains(body, "not member") {
			status = http.StatusBadRequest
		}
		var requests []string
		err := profileServer(t, status, body, &requests).
			RemoveQualityProfileEditor(t.Context(), "my-org", editorProfile, PermissionPrincipal{Group: true, Name: "devs"})
		if (err != nil) != wantErr {
			t.Errorf("%s: err = %v, want error %v", body, err, wantErr)
		}
	}
}

func TestHasQualityProfileEditorMatchesGroupWithoutCase(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusOK, `{"paging":{"total":1},"groups":[{"name":"Backend-Team"}]}`, &requests)
	found, err := c.HasQualityProfileEditor(t.Context(), "my-org", editorProfile, PermissionPrincipal{Group: true, Name: "backend-team"})
	if err != nil || !found {
		t.Errorf("found = %v, err = %v", found, err)
	}
	users := profileServer(t, http.StatusOK, `{"paging":{"total":1},"users":[{"login":"Alice@github"}]}`, &requests)
	if found, err := users.HasQualityProfileEditor(t.Context(), "my-org", editorProfile, PermissionPrincipal{Name: "alice@github"}); err != nil || found {
		t.Errorf("found = %v, err = %v, want a login that matches exactly", found, err)
	}
}
