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
