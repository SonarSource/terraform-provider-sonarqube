package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFindQualityGateAcrossPages(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("organizationId") != "organization-uuid" {
			t.Errorf("organizationId = %q", r.URL.Query().Get("organizationId"))
		}
		if r.URL.Query().Get("pageIndex") == "1" {
			w.Write([]byte(`{"qualityGates":[{"id":"first-id","name":"First"}],"page":{"total":2}}`))
			return
		}
		w.Write([]byte(`{"qualityGates":[{"id":"second-id","name":"Wanted"}],"page":{"total":2}}`))
	}))
	defer srv.Close()
	gate, err := newTestClient(srv).FindQualityGate(t.Context(), "organization-uuid", "Wanted")
	if err != nil {
		t.Fatal(err)
	}
	if gate.ID != "second-id" {
		t.Errorf("id = %q", gate.ID)
	}
}

func TestFindQualityGateAbsent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"qualityGates":[],"page":{"total":0}}`))
	}))
	defer srv.Close()
	_, err := newTestClient(srv).FindQualityGate(t.Context(), "organization-uuid", "absent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestQualityGateConditionWritePayloads(t *testing.T) {
	t.Parallel()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["operator"] != "GT" || body["threshold"] != "80" {
			t.Errorf("body = %v", body)
		}
		if calls == 1 {
			if r.Method != http.MethodPost || r.URL.Path != "/quality-gates/conditions" || body["qualityGateId"] != "gate-id" || body["legacyMetricId"] != float64(42) {
				t.Errorf("create request = %s %s %v", r.Method, r.URL.Path, body)
			}
		} else if r.Method != http.MethodPatch || r.URL.Path != "/quality-gates/conditions/condition-id" {
			t.Errorf("update request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := newTestClient(srv)
	if err := c.CreateQualityGateCondition(t.Context(), QualityGateConditionRequest{QualityGateID: "gate-id", LegacyMetricID: 42, Operator: "GT", Threshold: "80"}); err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateQualityGateCondition(t.Context(), "condition-id", QualityGateConditionRequest{Operator: "GT", Threshold: "80"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestQualityGateMetricMapping(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("missing authentication")
		}
		if r.URL.Query().Get("metricKeys") != "" {
			t.Error("metrics search received an unsupported metricKeys parameter")
		}
		w.Write([]byte(`{"total":2,"metrics":[{"id":"42","key":"new_coverage"},{"id":"43","key":"new_bugs"}]}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	keys, err := c.ListMetrics(t.Context())
	if err != nil || keys[42] != "new_coverage" || keys[43] != "new_bugs" {
		t.Errorf("ListMetrics = %v, %v", keys, err)
	}
}

func qualityGateCRUDHandler(t *testing.T, calls *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /quality-gates/quality-gates":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["organizationId"] != "organization-uuid" || body["name"] != "My Gate" {
				t.Errorf("create body = %v", body)
			}
			w.Write([]byte(`{"id":"gate-id","name":"My Gate"}`))
		case "GET /quality-gates/quality-gates/gate-id":
			w.Write([]byte(`{"id":"gate-id","name":"My Gate","aiQualified":true}`))
		case "PATCH /quality-gates/quality-gates/gate-id":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["aiQualified"] != true {
				t.Errorf("patch body = %v", body)
			}
			w.WriteHeader(http.StatusNoContent)
		case "GET /quality-gates/conditions":
			if r.URL.Query().Get("qualityGateId") != "gate-id" {
				t.Errorf("qualityGateId = %q", r.URL.Query().Get("qualityGateId"))
			}
			w.Write([]byte(`{"conditions":[{"id":"condition-id","qualityGateId":"gate-id","legacyMetricId":42,"operator":"LT","threshold":"80"}]}`))
		case "DELETE /quality-gates/quality-gates/gate-id", "DELETE /quality-gates/conditions/condition-id":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestQualityGateCRUDOperations(t *testing.T) {
	t.Parallel()
	calls := []string{}
	srv := httptest.NewServer(qualityGateCRUDHandler(t, &calls))
	defer srv.Close()
	c := newTestClient(srv)
	created, err := c.CreateQualityGate(t.Context(), "organization-uuid", "My Gate")
	if err != nil || created.ID != "gate-id" {
		t.Fatalf("create = %+v, %v", created, err)
	}
	gate, err := c.GetQualityGate(t.Context(), "gate-id")
	if err != nil || !gate.AIQualified {
		t.Errorf("get = %+v, %v", gate, err)
	}
	if err := c.UpdateQualityGate(t.Context(), "gate-id", map[string]any{"aiQualified": true}); err != nil {
		t.Error(err)
	}
	conditions, err := c.ListQualityGateConditions(t.Context(), "gate-id")
	if err != nil || len(conditions) != 1 || conditions[0].LegacyMetricID != 42 {
		t.Errorf("conditions = %+v, %v", conditions, err)
	}
	if err := c.DeleteQualityGateCondition(t.Context(), "condition-id"); err != nil {
		t.Error(err)
	}
	if err := c.DeleteQualityGate(t.Context(), "gate-id"); err != nil {
		t.Error(err)
	}
	if len(calls) != 6 {
		t.Errorf("calls = %v", calls)
	}
}

