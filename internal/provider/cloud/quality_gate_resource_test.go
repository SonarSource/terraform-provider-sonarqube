package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

type fakeQualityGates struct {
	name                string
	ai                  bool
	builtIn             bool
	noAIFeature         bool
	createAIQualified   bool
	gateExists          bool
	failedConditionPost bool
	failedConditionRead bool
	paginateMetrics     bool
	metricsRequests     int
	conditions          map[int]client.QualityGateCondition
	writes              []string
}

func newFakeQualityGates(t *testing.T) (*fakeQualityGates, *client.Client) {
	t.Helper()
	fake := &fakeQualityGates{conditions: map[int]client.QualityGateCondition{}}
	srv := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(srv.Close)
	return fake, providertest.NewCloudClient(srv)
}

func (f *fakeQualityGates) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		f.writes = append(f.writes, r.Method+" "+r.URL.Path)
	}
	switch {
	case r.URL.Path == "/organizations/organizations":
		json.NewEncoder(w).Encode([]map[string]string{{"key": "my-org", "uuidV4": providertest.FakeOrganizationUUID}})
	case r.URL.Path == "/api/metrics/search":
		f.serveMetrics(w, r)
	case r.URL.Path == "/quality-gates/quality-gates":
		f.serveGateCollection(w, r)
	case r.URL.Path == "/quality-gates/quality-gates/gate-id":
		f.serveGateItem(w, r)
	case r.URL.Path == "/quality-gates/conditions":
		f.serveConditionCollection(w, r)
	case strings.HasPrefix(r.URL.Path, "/quality-gates/conditions/"):
		f.serveConditionItem(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeQualityGates) serveMetrics(w http.ResponseWriter, r *http.Request) {
	f.metricsRequests++
	items := []map[string]string{{"id": "42", "key": "new_coverage"}, {"id": "43", "key": "new_bugs"}}
	total := len(items)
	if f.paginateMetrics {
		total = 3
		if r.URL.Query().Get("p") == "2" {
			items = []map[string]string{{"id": "44", "key": "new_vulnerabilities"}}
		}
	}
	json.NewEncoder(w).Encode(map[string]any{"metrics": items, "total": total})
}

func (f *fakeQualityGates) serveGateCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		gates := []client.QualityGate{}
		if f.gateExists {
			gates = append(gates, client.QualityGate{ID: "gate-id", Name: f.name, AIQualified: f.ai, BuiltIn: f.builtIn})
		}
		json.NewEncoder(w).Encode(map[string]any{"qualityGates": gates, "page": map[string]int{"total": len(gates)}})
		return
	}
	var body map[string]string
	json.NewDecoder(r.Body).Decode(&body)
	f.name, f.gateExists, f.ai = body["name"], true, f.createAIQualified
	json.NewEncoder(w).Encode(client.QualityGate{ID: "gate-id", Name: f.name, AIQualified: f.ai})
}

func (f *fakeQualityGates) serveGateItem(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !f.gateExists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Like the API, report false when the organization has no AI Code Assurance feature.
		json.NewEncoder(w).Encode(client.QualityGate{ID: "gate-id", Name: f.name, AIQualified: f.ai && !f.noAIFeature, BuiltIn: f.builtIn})
	case http.MethodPatch:
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if name, ok := body["name"].(string); ok {
			f.name = name
		}
		if ai, ok := body["aiQualified"].(bool); ok {
			f.ai = ai
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		f.gateExists = false
		f.conditions = map[int]client.QualityGateCondition{}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (f *fakeQualityGates) serveConditionCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if f.failedConditionRead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		items := []client.QualityGateCondition{}
		for _, condition := range f.conditions {
			items = append(items, condition)
		}
		json.NewEncoder(w).Encode(map[string]any{"conditions": items})
		return
	}
	if f.failedConditionPost {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var body client.QualityGateConditionRequest
	json.NewDecoder(r.Body).Decode(&body)
	f.conditions[body.LegacyMetricID] = client.QualityGateCondition{ID: fmt.Sprintf("condition-%d", body.LegacyMetricID), QualityGateID: body.QualityGateID, LegacyMetricID: body.LegacyMetricID, Operator: body.Operator, Threshold: body.Threshold}
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeQualityGates) serveConditionItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/quality-gates/conditions/")
	for metricID, condition := range f.conditions {
		if condition.ID != id {
			continue
		}
		if r.Method == http.MethodDelete {
			delete(f.conditions, metricID)
			break
		}
		var body client.QualityGateConditionRequest
		json.NewDecoder(r.Body).Decode(&body)
		condition.Operator, condition.Threshold = body.Operator, body.Threshold
		f.conditions[metricID] = condition
		break
	}
	w.WriteHeader(http.StatusNoContent)
}

func qualityGateSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewQualityGateResource().Schema(t.Context(), resource.SchemaRequest{}, resp)
	if err := resp.Schema.ValidateImplementation(t.Context()); err != nil {
		t.Fatal(err)
	}
	return resp.Schema
}

func gateModel(t *testing.T, name string, values ...qualityGateConditionModel) qualityGateResourceModel {
	t.Helper()
	set, diags := types.SetValueFrom(t.Context(), qualityGateConditionType(), values)
	if diags.HasError() {
		t.Fatal(diags)
	}
	return qualityGateResourceModel{
		ID: types.StringUnknown(), Organization: types.StringValue("my-org"), Name: types.StringValue(name),
		AIQualified: types.BoolValue(false), Conditions: set,
	}
}

func gateCondition(metric, threshold string) qualityGateConditionModel {
	return qualityGateConditionModel{Metric: types.StringValue(metric), Operator: types.StringValue("GT"), Threshold: types.StringValue(threshold)}
}

func gatePlan(t *testing.T, s schema.Schema, model qualityGateResourceModel) tfsdk.Plan {
	t.Helper()
	plan := tfsdk.Plan{Schema: s}
	if diags := plan.Set(t.Context(), &model); diags.HasError() {
		t.Fatal(diags)
	}
	return plan
}

func TestQualityGateResourceLifecycle(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: gatePlan(t, s, gateModel(t, "My Gate", gateCondition("new_coverage", "80")))}, created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	if !fake.gateExists || fake.conditions[42].Threshold != "80" {
		t.Fatalf("create did not make the gate and condition: %+v", fake)
	}
	if got := providertest.ReadModel[qualityGateResourceModel](t, created.State).ID.ValueString(); got != "gate-id" {
		t.Errorf("id = %q", got)
	}

	prior := created.State
	changed := gateModel(t, "My Gate", gateCondition("new_coverage", "85"), gateCondition("new_bugs", "0"))
	changed.ID = types.StringValue("gate-id")
	updated := &resource.UpdateResponse{State: prior}
	r.Update(t.Context(), resource.UpdateRequest{Plan: gatePlan(t, s, changed), State: prior}, updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	if fake.conditions[42].Threshold != "85" || fake.conditions[43].Threshold != "0" {
		t.Fatalf("conditions after update = %+v", fake.conditions)
	}

	fake.writes = nil
	unchanged := &resource.UpdateResponse{State: updated.State}
	r.Update(t.Context(), resource.UpdateRequest{Plan: gatePlan(t, s, changed), State: updated.State}, unchanged)
	if unchanged.Diagnostics.HasError() {
		t.Fatal(unchanged.Diagnostics)
	}
	if len(fake.writes) != 0 {
		t.Errorf("second unchanged update wrote %v", fake.writes)
	}

	readState := assertQualityGateDrift(t, fake, r, unchanged.State)
	deleted := &resource.DeleteResponse{State: readState}
	r.Delete(t.Context(), resource.DeleteRequest{State: readState}, deleted)
	if deleted.Diagnostics.HasError() || fake.gateExists {
		t.Errorf("delete = %v, gate exists %t", deleted.Diagnostics, fake.gateExists)
	}
}

