package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestListGroupsReadsEveryPageWithoutNameFilter(t *testing.T) {
	t.Parallel()
	var pages []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user_groups/search" || r.URL.Query().Get("organization") != "my-org" || r.URL.Query().Get("ps") != "500" {
			t.Errorf("unexpected group search: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		if _, present := r.URL.Query()["q"]; present {
			t.Error("search must not filter by a substring")
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("p"))
		pages = append(pages, page)
		count := 500
		if page == 2 {
			count = 1
		}
		groups := make([]map[string]any, count)
		for i := range groups {
			id := (page-1)*500 + i + 1
			groups[i] = map[string]any{"id": id, "name": fmt.Sprintf("group-%d", id)}
		}
		json.NewEncoder(w).Encode(map[string]any{"groups": groups, "paging": map[string]int{"total": 501}})
	}))
	defer srv.Close()

	groups, err := newTestClient(srv).ListGroups(t.Context(), "my-org", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 501 {
		t.Fatalf("groups=%d, want 501", len(groups))
	}
	if groups[500].ID != "501" || len(pages) != 2 || pages[0] != 1 || pages[1] != 2 {
		t.Errorf("final=%+v, pages=%v", groups[500], pages)
	}
}

func TestGroupLookupsKeepIdentityAndExactName(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"groups":[{"id":12,"name":"New name"},{"id":13,"name":"New name extra"}],"paging":{"total":2}}`)
	}))
	defer srv.Close()
	c := newTestClient(srv)
	byID, err := c.GetGroup(t.Context(), "my-org", "12")
	if err != nil || byID.Name != "New name" {
		t.Fatalf("GetGroup = %+v, %v", byID, err)
	}
	byName, err := c.FindGroupByName(t.Context(), "my-org", "New name")
	if err != nil || byName.ID != "12" {
		t.Fatalf("FindGroupByName = %+v, %v", byName, err)
	}
	_, err = c.FindGroupByName(t.Context(), "my-org", "New")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("substring lookup returned %v, want ErrNotFound", err)
	}
}

func TestCreateAndUpdateGroupUseNumericID(t *testing.T) {
	t.Parallel()
	var forms []url.Values
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		forms = append(forms, r.PostForm)
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/user_groups/create" {
			fmt.Fprint(w, `{"group":{"id":42,"name":"First","description":"Initial"}}`)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	c := newTestClient(srv)
	group, err := c.CreateGroup(t.Context(), "my-org", "First", nil)
	if err != nil || group.ID != "42" {
		t.Fatalf("CreateGroup = %+v, %v", group, err)
	}
	name, description := "Second", ""
	if err := c.UpdateGroup(t.Context(), group.ID, &name, &description); err != nil {
		t.Fatal(err)
	}
	if paths[0] != "/api/user_groups/create" || forms[0].Get("organization") != "my-org" {
		t.Errorf("create path and form: %s, %v", paths[0], forms[0])
	}
	if _, present := forms[0]["description"]; present {
		t.Error("create sent an absent description")
	}
	if paths[1] != "/api/user_groups/update" || forms[1].Get("id") != "42" || forms[1].Get("name") != "Second" {
		t.Errorf("update path and form: %s, %v", paths[1], forms[1])
	}
	if _, present := forms[1]["description"]; !present {
		t.Error("update did not clear the description")
	}
}

func TestDeleteGroupAcceptsMissingGroup(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user_groups/delete" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if err := newTestClient(srv).DeleteGroup(t.Context(), "42"); err != nil {
		t.Fatal(err)
	}
}

func TestGroupSearchOrganizationNotFoundIsAPIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	_, err := newTestClient(srv).GetGroup(t.Context(), "my-org", "42")
	var apiErr *APIError
	if !errors.Is(err, ErrNotFound) || !errors.As(err, &apiErr) {
		t.Errorf("search 404 returned %v, want an *APIError that matches ErrNotFound", err)
	}
}

func TestGroupMissingFromListIsPlainNotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"groups":[],"paging":{"total":0}}`)
	}))
	defer srv.Close()
	_, err := newTestClient(srv).GetGroup(t.Context(), "my-org", "42")
	var apiErr *APIError
	if !errors.Is(err, ErrNotFound) || errors.As(err, &apiErr) {
		t.Errorf("missing group returned %v, want a plain ErrNotFound", err)
	}
}

func TestListGroupsFollowsTotalWhenServerCapsPageSize(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("p"))
		groups := make([]map[string]any, 100)
		for i := range groups {
			id := (page-1)*100 + i + 1
			groups[i] = map[string]any{"id": id, "name": fmt.Sprintf("group-%d", id)}
		}
		json.NewEncoder(w).Encode(map[string]any{"groups": groups, "paging": map[string]int{"total": 200}})
	}))
	defer srv.Close()
	groups, err := newTestClient(srv).ListGroups(t.Context(), "my-org", "")
	if err != nil || len(groups) != 200 {
		t.Fatalf("groups=%d, err=%v, want 200", len(groups), err)
	}
}

func TestListGroupsRefusesEmptyPageBeforeTotal(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"groups":[],"paging":{"total":3}}`)
	}))
	defer srv.Close()
	if _, err := newTestClient(srv).ListGroups(t.Context(), "my-org", ""); err == nil {
		t.Error("an empty page before the total was accepted")
	}
}

func TestCreateGroupFindsIDWhenAnswerHasNone(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/user_groups/create" {
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprint(w, `{"groups":[{"id":42,"name":"First"}],"paging":{"total":1}}`)
	}))
	defer srv.Close()
	group, err := newTestClient(srv).CreateGroup(t.Context(), "my-org", "First", nil)
	if err != nil || group.ID != "42" {
		t.Fatalf("CreateGroup = %+v, %v", group, err)
	}
}

func TestFindGroupByNameNarrowsSearchByQuery(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("q"); q != "Developers" {
			t.Errorf("q = %q, want Developers", q)
		}
		fmt.Fprint(w, `{"groups":[{"id":1,"name":"Developers-extra"},{"id":2,"name":"Developers"}],"paging":{"total":2}}`)
	}))
	defer srv.Close()
	group, err := newTestClient(srv).FindGroupByName(t.Context(), "my-org", "Developers")
	if err != nil || group.ID != "2" {
		t.Fatalf("FindGroupByName = %+v, %v", group, err)
	}
}