func TestQualityGateReadsReportFailures(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message":"unavailable"}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	if _, err := c.ListQualityGates(t.Context(), "organization-uuid"); err == nil {
		t.Error("list error was ignored")
	}
	if _, err := c.FindQualityGate(t.Context(), "organization-uuid", "My Gate"); err == nil {
		t.Error("find error was ignored")
	}
	if _, err := c.GetQualityGate(t.Context(), "gate-id"); err == nil {
		t.Error("get error was ignored")
	}
	if _, err := c.ListQualityGateConditions(t.Context(), "gate-id"); err == nil {
		t.Error("condition list error was ignored")
	}
	if _, err := c.ListMetrics(t.Context()); err == nil {
		t.Error("metric list error was ignored")
	}
}

func TestQualityGateDeletesAlreadyGone(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer srv.Close()
	c := newTestClient(srv)
	if err := c.DeleteQualityGate(t.Context(), "gate-id"); err != nil {
		t.Error(err)
	}
	if err := c.DeleteQualityGateCondition(t.Context(), "condition-id"); err != nil {
		t.Error(err)
	}
}

func TestQualityGateMetricAndPagingErrors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/quality-gates/quality-gates" {
			w.Write([]byte(`{"qualityGates":[],"page":{"total":1}}`))
			return
		}
		w.Write([]byte(`{"metrics":[],"total":1}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	if _, err := c.ListQualityGates(t.Context(), "organization-uuid"); err == nil {
		t.Error("incomplete gate page was accepted")
	}
	if _, err := c.ListMetrics(t.Context()); err == nil {
		t.Error("incomplete metric page was accepted")
	}
}

func TestQualityGateMetricsAcrossPages(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("p") == "1" {
			w.Write([]byte(`{"metrics":[{"id":"42","key":"new_coverage"}],"total":2}`))
			return
		}
		w.Write([]byte(`{"metrics":[{"id":"43","key":"new_bugs"}],"total":2}`))
	}))
	defer srv.Close()
	keys, err := newTestClient(srv).ListMetrics(t.Context())
	if err != nil || len(keys) != 2 || keys[43] != "new_bugs" {
		t.Errorf("metrics = %v, %v", keys, err)
	}
}

func TestQualityGateCreateFailure(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"access denied"}`))
	}))
	defer srv.Close()
	if _, err := newTestClient(srv).CreateQualityGate(t.Context(), "organization-uuid", "My Gate"); err == nil {
		t.Error("create failure was ignored")
	}
}

// The legacy identifier is a number in the answer of the API. A string is
// accepted too, so that a change of the type does not break a read.
func TestGetQualityGateReadsTheLegacyID(t *testing.T) {
	t.Parallel()
	for name, answer := range map[string]string{
		"number": `{"id":"gate-id","name":"My Gate","legacyId":42}`,
		"string": `{"id":"gate-id","name":"My Gate","legacyId":"42"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte(answer))
			}))
			defer srv.Close()
			gate, err := newTestClient(srv).GetQualityGate(t.Context(), "gate-id")
			if err != nil {
				t.Fatal(err)
			}
			if gate.LegacyID != "42" {
				t.Errorf("LegacyID = %q, want 42", gate.LegacyID)
			}
		})
	}
}

// The web service takes the legacy identifier, so the client reads the gate
// first.
func TestSetDefaultQualityGate(t *testing.T) {
	t.Parallel()
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /quality-gates/quality-gates/gate-id":
			w.Write([]byte(`{"id":"gate-id","name":"My Gate","legacyId":42}`))
		case "POST /api/qualitygates/set_as_default":
			if err := r.ParseForm(); err != nil {
				t.Errorf("cannot read the form: %v", err)
			}
			gotForm = r.PostForm
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	if err := newTestClient(srv).SetDefaultQualityGate(t.Context(), "my-org", "gate-id"); err != nil {
		t.Fatal(err)
	}
	if gotForm.Get("organization") != "my-org" || gotForm.Get("id") != "42" {
		t.Errorf("form = %v", gotForm)
	}
}

func TestSetDefaultQualityGateFailures(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		status   int
		answer   string
		notFound bool
	}{
		"missing gate":         {status: http.StatusNotFound, notFound: true},
		"no legacy identifier": {status: http.StatusOK, answer: `{"id":"gate-id","name":"My Gate"}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(test.status)
				w.Write([]byte(test.answer))
			}))
			defer srv.Close()
			err := newTestClient(srv).SetDefaultQualityGate(t.Context(), "my-org", "gate-id")
			if err == nil {
				t.Fatal("no error")
			}
			if errors.Is(err, ErrNotFound) != test.notFound {
				t.Errorf("error = %v, want ErrNotFound %t", err, test.notFound)
			}
		})
	}
}