func assertQualityGateDrift(t *testing.T, fake *fakeQualityGates, r *qualityGateResource, prior tfsdk.State) tfsdk.State {
	t.Helper()
	fake.conditions[42] = client.QualityGateCondition{ID: "condition-42", QualityGateID: "gate-id", LegacyMetricID: 42, Operator: "GT", Threshold: "90"}
	read := &resource.ReadResponse{State: prior}
	r.Read(t.Context(), resource.ReadRequest{State: prior}, read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	var conditions []qualityGateConditionModel
	state := providertest.ReadModel[qualityGateResourceModel](t, read.State)
	if diags := state.Conditions.ElementsAs(t.Context(), &conditions, false); diags.HasError() {
		t.Fatal(diags)
	}
	for _, condition := range conditions {
		if condition.Metric.ValueString() == "new_coverage" && condition.Threshold.ValueString() == "90" {
			return read.State
		}
	}
	t.Errorf("read missed condition drift: %+v", conditions)
	return read.State
}

func TestQualityGateResourceImport(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.gateExists, fake.name = true, "Imported Gate"
	s := qualityGateSchema(t)
	r := &qualityGateResource{client: c}
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "my-org/Imported Gate"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	read := &resource.ReadResponse{State: resp.State}
	r.Read(t.Context(), resource.ReadRequest{State: resp.State}, read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	if got := providertest.ReadModel[qualityGateResourceModel](t, read.State).ID.ValueString(); got != "gate-id" {
		t.Errorf("id = %q", got)
	}
}

func TestQualityGateResourceRejectsBuiltInImport(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.gateExists, fake.name, fake.builtIn = true, "Sonar way", true
	s := qualityGateSchema(t)
	r := &qualityGateResource{client: c}
	imported := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "my-org/Sonar way"}, imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	read := &resource.ReadResponse{State: imported.State}
	r.Read(t.Context(), resource.ReadRequest{State: imported.State}, read)
	providertest.AssertDiagnosticsContain(t, read.Diagnostics, "Cannot manage built-in quality gate")
	if len(fake.writes) != 0 {
		t.Errorf("import wrote %v", fake.writes)
	}
}

func TestQualityGateResourceRejectsBuiltInMutation(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.gateExists, fake.name, fake.builtIn = true, "Sonar way", true
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	model := gateModel(t, "Sonar way")
	model.ID = types.StringValue("gate-id")
	prior := providertest.EmptyState(t, s)
	if diags := prior.Set(t.Context(), &model); diags.HasError() {
		t.Fatal(diags)
	}
	changed := gateModel(t, "Renamed Gate")
	changed.ID = model.ID
	updated := &resource.UpdateResponse{State: prior}
	r.Update(t.Context(), resource.UpdateRequest{Plan: gatePlan(t, s, changed), State: prior}, updated)
	providertest.AssertDiagnosticsContain(t, updated.Diagnostics, "Cannot manage built-in quality gate")
	deleted := &resource.DeleteResponse{State: prior}
	r.Delete(t.Context(), resource.DeleteRequest{State: prior}, deleted)
	providertest.AssertDiagnosticsContain(t, deleted.Diagnostics, "Cannot manage built-in quality gate")
	if !fake.gateExists || fake.name != "Sonar way" || len(fake.writes) != 0 {
		t.Errorf("built-in gate changed: %+v", fake)
	}
}

func TestQualityGateConditionRemoval(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.gateExists = true
	fake.conditions[42] = client.QualityGateCondition{ID: "condition-42", LegacyMetricID: 42, Operator: "GT", Threshold: "80"}
	fake.conditions[43] = client.QualityGateCondition{ID: "condition-43", LegacyMetricID: 43, Operator: "GT", Threshold: "0"}
	model := gateModel(t, "My Gate", gateCondition("new_bugs", "0"))
	r := &qualityGateResource{client: c}
	var diagnostics diag.Diagnostics
	if !r.reconcileConditions(t.Context(), "gate-id", model.Conditions, &diagnostics) || diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	if _, present := fake.conditions[42]; present {
		t.Error("removed condition remains")
	}
	if _, present := fake.conditions[43]; !present {
		t.Error("unrelated condition was removed")
	}
	if len(fake.writes) != 1 || fake.writes[0] != "DELETE /quality-gates/conditions/condition-42" {
		t.Errorf("writes = %v, want one condition delete", fake.writes)
	}
}

func TestQualityGateConditionMetricOnLaterPage(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.paginateMetrics = true
	r := &qualityGateResource{client: c}
	model := gateModel(t, "My Gate", gateCondition("new_coverage", "80"), gateCondition("new_vulnerabilities", "0"))
	var diagnostics diag.Diagnostics
	if !r.reconcileConditions(t.Context(), "gate-id", model.Conditions, &diagnostics) || diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	if fake.conditions[42].Threshold != "80" || fake.conditions[44].Threshold != "0" {
		t.Errorf("conditions = %+v", fake.conditions)
	}
	if fake.metricsRequests != 2 {
		t.Errorf("metrics requests = %d, want one two-page listing", fake.metricsRequests)
	}
}

func TestQualityGateResourceImportInvalid(t *testing.T) {
	t.Parallel()
	s := qualityGateSchema(t)
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	NewQualityGateResource().(*qualityGateResource).ImportState(context.Background(), resource.ImportStateRequest{ID: "invalid"}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Use <organization>/<name>")
}

func TestQualityGateResourceRenameAndAI(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.gateExists, fake.name = true, "Old Gate"
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
	plan.AIQualified = types.BoolValue(true)
	resp := &resource.UpdateResponse{State: prior}
	r.Update(t.Context(), resource.UpdateRequest{Plan: gatePlan(t, s, plan), State: prior}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if fake.name != "New Gate" || !fake.ai {
		t.Errorf("gate after update = %+v", fake)
	}
	if got := providertest.ReadModel[qualityGateResourceModel](t, resp.State); got.Name.ValueString() != "New Gate" || !got.AIQualified.ValueBool() {
		t.Errorf("state = %+v", got)
	}
}

func TestQualityGateResourceCreateWithAI(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	plan := gateModel(t, "My Gate")
	plan.AIQualified = types.BoolValue(true)
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: gatePlan(t, s, plan)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !fake.ai {
		t.Error("AI Code Assurance was not set")
	}
}

// The API creates every gate with aiQualified false today, but its contract
// does not say so. These tests cover a new gate that is qualified.
func TestQualityGateResourceCreateClearsAIQualified(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.createAIQualified = true
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: gatePlan(t, s, gateModel(t, "My Gate"))}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if fake.ai {
		t.Error("the configured false was not written")
	}
	if got := providertest.ReadModel[qualityGateResourceModel](t, resp.State); got.AIQualified.ValueBool() {
		t.Errorf("state = %+v", got)
	}
}

func TestQualityGateResourceCreateKeepsDefaultAIQualified(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.createAIQualified = true
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	plan := gateModel(t, "My Gate")
	plan.AIQualified = types.BoolUnknown()
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: gatePlan(t, s, plan)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	for _, write := range fake.writes {
		if write == "PATCH /quality-gates/quality-gates/gate-id" {
			t.Error("the flag was written although the configuration does not set it")
		}
	}
	if got := providertest.ReadModel[qualityGateResourceModel](t, resp.State); !got.AIQualified.ValueBool() {
		t.Errorf("state = %+v", got)
	}
}

func TestQualityGateResourceCreateWithoutAIFeature(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.noAIFeature = true
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	plan := gateModel(t, "My Gate", gateCondition("new_coverage", "80"))
	plan.AIQualified = types.BoolValue(true)
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: gatePlan(t, s, plan)}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "AI Code Assurance feature")
	state := providertest.ReadModel[qualityGateResourceModel](t, resp.State)
	if state.ID.ValueString() != "gate-id" || state.AIQualified.ValueBool() {
		t.Errorf("state must keep the gate and the flag that the API has: %+v", state)
	}
}

