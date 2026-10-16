package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

const groupOrganizationUUID = "00000000-0000-4000-8000-000000000001"

// groupServer answers the organization read, and hands every other request to
// groups.
func groupServer(t *testing.T, groups http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/organizations/organizations" {
			fmt.Fprint(w, projectOrganizationAnswer)
			return
		}
		groups(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestListGroupsReadsEveryPage(t *testing.T) {
	t.Parallel()
	var pages []int
	srv := groupServer(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != groupsPath || query.Get("organizationIds") != groupOrganizationUUID || query.Get("pageSize") != "500" {
			t.Errorf("unexpected group search: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		page, _ := strconv.Atoi(query.Get("pageIndex"))
		pages = append(pages, page)
		count := 500
		if page == 2 {
			count = 1
		}
		groups := make([]map[string]any, count)
		for i := range groups {
			id := (page-1)*500 + i + 1
			groups[i] = map[string]any{"id": fmt.Sprintf("group-id-%d", id), "name": fmt.Sprintf("group-%d", id)}
		}
		json.NewEncoder(w).Encode(map[string]any{"groups": groups, "page": map[string]int{"total": 501}})
	})

	groups, err := newTestClient(srv).listGroups(t.Context(), "my-org", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 501 || groups[500].ID != "group-id-501" || len(pages) != 2 || pages[0] != 1 || pages[1] != 2 {
		t.Errorf("groups=%d, final=%+v, pages=%v", len(groups), groups[len(groups)-1], pages)
	}
}

func TestListGroupsFollowsTotalWhenServerCapsPageSize(t *testing.T) {
	t.Parallel()
	srv := groupServer(t, func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("pageIndex"))
		groups := make([]map[string]any, 100)
		for i := range groups {
			groups[i] = map[string]any{"id": fmt.Sprintf("group-id-%d", (page-1)*100+i+1)}
		}
		json.NewEncoder(w).Encode(map[string]any{"groups": groups, "page": map[string]int{"total": 200}})
	})
	groups, err := newTestClient(srv).listGroups(t.Context(), "my-org", nil)
	if err != nil || len(groups) != 200 {
		t.Fatalf("groups=%d, err=%v, want 200", len(groups), err)
	}
}

func TestListGroupsRefusesEmptyPageBeforeTotal(t *testing.T) {
	t.Parallel()
	srv := groupServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"groups":[],"page":{"total":3}}`)
	})
	if _, err := newTestClient(srv).listGroups(t.Context(), "my-org", nil); err == nil {
		t.Error("an empty page before the total was accepted")
	}
}

func TestGetGroupFiltersByID(t *testing.T) {
	t.Parallel()
	srv := groupServer(t, func(w http.ResponseWriter, r *http.Request) {
		if ids := r.URL.Query().Get("ids"); ids != "group-id-12" {
			t.Errorf("ids = %q, want group-id-12", ids)
		}
		fmt.Fprint(w, `{"groups":[{"id":"group-id-12","name":"New name","builtIn":false,"managed":true}],"page":{"total":1}}`)
	})
	group, err := newTestClient(srv).GetGroup(t.Context(), "my-org", "group-id-12")
	if err != nil || group.Name != "New name" || !group.Managed || group.Description != "" {
		t.Fatalf("GetGroup = %+v, %v", group, err)
	}
}

func TestGetGroupMissingIsNotFound(t *testing.T) {
	t.Parallel()
	srv := groupServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"groups":[],"page":{"total":0}}`)
	})
	_, err := newTestClient(srv).GetGroup(t.Context(), "my-org", "group-id-12")
	if !errors.Is(err, ErrNotFound) || errors.Is(err, ErrOrganizationNotFound) {
		t.Errorf("missing group returned %v, want ErrNotFound only", err)
	}
}

func TestGroupCallsReportMissingOrganization(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	_, err := newTestClient(srv).GetGroup(t.Context(), "my-org", "group-id-12")
	if !errors.Is(err, ErrOrganizationNotFound) || errors.Is(err, ErrNotFound) {
		t.Errorf("organization 404 returned %v, want ErrOrganizationNotFound and not ErrNotFound", err)
	}
}

func TestFindGroupByNameMatchesExactName(t *testing.T) {
	t.Parallel()
	srv := groupServer(t, func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("q"); q != "Developers" {
			t.Errorf("q = %q, want Developers", q)
		}
		fmt.Fprint(w, `{"groups":[{"id":"group-id-1","name":"Developers-extra"},{"id":"group-id-2","name":"Developers"}],"page":{"total":2}}`)
	})
	group, err := newTestClient(srv).FindGroupByName(t.Context(), "my-org", "Developers")
	if err != nil || group.ID != "group-id-2" {
		t.Fatalf("FindGroupByName = %+v, %v", group, err)
	}
}

func TestCreateGroupSendsOrganizationUUID(t *testing.T) {
	t.Parallel()
	var body map[string]string
	srv := groupServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != groupsPath {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":"group-id-42","name":"First","builtIn":false,"managed":false}`)
	})
	group, err := newTestClient(srv).CreateGroup(t.Context(), "my-org", "First", "")
	if err != nil || group.ID != "group-id-42" {
		t.Fatalf("CreateGroup = %+v, %v", group, err)
	}
	if body["organizationId"] != groupOrganizationUUID || body["name"] != "First" || body["description"] != "" {
		t.Errorf("create body = %v", body)
	}
}

func TestCreateGroupRefusesAnswerWithoutID(t *testing.T) {
	t.Parallel()
	srv := groupServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{}`)
	})
	_, err := newTestClient(srv).CreateGroup(t.Context(), "my-org", "First", "")
	if err == nil {
		t.Fatal("an answer without an id was accepted")
	}
}

func TestUpdateGroupSendsMergePatchWithBothFields(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != groupsPath+"/group-id-42" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/merge-patch+json" {
			t.Errorf("Content-Type = %q", ct)
		}
		var body map[string]string
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		if body["name"] != "Second" {
			t.Errorf("update body = %v", body)
		}
		if description, present := body["description"]; !present || description != "" {
			t.Errorf("update body = %v, want an empty description that removes it", body)
		}
		fmt.Fprint(w, `{"id":"group-id-42","name":"Second"}`)
	}))
	t.Cleanup(srv.Close)
	if err := newTestClient(srv).UpdateGroup(t.Context(), "group-id-42", "Second", ""); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteGroupAcceptsMissingGroup(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != groupsPath+"/group-id-42" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	if err := newTestClient(srv).DeleteGroup(t.Context(), "group-id-42"); err != nil {
		t.Fatal(err)
	}
}

func TestSearchGroupsSendsTheText(t *testing.T) {
	t.Parallel()
	srv := groupServer(t, func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("q"); q != "tf-acc-test-" {
			t.Errorf("q=%q", q)
		}
		fmt.Fprint(w, `{"groups":[{"id":"group-id","name":"tf-acc-test-1"}],"page":{"total":1}}`)
	})

	groups, err := newTestClient(srv).SearchGroups(t.Context(), "my-org", "tf-acc-test-")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].ID != "group-id" {
		t.Errorf("groups=%+v", groups)
	}
}
