package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// profileServer answers each request with status and body, and records the
// method, path and form of each request.
func profileServer(t *testing.T, status int, body string, requests *[]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		*requests = append(*requests, r.Method+" "+r.URL.Path+" "+r.Form.Encode())
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return newTestClient(srv)
}

func TestCreateQualityProfileSendsFormAndReturnsProfile(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusOK,
		`{"profile":{"key":"profile-1","name":"Backend","language":"java","isDefault":false}}`, &requests)
	profile, err := c.CreateQualityProfile(t.Context(), "my-org", "java", "Backend")
	if err != nil {
		t.Fatal(err)
	}
	want := QualityProfile{Key: "profile-1", Name: "Backend", Language: "java"}
	if *profile != want {
		t.Errorf("profile = %+v", *profile)
	}
	form := url.Values{"organization": {"my-org"}, "language": {"java"}, "name": {"Backend"}}
	if requests[0] != "POST /api/qualityprofiles/create "+form.Encode() {
		t.Errorf("request = %s", requests[0])
	}
}

func TestCreateQualityProfileRefusesAnswerWithoutKey(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusOK, `{"profile":{}}`, &requests)
	if _, err := c.CreateQualityProfile(t.Context(), "my-org", "java", "Backend"); err == nil || !strings.Contains(err.Error(), "no key") {
		t.Fatalf("err = %v", err)
	}
}

func TestQualityProfileWritesSendKey(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		call func(ctx context.Context, c *Client) error
		want string
	}{
		"set parent": {
			call: func(ctx context.Context, c *Client) error {
				return c.ChangeQualityProfileParent(ctx, "profile-1", "parent-1")
			},
			want: "POST /api/qualityprofiles/change_parent key=profile-1&parentKey=parent-1",
		},
		"remove parent": {
			call: func(ctx context.Context, c *Client) error {
				return c.ChangeQualityProfileParent(ctx, "profile-1", "")
			},
			want: "POST /api/qualityprofiles/change_parent key=profile-1",
		},
		"rename": {
			call: func(ctx context.Context, c *Client) error {
				return c.RenameQualityProfile(ctx, "profile-1", "Backend 2")
			},
			want: "POST /api/qualityprofiles/rename key=profile-1&name=Backend+2",
		},
		"delete": {
			call: func(ctx context.Context, c *Client) error { return c.DeleteQualityProfile(ctx, "profile-1") },
			want: "POST /api/qualityprofiles/delete key=profile-1",
		},
		"set default": {
			call: func(ctx context.Context, c *Client) error {
				return c.SetDefaultQualityProfile(ctx, "profile-1")
			},
			want: "POST /api/qualityprofiles/set_default key=profile-1",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requests []string
			if err := tt.call(t.Context(), profileServer(t, http.StatusNoContent, "", &requests)); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(requests, []string{tt.want}) {
				t.Errorf("requests = %v, want %s", requests, tt.want)
			}
		})
	}
}

func TestDeleteQualityProfileAcceptsMissingProfile(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusNotFound, `{"errors":[{"msg":"Quality Profile with key 'profile-1' does not exist"}]}`, &requests)
	if err := c.DeleteQualityProfile(t.Context(), "profile-1"); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteQualityProfileExplainsDefault(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusBadRequest,
		`{"errors":[{"msg":"Profile 'Backend' cannot be deleted because its descendant named 'Child' is marked as default"}]}`, &requests)
	err := c.DeleteQualityProfile(t.Context(), "profile-1")
	if err == nil || !strings.Contains(err.Error(), "Set a different default profile first") ||
		!strings.Contains(err.Error(), "descendant named 'Child'") {
		t.Fatalf("err = %v", err)
	}
}

const testProfileList = `{"profiles":[
	{"key":"core","name":"Sonar way core","language":"java","isBuiltIn":true},
	{"key":"profile-1","name":"Backend","language":"java","parentKey":"core","isDefault":true}
]}`

func TestGetQualityProfileFindsKeyAndParent(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusOK, testProfileList, &requests)
	profile, err := c.GetQualityProfile(t.Context(), "my-org", "java", "profile-1")
	if err != nil {
		t.Fatal(err)
	}
	want := QualityProfile{Key: "profile-1", Name: "Backend", Language: "java", ParentKey: "core", IsDefault: true}
	if *profile != want {
		t.Errorf("profile = %+v", *profile)
	}
	if requests[0] != "GET /api/qualityprofiles/search language=java&organization=my-org" {
		t.Errorf("request = %s", requests[0])
	}
	if _, err := c.GetQualityProfile(t.Context(), "my-org", "java", "profile-2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing key: err = %v", err)
	}
}