func TestQualityGateResourceUpdateWithoutAIFeature(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.gateExists, fake.name, fake.noAIFeature = true, "My Gate", true
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	priorModel := gateModel(t, "My Gate")
	priorModel.ID = types.StringValue("gate-id")
	prior := providertest.EmptyState(t, s)
	if diags := prior.Set(t.Context(), &priorModel); diags.HasError() {
		t.Fatal(diags)
	}
	plan := priorModel
	plan.AIQualified = types.BoolValue(true)
	resp := &resource.UpdateResponse{State: prior}
	r.Update(t.Context(), resource.UpdateRequest{Plan: gatePlan(t, s, plan), State: prior}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "AI Code Assurance feature")
	if got := providertest.ReadModel[qualityGateResourceModel](t, resp.State); got.AIQualified.ValueBool() {
		t.Errorf("state = %+v", got)
	}
}

func TestQualityGateResourceCreateFailureStoresKnownAI(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.failedConditionPost = true
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	plan := gateModel(t, "My Gate", gateCondition("new_coverage", "80"))
	plan.AIQualified = types.BoolUnknown()
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: gatePlan(t, s, plan)}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Cannot create quality gate condition")
	state := providertest.ReadModel[qualityGateResourceModel](t, resp.State)
	if state.ID.ValueString() != "gate-id" || state.AIQualified.IsUnknown() || state.AIQualified.IsNull() || state.AIQualified.ValueBool() {
		t.Errorf("partial state = %+v", state)
	}
}

