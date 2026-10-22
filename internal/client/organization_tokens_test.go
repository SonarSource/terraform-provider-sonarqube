package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateOrganizationToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != organizationTokensPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var request OrganizationTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.OrganizationID != "org" || !request.AutoGrantProjectPermissions || len(request.RoleAssignments) != 2 {
			t.Errorf("unexpected request: %+v", request)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":"token-id","token":"test-secret","organizationId":"org","createdAt":"2026-01-01T00:00:00Z"}`)
	}))
	defer srv.Close()
	token, err := newTestClient(srv).CreateOrganizationToken(t.Context(), OrganizationTokenRequest{OrganizationID: "org", Name: "test", AutoGrantProjectPermissions: true, RoleAssignments: []TokenRoleAssignment{{ResourceType: "organization", Roles: []string{"provisioning"}}, {ResourceType: "project", Roles: []string{"scan"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if token.Token != "test-secret" || token.ID != "token-id" {
		t.Fatalf("unexpected token response: id=%q", token.ID)
	}
}

func TestListOrganizationTokens(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantError bool
	}{
		{"empty", 200, `{"tokenDefinitions":[],"page":{"total":0}}`, false},
		{"missing scope", 404, `{}`, true}, {"forbidden", 403, `{}`, true}, {"server failure", 500, `{}`, true},
		{"malformed", 200, `{`, true}, {"missing pagination", 200, `{}`, true},
		{"empty page", 200, `{"page":{}}`, true},
		{"missing total", 200, `{"tokenDefinitions":[],"page":{}}`, true},
		{"null total", 200, `{"tokenDefinitions":[],"page":{"total":null}}`, true},
		{"missing collection", 200, `{"page":{"total":0}}`, true},
		{"null collection", 200, `{"tokenDefinitions":null,"page":{"total":0}}`, true},
		{"incomplete", 200, `{"tokenDefinitions":[],"page":{"total":1}}`, true},
		{"wrong organization", 200, `{"tokenDefinitions":[{"id":"id","organizationId":"other"}],"page":{"total":1}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer srv.Close()
			_, err := newTestClient(srv).ListOrganizationTokens(t.Context(), "org")
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
			if tc.status == 404 && errors.Is(err, ErrNotFound) {
				t.Fatal("scope failure must not mean that the token is absent")
			}
		})
	}
}

func TestFindOrganizationTokenAcrossPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("organizationId") != "org" {
			t.Error("missing organization filter")
		}
		switch r.URL.Query().Get("pageIndex") {
		case "1":
			fmt.Fprint(w, `{"tokenDefinitions":[{"id":"first","organizationId":"org"}],"page":{"total":2}}`)
		case "2":
			fmt.Fprint(w, `{"tokenDefinitions":[{"id":"target","organizationId":"org"}],"page":{"total":2}}`)
		default:
			t.Error("unexpected page")
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	token, err := newTestClient(srv).FindOrganizationToken(t.Context(), "org", "target")
	if err != nil {
		t.Fatal(err)
	}
	if token.ID != "target" {
		t.Fatal("wrong token")
	}
}

func TestDeleteOrganizationToken(t *testing.T) {
	for _, tc := range []struct {
		name         string
		exists, noOp bool
		deleteStatus int
		wantError    bool
		wantDeletes  int
	}{
		{"revoked", true, false, 204, false, 1}, {"already absent", false, false, 204, false, 1},
		{"already absent with 404", false, false, 404, false, 1},
		{"silent refusal", true, true, 204, true, 1}, {"explicit refusal", true, true, 403, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exists := tc.exists
			deletes := 0
			srv := httptest.NewServer(tokenDeletionHandler(&exists, &deletes, tc.noOp, tc.deleteStatus))
			defer srv.Close()
			err := newTestClient(srv).DeleteOrganizationToken(t.Context(), "org", "id")
			if (err != nil) != tc.wantError || deletes != tc.wantDeletes {
				t.Fatalf("error=%v, deletes=%d", err, deletes)
			}
		})
	}
}

func tokenDeletionHandler(exists *bool, deletes *int, noOp bool, status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			*deletes++
			if !noOp {
				*exists = false
			}
			w.WriteHeader(status)
			return
		}
		if *exists {
			fmt.Fprint(w, `{"tokenDefinitions":[{"id":"id","organizationId":"org"}],"page":{"total":1}}`)
		} else {
			fmt.Fprint(w, `{"tokenDefinitions":[],"page":{"total":0}}`)
		}
	}
}

func TestDeleteOrganizationTokenRejectsIncompleteList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		fmt.Fprint(w, `{"page":{}}`)
	}))
	defer srv.Close()
	err := newTestClient(srv).DeleteOrganizationToken(t.Context(), "org", "id")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("incomplete list must produce an error, got %v", err)
	}
}
