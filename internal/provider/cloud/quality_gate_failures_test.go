package cloud

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func qualityGateFailureClient(t *testing.T, method, path string) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == method && r.URL.Path == path {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"access denied"}`))
			return
		}
		switch r.URL.Path {
		case "/organizations/organizations":
			w.Write([]byte(`[{"key":"my-org","uuidV4":"00000000-0000-4000-8000-000000000001"}]`))
		case "/quality-gates/quality-gates":
			w.Write([]byte(`{"id":"gate-id","name":"My Gate"}`))
		case "/quality-gates/quality-gates/gate-id":
			w.Write([]byte(`{"id":"gate-id","name":"My Gate"}`))
		case "/quality-gates/conditions":
			w.Write([]byte(`{"conditions":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return providertest.NewCloudClient(srv)
}

func TestQualityGateResourceCreateRetainsIDAfterAIFailure(t *testing.T) {
	t.Parallel()
	c := qualityGateFailureClient(t, http.MethodPatch, "/quality-gates/quality-gates/gate-id")
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	plan := gateModel(t, "My Gate")
	plan.AICodeAssurance = types.BoolValue(true)
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: gatePlan(t, s, plan)}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "access denied")
	if got := providertest.ReadModel[qualityGateResourceModel](t, resp.State).ID.ValueString(); got != "gate-id" {
		t.Errorf("created gate ID was lost: %q", got)
	}
}

func TestQualityGateResourceCreateReportsAPIFailure(t *testing.T) {
	t.Parallel()
	c := qualityGateFailureClient(t, http.MethodPost, "/quality-gates/quality-gates")
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: gatePlan(t, s, gateModel(t, "My Gate"))}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "access denied")
	if !resp.State.Raw.IsNull() {
		t.Error("failed create wrote state")
	}
}

func TestQualityGateResourceUpdateKeepsPriorNameOnFailure(t *testing.T) {
	t.Parallel()
	c := qualityGateFailureClient(t, http.MethodPatch, "/quality-gates/quality-gates/gate-id")
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	priorModel := gateModel(t, "Old Gate")
	priorModel.ID = types.StringValue("gate-id")
	prior := providertest.EmptyState(t, s)
	if diags := prior.Set(t.Context(), &priorModel); diags.HasError() {
		t.Fatal(diags)
	}
	plan := gateModel(t, "New Gate")
	plan.ID = types.StringValue("gate-id")
	resp := &resource.UpdateResponse{State: prior}
	r.Update(t.Context(), resource.UpdateRequest{Plan: gatePlan(t, s, plan), State: prior}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "access denied")
	if got := providertest.ReadModel[qualityGateResourceModel](t, resp.State).Name.ValueString(); got != "Old Gate" {
		t.Errorf("name after failed update = %q", got)
	}
}

func TestQualityGateResourceReadReportsConditionFailure(t *testing.T) {
	t.Parallel()
	c := qualityGateFailureClient(t, http.MethodGet, "/quality-gates/conditions")
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	model := gateModel(t, "My Gate")
	model.ID = types.StringValue("gate-id")
	prior := providertest.EmptyState(t, s)
	if diags := prior.Set(t.Context(), &model); diags.HasError() {
		t.Fatal(diags)
	}
	resp := &resource.ReadResponse{State: prior}
	r.Read(t.Context(), resource.ReadRequest{State: prior}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Cannot read quality gate conditions")
}

func TestQualityGateResourceDeleteReportsFailure(t *testing.T) {
	t.Parallel()
	c := qualityGateFailureClient(t, http.MethodDelete, "/quality-gates/quality-gates/gate-id")
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	model := gateModel(t, "My Gate")
	model.ID = types.StringValue("gate-id")
	prior := providertest.EmptyState(t, s)
	if diags := prior.Set(t.Context(), &model); diags.HasError() {
		t.Fatal(diags)
	}
	resp := &resource.DeleteResponse{State: prior}
	r.Delete(t.Context(), resource.DeleteRequest{State: prior}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "access denied")
}

func TestQualityGateResourceRejectsUnknownConditions(t *testing.T) {
	t.Parallel()
	_, c := newFakeQualityGates(t)
	r := &qualityGateResource{client: c}
	var diagnostics diag.Diagnostics
	if r.reconcileConditions(t.Context(), "gate-id", types.SetUnknown(qualityGateConditionType()), &diagnostics) {
		t.Error("unknown conditions were accepted")
	}
	providertest.AssertDiagnosticsContain(t, diagnostics, "Condition values must be known")
}

func TestQualityGateResourceRejectsUnknownMetric(t *testing.T) {
	t.Parallel()
	_, c := newFakeQualityGates(t)
	r := &qualityGateResource{client: c}
	model := gateModel(t, "My Gate", gateCondition("unknown_metric", "80"))
	var diagnostics diag.Diagnostics
	if r.reconcileConditions(t.Context(), "gate-id", model.Conditions, &diagnostics) {
		t.Error("unknown metric was accepted")
	}
	providertest.AssertDiagnosticsContain(t, diagnostics, "Cannot resolve metric unknown_metric")
}
