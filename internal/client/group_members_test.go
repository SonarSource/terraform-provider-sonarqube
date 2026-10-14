package client

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestAddAndRemoveGroupMemberSendForm(t *testing.T) {
	t.Parallel()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Method != http.MethodPost || r.PostForm.Get("organization") != "my-org" ||
			r.PostForm.Get("name") != "team/a" || r.PostForm.Get("login") != "alice@github" {
			t.Errorf("unexpected request %s %s %v", r.Method, r.URL.Path, r.PostForm)
		}
		calls = append(calls, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)
	if err := c.AddGroupMember(t.Context(), "my-org", "team/a", "alice@github"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveGroupMember(t.Context(), "my-org", "team/a", "alice@github"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"/api/user_groups/add_user", "/api/user_groups/remove_user"}; !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
}

func TestIsGroupMemberReadsAllPagesAndMatchesExactLogin(t *testing.T) {
	t.Parallel()
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/user_groups/users" || q.Get("organization") != "my-org" || q.Get("name") != "Developers" ||
			q.Get("selected") != "selected" || q.Get("q") != "alice" || q.Get("ps") != "500" {
			t.Errorf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		pages = append(pages, q.Get("p"))
		if q.Get("p") == "1" {
			fmt.Fprint(w, `{"total":2,"users":[{"login":"alice-2","name":"alice"}]}`)
			return
		}
		fmt.Fprint(w, `{"total":2,"users":[{"login":"alice"}]}`)
	}))
	t.Cleanup(srv.Close)
	member, err := newTestClient(srv).IsGroupMember(t.Context(), "my-org", "Developers", "alice")
	if err != nil || !member {
		t.Fatalf("member = %v, err = %v", member, err)
	}
	if !reflect.DeepEqual(pages, []string{"1", "2"}) {
		t.Errorf("pages = %v", pages)
	}
}

func TestIsGroupMemberIgnoresOtherCase(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"total":1,"users":[{"login":"Alice"}]}`)
	}))
	t.Cleanup(srv.Close)
	member, err := newTestClient(srv).IsGroupMember(t.Context(), "my-org", "Developers", "alice")
	if err != nil || member {
		t.Fatalf("member = %v, err = %v", member, err)
	}
}

func TestIsGroupMemberStopsOnEmptyPage(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"total":3,"users":[]}`)
	}))
	t.Cleanup(srv.Close)
	if _, err := newTestClient(srv).IsGroupMember(t.Context(), "my-org", "Developers", "alice"); err == nil {
		t.Fatal("an empty page before the total was accepted")
	}
}

func TestGroupMemberErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		msg    string
		want   error
		detail string
	}{
		{"organization", http.StatusNotFound, "No organization with key 'my-org'", ErrOrganizationNotFound, ""},
		{"group", http.StatusNotFound, "No group with name 'Developers' in organization 'my-org'", ErrNotFound, ""},
		{"user", http.StatusNotFound, "Could not find a user with login 'alice'", ErrNotFound, ""},
		{"not member", http.StatusBadRequest, "User 'alice' is not member of organization 'my-org'", nil, "Add the user to the organization"},
		{"last admin", http.StatusBadRequest, "The last administrator user cannot be removed", nil, "Give the admin permission directly"},
		{"other", http.StatusBadRequest, "Default group 'Members' cannot be used to perform this action", nil, "Default group"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprintf(w, `{"errors":[{"msg":%q}]}`, tc.msg)
			}))
			t.Cleanup(srv.Close)
			c := newTestClient(srv)
			for _, err := range []error{
				c.AddGroupMember(t.Context(), "my-org", "Developers", "alice"),
				c.RemoveGroupMember(t.Context(), "my-org", "Developers", "alice"),
				func() error { _, err := c.IsGroupMember(t.Context(), "my-org", "Developers", "alice"); return err }(),
			} {
				if err == nil {
					t.Fatal("no error")
				}
				if tc.want != nil && !errors.Is(err, tc.want) {
					t.Errorf("err = %v, want %v", err, tc.want)
				}
				if !strings.Contains(err.Error(), tc.detail) {
					t.Errorf("err = %v, want it to hold %q", err, tc.detail)
				}
			}
		})
	}
}

func TestGroupMemberErrorsKeepOrganizationApart(t *testing.T) {
	t.Parallel()
	missingGroup := groupMemberError(&APIError{StatusCode: http.StatusNotFound, Messages: []string{"No group with name 'x'"}}, "my-org", "alice")
	if errors.Is(missingGroup, ErrOrganizationNotFound) || !errors.Is(missingGroup, ErrNotFound) {
		t.Errorf("missing group = %v matches the wrong sentinels", missingGroup)
	}
	missingOrganization := groupMemberError(&APIError{StatusCode: http.StatusNotFound, Messages: []string{"No organization with key 'my-org'"}}, "my-org", "alice")
	if !errors.Is(missingOrganization, ErrOrganizationNotFound) || errors.Is(missingOrganization, ErrNotFound) {
		t.Errorf("missing organization = %v matches the wrong sentinels", missingOrganization)
	}
}