func TestFindQualityProfileByNameMatchesExactName(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusOK, testProfileList, &requests)
	profile, err := c.FindQualityProfileByName(t.Context(), "my-org", "java", "Sonar way core")
	if err != nil || profile.Key != "core" || !profile.IsBuiltIn {
		t.Fatalf("profile = %+v, err = %v", profile, err)
	}
	if requests[0] != "GET /api/qualityprofiles/search language=java&organization=my-org&qualityProfile=Sonar+way+core" {
		t.Errorf("request = %s", requests[0])
	}
	if _, err := c.FindQualityProfileByName(t.Context(), "my-org", "java", "backend"); !errors.Is(err, ErrNotFound) {
		t.Errorf("other case: err = %v", err)
	}
}

func TestListQualityProfilesOfAllLanguages(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusOK, testProfileList, &requests)
	profiles, err := c.ListQualityProfiles(t.Context(), "my-org", "")
	if err != nil || len(profiles) != 2 {
		t.Fatalf("profiles = %v, err = %v", profiles, err)
	}
	if requests[0] != "GET /api/qualityprofiles/search organization=my-org" {
		t.Errorf("request = %s", requests[0])
	}
}

func TestQualityProfileSearchReportsMissingOrganization(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusNotFound, `{"errors":[{"msg":"No organization with key 'my-org'"}]}`, &requests)
	_, err := c.GetQualityProfile(t.Context(), "my-org", "java", "profile-1")
	if !errors.Is(err, ErrOrganizationNotFound) || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestGetQualityProfileParentKey(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		body string
		want string
	}{
		"parent":    {`{"profile":{"key":"profile-1"},"ancestors":[{"key":"parent-1"},{"key":"root-1"}]}`, "parent-1"},
		"no parent": {`{"profile":{"key":"profile-1"},"ancestors":[]}`, ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requests []string
			got, err := profileServer(t, http.StatusOK, tt.body, &requests).GetQualityProfileParentKey(t.Context(), "profile-1")
			if err != nil || got != tt.want {
				t.Fatalf("parent = %q, err = %v", got, err)
			}
			if requests[0] != "GET /api/qualityprofiles/inheritance key=profile-1" {
				t.Errorf("request = %s", requests[0])
			}
		})
	}
}

func TestGetDefaultQualityProfileAsksForDefaults(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusOK, testProfileList, &requests)
	profile, err := c.GetDefaultQualityProfile(t.Context(), "my-org", "java")
	if err != nil || profile.Key != "profile-1" {
		t.Fatalf("profile = %+v, err = %v", profile, err)
	}
	if requests[0] != "GET /api/qualityprofiles/search defaults=true&language=java&organization=my-org" {
		t.Errorf("request = %s", requests[0])
	}
}

