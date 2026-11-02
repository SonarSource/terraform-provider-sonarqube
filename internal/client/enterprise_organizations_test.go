package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const relationAnswer = `{"enterpriseOrganizationId":"relation-id","enterpriseId":"enterprise-id","organizationId":"org-legacy-id","organizationUuidV4":"org-uuid"}`

func TestListEnterpriseOrganizations(t *testing.T) {
	for _, tc := range []struct {
		name      string
		list      func(c *Client) ([]EnterpriseOrganization, error)
		parameter string
	}{
		{"of enterprise", func(c *Client) ([]EnterpriseOrganization, error) {
			return c.ListEnterpriseOrganizationsOfEnterprise(t.Context(), "enterprise-id")
		}, "enterpriseId=enterprise-id"},
		{"of organization", func(c *Client) ([]EnterpriseOrganization, error) {
			return c.ListEnterpriseOrganizationsOfOrganization(t.Context(), "org-legacy-id")
		}, "organizationId=org-legacy-id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != enterpriseOrganizationsPath || r.URL.RawQuery != tc.parameter {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				fmt.Fprint(w, "["+relationAnswer+"]")
			}))
			defer srv.Close()
			relations, err := tc.list(newTestClient(srv))
			if err != nil {
				t.Fatal(err)
			}
			want := EnterpriseOrganization{ID: "relation-id", EnterpriseID: "enterprise-id", OrganizationID: "org-legacy-id", OrganizationUUIDV4: "org-uuid"}
			if len(relations) != 1 || relations[0] != want {
				t.Errorf("relations = %+v", relations)
			}
		})
	}
}

func TestAttachEnterpriseOrganization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != enterpriseOrganizationsPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["enterpriseId"] != "enterprise-id" || body["organizationId"] != "org-legacy-id" || len(body) != 2 {
			t.Errorf("body = %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, relationAnswer)
	}))
	defer srv.Close()

	relation, err := newTestClient(srv).AttachEnterpriseOrganization(t.Context(), "enterprise-id", "org-legacy-id")
	if err != nil {
		t.Fatal(err)
	}
	if relation.ID != "relation-id" {
		t.Errorf("id = %q", relation.ID)
	}
}

func TestAttachEnterpriseOrganizationAlreadyInEnterprise(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"message":"Organization is already part of an enterprise"}`)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).AttachEnterpriseOrganization(t.Context(), "enterprise-id", "org-legacy-id")
	if !IsAlreadyInEnterprise(err) {
		t.Errorf("error = %v", err)
	}
	if IsAlreadyInEnterprise(newAPIError(http.StatusBadRequest, []byte(`{"message":"Invalid"}`))) {
		t.Error("another 400 matches")
	}
}

func TestDetachEnterpriseOrganization(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		wantErr bool
	}{
		{"detached", 204, false}, {"gone", 404, false}, {"forbidden", 403, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete || r.URL.Path != enterpriseOrganizationsPath+"/relation-id" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			err := newTestClient(srv).DetachEnterpriseOrganization(t.Context(), "relation-id")
			if (err != nil) != tc.wantErr {
				t.Errorf("error = %v", err)
			}
		})
	}
}

func TestEnterpriseOrganizationMatches(t *testing.T) {
	relation := EnterpriseOrganization{OrganizationID: "org-legacy-id", OrganizationUUIDV4: "org-uuid"}
	for _, tc := range []struct {
		org  Organization
		want bool
	}{
		{Organization{ID: "org-legacy-id"}, true},
		{Organization{ID: "other", UUIDV4: "org-uuid"}, true},
		{Organization{ID: "other", UUIDV4: "other-uuid"}, false},
		{Organization{ID: "other"}, false},
	} {
		if got := relation.Matches(&tc.org); got != tc.want {
			t.Errorf("Matches(%+v) = %v", tc.org, got)
		}
	}
}