func TestQualityGateResourceReadMissing(t *testing.T) {
	t.Parallel()
	_, c := newFakeQualityGates(t)
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	model := gateModel(t, "Gone")
	model.ID = types.StringValue("gate-id")
	prior := providertest.EmptyState(t, s)
	if diags := prior.Set(t.Context(), &model); diags.HasError() {
		t.Fatal(diags)
	}
	resp := &resource.ReadResponse{State: prior}
	r.Read(t.Context(), resource.ReadRequest{State: prior}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("missing gate remained in state")
	}
}

func TestQualityGateResourceReadConditionFailureKeepsState(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.gateExists, fake.name, fake.failedConditionRead = true, "My Gate", true
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	model := gateModel(t, "My Gate")
	model.ID = types.StringValue("gate-id")
	prior := providertest.EmptyState(t, s)
	if diags := prior.Set(t.Context(), &model); diags.HasError() {
		t.Fatal(diags)
	}
	read := &resource.ReadResponse{State: prior}
	r.Read(t.Context(), resource.ReadRequest{State: prior}, read)
	providertest.AssertDiagnosticsContain(t, read.Diagnostics, "Cannot read quality gate conditions")
	if read.State.Raw.IsNull() {
		t.Error("condition read failure removed the gate from state")
	}
}

func TestQualityGateResourceReadUnknownMetric(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.gateExists, fake.name = true, "My Gate"
	fake.conditions[99] = client.QualityGateCondition{ID: "condition-99", LegacyMetricID: 99, Operator: "GT", Threshold: "0"}
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
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "No metric key matches")
}

func TestQualityGateResourceRejectsDuplicateMetric(t *testing.T) {
	t.Parallel()
	_, c := newFakeQualityGates(t)
	r := &qualityGateResource{client: c}
	model := gateModel(t, "My Gate", gateCondition("new_coverage", "80"), gateCondition("new_coverage", "90"))
	var diagnostics diag.Diagnostics
	if r.reconcileConditions(t.Context(), "gate-id", model.Conditions, &diagnostics) {
		t.Error("duplicate metric was accepted")
	}
	providertest.AssertDiagnosticsContain(t, diagnostics, "Each metric may have one condition")
}

func TestQualityGateResourceRejectsDuplicateBeforeCreate(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	r := &qualityGateResource{client: c}
	s := qualityGateSchema(t)
	valid := gatePlan(t, s, gateModel(t, "My Gate", gateCondition("new_coverage", "80")))
	accepted := &resource.ValidateConfigResponse{}
	r.ValidateConfig(t.Context(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: s, Raw: valid.Raw}}, accepted)
	if accepted.Diagnostics.HasError() {
		t.Fatalf("valid condition was rejected: %v", accepted.Diagnostics)
	}
	model := gateModel(t, "My Gate", gateCondition("new_coverage", "80"), gateCondition("new_coverage", "90"))
	plan := gatePlan(t, s, model)
	validated := &resource.ValidateConfigResponse{}
	r.ValidateConfig(t.Context(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: s, Raw: plan.Raw}}, validated)
	providertest.AssertDiagnosticsContain(t, validated.Diagnostics, "Each metric may have one condition")
	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, created)
	providertest.AssertDiagnosticsContain(t, created.Diagnostics, "Each metric may have one condition")
	if fake.gateExists || len(fake.writes) != 0 || !created.State.Raw.IsNull() {
		t.Errorf("duplicate metric caused a partial create: %+v", fake)
	}
}

func TestQualityGateResourceMetadataAndConfigure(t *testing.T) {
	t.Parallel()
	_, c := newFakeQualityGates(t)
	r := NewQualityGateResource().(*qualityGateResource)
	metadata := &resource.MetadataResponse{}
	r.Metadata(t.Context(), resource.MetadataRequest{ProviderTypeName: "sonarqube"}, metadata)
	if metadata.TypeName != "sonarqube_cloud_quality_gate" {
		t.Errorf("type name = %q", metadata.TypeName)
	}
	configured := &resource.ConfigureResponse{}
	r.Configure(t.Context(), resource.ConfigureRequest{ProviderData: c}, configured)
	if configured.Diagnostics.HasError() || r.client != c {
		t.Errorf("configure = %v, client %p", configured.Diagnostics, r.client)
	}
}
