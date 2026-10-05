package client

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFindQualityGateProjectAssociation(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		body    string
		wantID  string
		wantErr error
	}{
		"explicit": {
			body:   `{"projectAssociations":[{"id":"assoc-id","projectId":"project-uuid","qualityGateId":"gate-id","defaultFallback":false}]}`,
			wantID: "assoc-id",
		},
		"default fallback": {
			body:    `{"projectAssociations":[{"id":"assoc-id","projectId":"project-uuid","qualityGateId":"gate-id","defaultFallback":true}]}`,
			wantErr: ErrNotFound,
		},
		"empty": {
			body:    `{"projectAssociations":[]}`,
			wantErr: ErrNotFound,
		},
		"other project": {
			body:    `{"projectAssociations":[{"id":"assoc-id","projectId":"other-uuid","qualityGateId":"gate-id"}]}`,
			wantErr: ErrNotFound,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				if query.Get("projectIds") != "project-uuid" || query.Get("organizationId") != "organization-uuid" {
					t.Errorf("query = %v", query)
				}
				_, _ = w.Write([]byte(test.body))
			}))
			defer srv.Close()
			got, err := newTestClient(srv).FindQualityGateProjectAssociation(
				t.Context(), "organization-uuid", &Project{LegacyID: "project-uuid"})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil && got.ID != test.wantID {
				t.Errorf("id = %q, want %q", got.ID, test.wantID)
			}
		})
	}
}

func TestCreateQualityGateProjectAssociation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/quality-gates/project-associations" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"projectId":"project-uuid","qualityGateId":"gate-id"}` {
			t.Errorf("body = %s", body)
		}
		_, _ = w.Write([]byte(`{"id":"assoc-id","projectId":"project-uuid","qualityGateId":"gate-id"}`))
	}))
	defer srv.Close()
	got, err := newTestClient(srv).CreateQualityGateProjectAssociation(t.Context(), &Project{LegacyID: "project-uuid"}, "gate-id")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "assoc-id" {
		t.Errorf("id = %q", got.ID)
	}
}

func TestDeleteQualityGateProjectAssociation(t *testing.T) {
	t.Parallel()
	for name, status := range map[string]int{"deleted": http.StatusNoContent, "already gone": http.StatusNotFound} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete || r.URL.Path != "/quality-gates/project-associations/assoc-id" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(status)
			}))
			defer srv.Close()
			if err := newTestClient(srv).DeleteQualityGateProjectAssociation(t.Context(), "assoc-id"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
