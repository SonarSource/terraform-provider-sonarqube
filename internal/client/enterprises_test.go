package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const enterpriseAnswer = `{"id":"00000000-0000-4000-8000-000000000010","key":"my-enterprise","name":"My enterprise","avatar":"https://example.com/a.png"}`

func TestCreateEnterpriseSendsEveryField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != enterprisesPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, field := range []string{"key", "name", "avatar", "license", "organizationId"} {
			if _, ok := body[field]; !ok {
				t.Errorf("the body has no %s: %v", field, body)
			}
		}
		if body["avatar"] != "" {
			t.Errorf("avatar = %v, want an empty string", body["avatar"])
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, enterpriseAnswer)
	}))
	defer srv.Close()

	enterprise, err := newTestClient(srv).CreateEnterprise(t.Context(), EnterpriseRequest{
		Key: "my-enterprise", Name: "My enterprise", License: "test-license", OrganizationID: "org-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if enterprise.ID != "00000000-0000-4000-8000-000000000010" {
		t.Errorf("id = %q", enterprise.ID)
	}
}

func TestGetEnterprise(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		notFound bool
		wantErr  bool
	}{
		{"found", 200, enterpriseAnswer, false, false},
		{"missing avatar", 200, `{"id":"id","key":"my-enterprise","name":"n"}`, false, false},
		{"missing", 404, `{"message":"Enterprise with uuid id is not found"}`, true, true},
		{"forbidden", 403, `{}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != enterprisesPath+"/id" {
					t.Errorf("path = %s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			_, err := newTestClient(srv).GetEnterprise(t.Context(), "id")
			if (err != nil) != tc.wantErr || errors.Is(err, ErrNotFound) != tc.notFound {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestGetEnterpriseByKey(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		notFound bool
		wantErr  bool
	}{
		{"found", 200, "[" + enterpriseAnswer + "]", false, false},
		{"empty list", 200, `[]`, true, true},
		{"other key", 200, `[{"id":"id","key":"other"}]`, true, true},
		{"missing", 404, `{"message":"Enterprise with key my-enterprise is not found"}`, true, true},
		{"forbidden", 403, `{}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != enterprisesPath || r.URL.Query().Get("enterpriseKey") != "my-enterprise" {
					t.Errorf("unexpected request: %s", r.URL)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			enterprise, err := newTestClient(srv).GetEnterpriseByKey(t.Context(), "my-enterprise")
			if (err != nil) != tc.wantErr || errors.Is(err, ErrNotFound) != tc.notFound {
				t.Fatalf("error = %v", err)
			}
			if err == nil && enterprise.Avatar != "https://example.com/a.png" {
				t.Errorf("avatar = %q", enterprise.Avatar)
			}
		})
	}
}

func TestUpdateEnterpriseSendsMergePatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != enterprisesPath+"/id" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/merge-patch+json" {
			t.Errorf("content type = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"name":"New name","avatar":""}` {
			t.Errorf("body = %s", body)
		}
		fmt.Fprint(w, enterpriseAnswer)
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).UpdateEnterprise(t.Context(), "id", EnterpriseUpdate{Name: "New name"}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteEnterprise(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		wantErr bool
	}{
		{"deleted", 204, false}, {"gone", 404, false}, {"forbidden", 403, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete || r.URL.Path != enterprisesPath+"/id" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			if err := newTestClient(srv).DeleteEnterprise(t.Context(), "id"); (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestListEnterprises(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != enterprisesPath || r.URL.RawQuery != "" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		fmt.Fprint(w, "["+enterpriseAnswer+"]")
	}))
	defer srv.Close()
	enterprises, err := newTestClient(srv).ListEnterprises(t.Context())
	if err != nil || len(enterprises) != 1 {
		t.Fatalf("enterprises = %v, error = %v", enterprises, err)
	}
}

func TestIsEnterpriseKeyRefused(t *testing.T) {
	refused := &APIError{StatusCode: 400, Messages: []string{"Enterprise key is not valid"}}
	other := &APIError{StatusCode: 400, Messages: []string{"Invalid license"}}
	if !IsEnterpriseKeyRefused(fmt.Errorf("wrapped: %w", refused)) || IsEnterpriseKeyRefused(other) || IsEnterpriseKeyRefused(errors.New("x")) {
		t.Fatal("wrong classification")
	}
}