func TestGetDefaultQualityProfileWithoutDefault(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusOK, `{"profiles":[]}`, &requests)
	if _, err := c.GetDefaultQualityProfile(t.Context(), "my-org", "java"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestGetDefaultQualityProfileReportsMissingOrganization(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusNotFound, `{"errors":[{"msg":"No organization with key 'my-org'"}]}`, &requests)
	if _, err := c.GetDefaultQualityProfile(t.Context(), "my-org", "java"); !errors.Is(err, ErrOrganizationNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestSetDefaultQualityProfileExplainsRefusals(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		status int
		body   string
		want   string
	}{
		"MISRA entitlement": {
			http.StatusForbidden, `{"errors":[{"msg":"Enterprise MISRA rules require a MISRA entitlement"}]}`,
			"does not have the MISRA entitlement",
		},
		"permission": {
			http.StatusForbidden, `{"errors":[{"msg":"Insufficient privileges"}]}`,
			"Administer Quality Profiles permission",
		},
		"plan": {
			http.StatusBadRequest, `{"errors":[{"msg":"Organization 'organization-uuid' is not allowed to perform this action"}]}`,
			"Team or the Enterprise plan",
		},
		"other": {
			http.StatusBadRequest, `{"errors":[{"msg":"Something else"}]}`,
			"Something else",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requests []string
			err := profileServer(t, tt.status, tt.body, &requests).SetDefaultQualityProfile(t.Context(), "profile-1")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestQualityProfileProjectWritesSendKeyAndProject(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusNoContent, "", &requests)
	if err := c.AddQualityProfileProject(t.Context(), "profile-1", "my-project"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveQualityProfileProject(t.Context(), "profile-1", "my-project"); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"key": {"profile-1"}, "project": {"my-project"}}.Encode()
	want := []string{"POST /api/qualityprofiles/add_project " + form, "POST /api/qualityprofiles/remove_project " + form}
	if !reflect.DeepEqual(requests, want) {
		t.Errorf("requests = %v", requests)
	}
}

func TestAddQualityProfileProjectExplainsRefusals(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		status   int
		body     string
		want     string
		sentinel error
	}{
		"missing project": {
			http.StatusNotFound, `{"errors":[{"msg":"Component key 'my-project' not found"}]}`, "my-project", ErrProjectNotFound,
		},
		"missing profile": {
			http.StatusNotFound, `{"errors":[{"msg":"Quality Profile with key 'profile-1' does not exist"}]}`, "profile-1", ErrNotFound,
		},
		"permission": {
			http.StatusNotFound, `{"errors":[{"msg":"Project doesn't exist"}]}`, "Administer permission of the project", nil,
		},
		"MISRA entitlement": {
			http.StatusForbidden, `{"errors":[{"msg":"Enterprise MISRA rules require a MISRA entitlement"}]}`,
			"does not have the MISRA entitlement", nil,
		},
		"plan": {
			http.StatusBadRequest, `{"errors":[{"msg":"Organization 'organization-uuid' is not allowed to perform this action"}]}`,
			"Team or the Enterprise plan", ErrQualityProfilePlanRefused,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requests []string
			err := profileServer(t, tt.status, tt.body, &requests).AddQualityProfileProject(t.Context(), "profile-1", "my-project")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if tt.sentinel != nil && !errors.Is(err, tt.sentinel) {
				t.Errorf("err = %v, want %v", err, tt.sentinel)
			}
			if tt.sentinel == nil && (errors.Is(err, ErrNotFound) || errors.Is(err, ErrProjectNotFound)) {
				t.Errorf("err = %v matches a not-found error", err)
			}
		})
	}
}

func TestRemoveQualityProfileProjectAcceptsMissingProjectOrProfileOnly(t *testing.T) {
	t.Parallel()
	for body, wantErr := range map[string]bool{
		`{"errors":[{"msg":"Component key 'my-project' not found"}]}`:                false,
		`{"errors":[{"msg":"Quality Profile with key 'profile-1' does not exist"}]}`: false,
		`{"errors":[{"msg":"Project doesn't exist"}]}`:                               true,
	} {
		var requests []string
		err := profileServer(t, http.StatusNotFound, body, &requests).RemoveQualityProfileProject(t.Context(), "profile-1", "my-project")
		if (err != nil) != wantErr {
			t.Errorf("body %s: err = %v, want error %v", body, err, wantErr)
		}
	}
}

func TestGetProjectQualityProfile(t *testing.T) {
	t.Parallel()
	var requests []string
	c := profileServer(t, http.StatusOK, `{"profiles":[{"key":"profile-1","language":"java","isDefault":true}]}`, &requests)
	profile, err := c.GetProjectQualityProfile(t.Context(), "my-org", "my-project", "java")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Key != "profile-1" {
		t.Errorf("profile = %+v", *profile)
	}
	form := url.Values{"organization": {"my-org"}, "project": {"my-project"}, "language": {"java"}}
	if requests[0] != "GET /api/qualityprofiles/search "+form.Encode() {
		t.Errorf("request = %s", requests[0])
	}

	empty := profileServer(t, http.StatusOK, `{"profiles":[]}`, &requests)
	if _, err := empty.GetProjectQualityProfile(t.Context(), "my-org", "my-project", "java"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	for body, want := range map[string]error{
		`{"errors":[{"msg":"Component key 'my-project' not found"}]}`: ErrProjectNotFound,
		`{"errors":[{"msg":"No organization with key 'my-org'"}]}`:    ErrOrganizationNotFound,
	} {
		_, err := profileServer(t, http.StatusNotFound, body, &requests).GetProjectQualityProfile(t.Context(), "my-org", "my-project", "java")
		if !errors.Is(err, want) || errors.Is(err, ErrNotFound) {
			t.Errorf("body %s: err = %v, want %v", body, err, want)
		}
	}
	// search answers this for a project of a different organization.
	_, err = profileServer(t, http.StatusNotFound, `{"errors":[{"msg":"Project 'my-project' not found"}]}`, &requests).
		GetProjectQualityProfile(t.Context(), "my-org", "my-project", "java")
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("err = %v, want ErrProjectNotFound", err)
	}
}
