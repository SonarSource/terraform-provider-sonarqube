package shared

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func qualityProfileRuleSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewQualityProfileRuleResource().Schema(t.Context(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func TestQualityProfileRuleResourceSchema(t *testing.T) {
	t.Parallel()
	s := qualityProfileRuleSchema(t)
	if err := s.ValidateImplementation(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"quality_profile_key", "rule"} {
		if !s.Attributes[name].IsRequired() {
			t.Errorf("%s must be required", name)
		}
	}
	for value, valid := range map[string]bool{"java:S107": true, "c:MC-AMD3_16.1": true, "S107": false, "java:": false, "java: S1": false} {
		if got := !providertest.ValidateString(t, s, "rule", value).HasError(); got != valid {
			t.Errorf("rule %q valid = %v, want %v", value, got, valid)
		}
	}
	for value, valid := range map[string]bool{`^[a-z];x$`: true, "": false, `a"b`: false} {
		diagnostics := validateMap(t, s, "params", map[string]string{"format": value})
		if got := !diagnostics.HasError(); got != valid {
			t.Errorf("param value %q valid = %v, want %v", value, got, valid)
		}
	}
	if !providertest.ValidateString(t, s, "quality_profile_key", "a/b").HasError() {
		t.Error("a profile key with / was accepted")
	}
}

func validateMap(t *testing.T, s schema.Schema, name string, value map[string]string) diag.Diagnostics {
	t.Helper()
	attribute, ok := s.Attributes[name].(schema.MapAttribute)
	if !ok {
		t.Fatalf("%s is not a map attribute", name)
	}
	resp := &validator.MapResponse{}
	req := validator.MapRequest{Path: path.Root(name), ConfigValue: stringMap(value)}
	for _, v := range attribute.Validators {
		v.ValidateMap(t.Context(), req, resp)
	}
	return resp.Diagnostics
}

// ruleActivation is an active rule of the fake.
type ruleActivation struct {
	inherit         string
	params, impacts map[string]string
}

// fakeRules serves activate_rule, deactivate_rule and rules/search for the
// profile "child", which extends the built-in profile "core". The rules have
// the parameters and impacts of ruleDefaults. Like the server, activate_rule
// keeps the values that it does not get, and ignores an impact for a software
// quality that the rule does not have.
type fakeRules struct {
	t      *testing.T
	active map[string]map[string]*ruleActivation
	calls  []string
	// lag makes the next searches find no rule, as the index can do after a
	// write.
	lag int
	// refuseValues makes each activation with values fail.
	refuseValues bool
}

var ruleDefaults = map[string]ruleActivation{
	"java:S107":  {params: map[string]string{"max": "7", "constructorMax": "7"}, impacts: map[string]string{"MAINTAINABILITY": "MEDIUM"}},
	"java:S3329": {params: map[string]string{}, impacts: map[string]string{"SECURITY": "HIGH"}},
}

func newFakeRules(t *testing.T) (*fakeRules, *qualityProfileRuleResource) {
	t.Helper()
	f := &fakeRules{t: t, active: map[string]map[string]*ruleActivation{
		"core":  {"java:S3329": copyActivation(ruleDefaults["java:S3329"], "NONE")},
		"child": {"java:S3329": copyActivation(ruleDefaults["java:S3329"], "INHERITED")},
	}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	// No waits: a test that needs them sets them.
	return f, &qualityProfileRuleResource{client: providertest.NewCloudClient(srv)}
}

func copyActivation(a ruleActivation, inherit string) *ruleActivation {
	return &ruleActivation{inherit: inherit, params: maps.Clone(a.params), impacts: maps.Clone(a.impacts)}
}

func (f *fakeRules) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	key, rule := r.Form.Get("key"), r.Form.Get("rule")
	switch r.URL.Path {
	case "/api/rules/search":
		f.calls = append(f.calls, "search")
		f.search(w, r)
		return
	case "/api/qualityprofiles/inheritance":
		f.inheritance(w, key)
		return
	case "/api/qualityprofiles/activate_rule":
		if !f.recordActivation(w, r) {
			return
		}
	case "/api/qualityprofiles/deactivate_rule":
		f.calls = append(f.calls, "deactivate")
	}
	if key == "core" {
		writeError(w, http.StatusBadRequest, "Operation forbidden for built-in Quality Profile 'Sonar way core' with language 'java'")
		return
	}
	defaults, ok := ruleDefaults[rule]
	if !ok {
		writeError(w, http.StatusNotFound, "Rule with key '"+rule+"' not found")
		return
	}
	if strings.HasSuffix(r.URL.Path, "deactivate_rule") {
		delete(f.active["child"], rule)
	} else {
		f.activate(r, rule, defaults)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeRules) inheritance(w http.ResponseWriter, key string) {
	if _, ok := f.active[key]; !ok {
		writeError(w, http.StatusNotFound, "Quality Profile with key '"+key+"' does not exist")
		return
	}
	ancestors := []any{}
	if key == "child" {
		ancestors = append(ancestors, map[string]string{"key": "core"})
	}
	writeJSON(w, map[string]any{"ancestors": ancestors})
}

// recordActivation records the call, and reports false when refuseValues
// refused it.
func (f *fakeRules) recordActivation(w http.ResponseWriter, r *http.Request) bool {
	if r.Form.Get("reset") == "true" {
		f.calls = append(f.calls, "reset")
	} else {
		f.calls = append(f.calls, "activate "+r.Form.Get("impacts")+" "+r.Form.Get("params"))
	}
	if f.refuseValues && (r.Form.Get("impacts") != "" || r.Form.Get("params") != "") {
		writeError(w, http.StatusBadRequest, "Value 'abc' must be an integer.")
		return false
	}
	return true
}

func (f *fakeRules) activate(r *http.Request, rule string, defaults ruleActivation) {
	parent := f.active["core"][rule]
	if r.Form.Get("reset") == "true" {
		if parent != nil {
			f.active["child"][rule] = copyActivation(*parent, "INHERITED")
		} else {
			f.active["child"][rule] = copyActivation(defaults, "NONE")
		}
		return
	}
	current := f.active["child"][rule]
	withValues := r.Form.Get("impacts") != "" || r.Form.Get("params") != ""
	switch {
	case current == nil && parent != nil:
		// A new activation of a rule of the parent starts from the values
		// of the parent. With values, the server makes it the profile's own
		// rule, and not an override.
		current = copyActivation(*parent, "INHERITED")
		if withValues {
			current.inherit = "NONE"
		}
		f.active["child"][rule] = current
	case current == nil:
		current = copyActivation(defaults, "NONE")
		f.active["child"][rule] = current
	case parent != nil && withValues:
		current.inherit = "OVERRIDES"
	}
	for quality, severity := range parseList(r.Form.Get("impacts")) {
		if _, ok := current.impacts[quality]; ok {
			current.impacts[quality] = severity
		}
	}
	maps.Copy(current.params, parseList(r.Form.Get("params")))
}

func parseList(list string) map[string]string {
	values := map[string]string{}
	for pair := range strings.SplitSeq(list, ";") {
		if key, value, ok := strings.Cut(pair, "="); ok {
			values[key] = strings.Trim(value, `"`)
		}
	}
	return values
}

func (f *fakeRules) search(w http.ResponseWriter, r *http.Request) {
	profile, rule := r.Form.Get("qprofile"), r.Form.Get("rule_key")
	active := f.active[profile][rule]
	if f.lag > 0 {
		f.lag--
		active = nil
	}
	if active == nil {
		writeJSON(w, map[string]any{"total": 0, "rules": []any{}, "actives": map[string]any{}})
		return
	}
	defaults := ruleDefaults[rule]
	var ruleParams, activeParams []map[string]string
	for key, value := range defaults.params {
		ruleParams = append(ruleParams, map[string]string{"key": key, "defaultValue": value})
	}
	for key, value := range active.params {
		activeParams = append(activeParams, map[string]string{"key": key, "value": value})
	}
	writeJSON(w, map[string]any{
		"total": 1,
		"rules": []any{map[string]any{"key": rule, "params": ruleParams, "impacts": impactList(defaults.impacts)}},
		"actives": map[string]any{rule: []any{map[string]any{
			"qProfile": profile, "inherit": active.inherit, "params": activeParams, "impacts": impactList(active.impacts),
		}}},
	})
}

func impactList(impacts map[string]string) []map[string]string {
	list := []map[string]string{}
	for quality, severity := range impacts {
		list = append(list, map[string]string{"softwareQuality": quality, "severity": severity})
	}
	return list
}

func ruleModel(profile, rule string, impacts, params map[string]string) qualityProfileRuleResourceModel {
	return qualityProfileRuleResourceModel{
		ID: types.StringUnknown(), QualityProfileKey: types.StringValue(profile), Rule: types.StringValue(rule),
		Impacts: stringMap(impacts), Params: stringMap(params), Active: types.BoolValue(true),
	}
}

func rulePlan(t *testing.T, model qualityProfileRuleResourceModel) tfsdk.Plan {
	t.Helper()
	s := qualityProfileRuleSchema(t)
	plan := tfsdk.Plan{Schema: s, Raw: providertest.EmptyState(t, s).Raw}
	if diagnostics := plan.Set(t.Context(), &model); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	return plan
}

func createRule(t *testing.T, r *qualityProfileRuleResource, model qualityProfileRuleResourceModel) *resource.CreateResponse {
	t.Helper()
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, qualityProfileRuleSchema(t))}
	r.Create(t.Context(), resource.CreateRequest{Plan: rulePlan(t, model)}, resp)
	return resp
}

func readRule(t *testing.T, r *qualityProfileRuleResource, state tfsdk.State) *resource.ReadResponse {
	t.Helper()
	resp := &resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp
}

func updateRule(t *testing.T, r *qualityProfileRuleResource, state tfsdk.State, model qualityProfileRuleResourceModel) *resource.UpdateResponse {
	t.Helper()
	resp := &resource.UpdateResponse{State: state}
	r.Update(t.Context(), resource.UpdateRequest{Plan: rulePlan(t, model), State: state}, resp)
	return resp
}

func deleteRule(t *testing.T, r *qualityProfileRuleResource, state tfsdk.State) {
	t.Helper()
	resp := &resource.DeleteResponse{State: state}
	r.Delete(t.Context(), resource.DeleteRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
}

func TestQualityProfileRuleResourceLifecycleAndDrift(t *testing.T) {
	t.Parallel()
	f, r := newFakeRules(t)
	create := createRule(t, r, ruleModel("child", "java:S107",
		map[string]string{"MAINTAINABILITY": "HIGH"}, map[string]string{"max": "5"}))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	got := f.active["child"]["java:S107"]
	if got.params["max"] != "5" || got.impacts["MAINTAINABILITY"] != "HIGH" || got.inherit != "NONE" {
		t.Fatalf("active = %+v", got)
	}
	state := providertest.ReadModel[qualityProfileRuleResourceModel](t, create.State)
	if state.ID.ValueString() != "child/java:S107" {
		t.Errorf("id = %s", state.ID)
	}

	// The read keeps only the configured parameter, so the default value of
	// constructorMax shows no change.
	read := readRule(t, r, create.State)
	if !read.State.Raw.Equal(create.State.Raw) {
		t.Fatalf("read changed the state: %v", read.State.Raw)
	}

	// A change in the UI to a configured value shows; a change to a value
	// that the configuration does not set does not.
	got.params["max"], got.params["constructorMax"] = "9", "9"
	drift := providertest.ReadModel[qualityProfileRuleResourceModel](t, readRule(t, r, read.State).State)
	if drift.Params.Elements()["max"].String() != `"9"` || len(drift.Params.Elements()) != 1 {
		t.Errorf("params = %v", drift.Params)
	}

	// A removed impact resets the rule before the activation, so the impact
	// gets its default severity again.
	f.calls = nil
	update := updateRule(t, r, read.State, ruleModel("child", "java:S107", nil, map[string]string{"max": "6"}))
	if update.Diagnostics.HasError() {
		t.Fatal(update.Diagnostics)
	}
	providertest.AssertCallOrder(t, f.calls, "reset", `activate  max="6"`)
	if got := f.active["child"]["java:S107"]; got.impacts["MAINTAINABILITY"] != "MEDIUM" || got.params["max"] != "6" || got.params["constructorMax"] != "7" {
		t.Errorf("active = %+v", got)
	}

	// A change of a value only activates the rule again.
	f.calls = nil
	update = updateRule(t, r, update.State, ruleModel("child", "java:S107", nil, map[string]string{"max": "8"}))
	if update.Diagnostics.HasError() || strings.Contains(strings.Join(f.calls, ","), "reset") {
		t.Fatalf("calls = %v, diagnostics = %v", f.calls, update.Diagnostics)
	}

	deleteRule(t, r, update.State)
	if f.active["child"]["java:S107"] != nil {
		t.Error("the rule is still active")
	}
	if !readRule(t, r, update.State).State.Raw.IsNull() {
		t.Error("the read did not remove the deactivated rule")
	}
}

func TestQualityProfileRuleResourceInheritedRuleGetsParentValuesOnDelete(t *testing.T) {
	t.Parallel()
	f, r := newFakeRules(t)
	create := createRule(t, r, ruleModel("child", "java:S3329", map[string]string{"SECURITY": "LOW"}, nil))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	if got := f.active["child"]["java:S3329"]; got.inherit != "OVERRIDES" || got.impacts["SECURITY"] != "LOW" {
		t.Fatalf("active = %+v", got)
	}
	f.calls = nil
	deleteRule(t, r, create.State)
	got := f.active["child"]["java:S3329"]
	if got == nil || got.inherit != "INHERITED" || got.impacts["SECURITY"] != "HIGH" {
		t.Errorf("active = %+v", got)
	}
	if strings.Contains(strings.Join(f.calls, ","), "deactivate") {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestQualityProfileRuleResourceCreateWaitsForIndex(t *testing.T) {
	t.Parallel()
	f, r := newFakeRules(t)
	r.writeRetries = []time.Duration{0, 0, 0}
	f.lag = 2
	create := createRule(t, r, ruleModel("child", "java:S107", nil, map[string]string{"max": "5"}))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
}

func TestQualityProfileRuleResourceDeleteWaitsForIndex(t *testing.T) {
	t.Parallel()
	f, r := newFakeRules(t)
	create := createRule(t, r, ruleModel("child", "java:S107", nil, map[string]string{"max": "5"}))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	r.readRetries = []time.Duration{0}
	f.lag = 1
	deleteRule(t, r, create.State)
	if f.active["child"]["java:S107"] != nil {
		t.Error("the delete skipped the rule that the lagging search did not find")
	}
}

func TestQualityProfileRuleResourceCreateErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		model qualityProfileRuleResourceModel
		want  string
	}{
		"impact the rule does not have": {
			model: ruleModel("child", "java:S107", map[string]string{"SECURITY": "LOW"}, nil),
			want:  "The rule has no SECURITY impact. Set only the software qualities that the rule has: MAINTAINABILITY.",
		},
		"built-in profile": {
			model: ruleModel("core", "java:S107", nil, map[string]string{"max": "5"}),
			want:  "a built-in quality profile is read-only",
		},
		"unknown rule": {
			model: ruleModel("child", "java:S99999", nil, nil),
			want:  "Check the key of the quality profile and the key of the rule",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, r := newFakeRules(t)
			providertest.AssertDiagnosticsContain(t, createRule(t, r, tt.model).Diagnostics, tt.want)
		})
	}
}

func TestQualityProfileRuleResourceImport(t *testing.T) {
	t.Parallel()
	f, r := newFakeRules(t)
	f.active["child"]["java:S107"] = &ruleActivation{inherit: "NONE",
		params: map[string]string{"max": "5", "constructorMax": "7"}, impacts: map[string]string{"MAINTAINABILITY": "MEDIUM"}}
	s := qualityProfileRuleSchema(t)
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "child/java:S107"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	state := providertest.ReadModel[qualityProfileRuleResourceModel](t, resp.State)
	if !state.Impacts.IsNull() || len(state.Params.Elements()) != 1 || state.Params.Elements()["max"].String() != `"5"` {
		t.Errorf("state = %+v", state)
	}
	if !readRule(t, r, resp.State).State.Raw.Equal(resp.State.Raw) {
		t.Error("the read after the import changed the state")
	}

	// An inherited rule keeps only the values that differ from the parent.
	f.active["child"]["java:S3329"].impacts["SECURITY"] = "LOW"
	f.active["child"]["java:S3329"].inherit = "OVERRIDES"
	inherited := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "child/java:S3329"}, inherited)
	if inherited.Diagnostics.HasError() {
		t.Fatal(inherited.Diagnostics)
	}
	if got := providertest.ReadModel[qualityProfileRuleResourceModel](t, inherited.State); got.Impacts.Elements()["SECURITY"].String() != `"LOW"` {
		t.Errorf("impacts = %v", got.Impacts)
	}
	f.active["core"]["java:S3329"].impacts["SECURITY"] = "LOW"
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "child/java:S3329"}, inherited)
	if got := providertest.ReadModel[qualityProfileRuleResourceModel](t, inherited.State); !got.Impacts.IsNull() {
		t.Errorf("impacts = %v, want null for the values of the parent", got.Impacts)
	}

	for id, want := range map[string]string{
		"child":            "Invalid quality profile rule import ID",
		"/java:S107":       "Invalid quality profile rule import ID",
		"child/S107":       "Invalid quality profile rule import ID",
		"child/java:S1135": "The rule is not active in the quality profile child",
		"other/java:S107":  "The rule is not active in the quality profile other",
	} {
		resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
		r.ImportState(t.Context(), resource.ImportStateRequest{ID: id}, resp)
		providertest.AssertDiagnosticsContain(t, resp.Diagnostics, want)
	}
}

func inactiveRuleModel(profile, rule string) qualityProfileRuleResourceModel {
	m := ruleModel(profile, rule, nil, nil)
	m.Active = types.BoolValue(false)
	return m
}

// java:S3329 is active in the parent "core", so "child" gets it from its
// parent and can deactivate it.
func TestQualityProfileRuleResourceInactiveLifecycle(t *testing.T) {
	t.Parallel()
	f, r := newFakeRules(t)
	create := createRule(t, r, inactiveRuleModel("child", "java:S3329"))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	if f.active["child"]["java:S3329"] != nil {
		t.Fatal("the rule is still active")
	}
	read := readRule(t, r, create.State)
	if !read.State.Raw.Equal(create.State.Raw) {
		t.Fatalf("read changed the state: %v", read.State.Raw)
	}

	// A change in the parent activates the rule again, and the plan shows
	// it.
	f.active["child"]["java:S3329"] = copyActivation(*f.active["core"]["java:S3329"], "INHERITED")
	drift := readRule(t, r, read.State)
	if got := providertest.ReadModel[qualityProfileRuleResourceModel](t, drift.State); !got.Active.ValueBool() {
		t.Fatal("the read did not show the rule as active again")
	}
	update := updateRule(t, r, drift.State, inactiveRuleModel("child", "java:S3329"))
	if update.Diagnostics.HasError() || f.active["child"]["java:S3329"] != nil {
		t.Fatalf("diagnostics = %v, active = %v", update.Diagnostics, f.active["child"]["java:S3329"])
	}

	// The destroy activates the rule again, with the values of the parent.
	deleteRule(t, r, update.State)
	got := f.active["child"]["java:S3329"]
	if got == nil || got.inherit != "INHERITED" || got.impacts["SECURITY"] != "HIGH" {
		t.Errorf("active = %+v, want the rule of the parent", got)
	}
}

func TestQualityProfileRuleResourceActivateInactiveRuleAgain(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		model         qualityProfileRuleResourceModel
		inherit, want string
	}{
		"without values": {ruleModel("child", "java:S3329", nil, nil), "INHERITED", "HIGH"},
		"with an impact": {ruleModel("child", "java:S3329", map[string]string{"SECURITY": "LOW"}, nil), "OVERRIDES", "LOW"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, r := newFakeRules(t)
			create := createRule(t, r, inactiveRuleModel("child", "java:S3329"))
			if create.Diagnostics.HasError() {
				t.Fatal(create.Diagnostics)
			}
			update := updateRule(t, r, create.State, tt.model)
			if update.Diagnostics.HasError() {
				t.Fatal(update.Diagnostics)
			}
			got := f.active["child"]["java:S3329"]
			if got == nil || got.inherit != tt.inherit || got.impacts["SECURITY"] != tt.want {
				t.Errorf("active = %+v", got)
			}
		})
	}
}

func TestQualityProfileRuleResourceInactiveRuleThatTheParentDoesNotActivate(t *testing.T) {
	t.Parallel()
	_, r := newFakeRules(t)
	resp := createRule(t, r, inactiveRuleModel("child", "java:S107"))
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "The parent profile does not activate the rule")
	if !resp.State.Raw.IsNull() {
		t.Error("the state keeps a rule that was not deactivated")
	}
}

// A deactivation outside of Terraform of an inherited rule shows as
// active = false. An own rule that is not active any more is gone.
func TestQualityProfileRuleResourceReadsDeactivationOutside(t *testing.T) {
	t.Parallel()
	f, r := newFakeRules(t)
	create := createRule(t, r, ruleModel("child", "java:S3329", map[string]string{"SECURITY": "LOW"}, nil))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	delete(f.active["child"], "java:S3329")
	inactive := readRule(t, r, create.State).State
	if got := providertest.ReadModel[qualityProfileRuleResourceModel](t, inactive); got.Active.ValueBool() || !got.Impacts.IsNull() {
		t.Errorf("state = %+v, want active = false without values", got)
	}

	// When the parent does not activate the rule any more, there is nothing
	// to activate again on destroy.
	delete(f.active["core"], "java:S3329")
	f.calls = nil
	deleteRule(t, r, inactive)
	if slices.ContainsFunc(f.calls, func(call string) bool { return strings.HasPrefix(call, "activate") }) {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestQualityProfileRuleResourceImportsInactiveRule(t *testing.T) {
	t.Parallel()
	f, r := newFakeRules(t)
	delete(f.active["child"], "java:S3329")
	s := qualityProfileRuleSchema(t)
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "child/java:S3329"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	got := providertest.ReadModel[qualityProfileRuleResourceModel](t, resp.State)
	if got.Active.ValueBool() || !got.Impacts.IsNull() || !got.Params.IsNull() {
		t.Errorf("state = %+v", got)
	}
	if !readRule(t, r, resp.State).State.Raw.Equal(resp.State.Raw) {
		t.Error("the read after the import changed the state")
	}
}

func TestQualityProfileRuleResourceRefusesValuesOfInactiveRule(t *testing.T) {
	t.Parallel()
	_, r := newFakeRules(t)
	model := inactiveRuleModel("child", "java:S3329")
	model.Impacts = stringMap(map[string]string{"SECURITY": "LOW"})
	plan := rulePlan(t, model)
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(t.Context(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: plan.Schema, Raw: plan.Raw}}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "A rule with active = false has no values")

	model.Impacts = types.MapNull(types.StringType)
	plan = rulePlan(t, model)
	resp = &resource.ValidateConfigResponse{}
	r.ValidateConfig(t.Context(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: plan.Schema, Raw: plan.Raw}}, resp)
	if resp.Diagnostics.HasError() {
		t.Error(resp.Diagnostics)
	}
}

// A deleted profile has no rules to manage, for active = true and for
// active = false, and its destroy does nothing.
func TestQualityProfileRuleResourceDeletedProfile(t *testing.T) {
	t.Parallel()
	for name, model := range map[string]qualityProfileRuleResourceModel{
		"active":   ruleModel("child", "java:S3329", map[string]string{"SECURITY": "LOW"}, nil),
		"inactive": inactiveRuleModel("child", "java:S3329"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, r := newFakeRules(t)
			create := createRule(t, r, model)
			if create.Diagnostics.HasError() {
				t.Fatal(create.Diagnostics)
			}
			delete(f.active, "child")
			if !readRule(t, r, create.State).State.Raw.IsNull() {
				t.Error("the read did not remove the rule of a deleted profile")
			}
			deleteRule(t, r, create.State)
		})
	}
}

// When the parent does not activate the rule any more, a rule with
// active = false manages nothing, so the read removes it, and the plan shows
// that it must be created again.
func TestQualityProfileRuleResourceInactiveRuleNotInheritedAnyMore(t *testing.T) {
	t.Parallel()
	f, r := newFakeRules(t)
	create := createRule(t, r, inactiveRuleModel("child", "java:S3329"))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	delete(f.active["core"], "java:S3329")
	if !readRule(t, r, create.State).State.Raw.IsNull() {
		t.Error("the read kept a rule that the profile does not inherit any more")
	}
}

func TestQualityProfileRuleResourceAcceptsUnknownValuesOfInactiveRule(t *testing.T) {
	t.Parallel()
	_, r := newFakeRules(t)
	model := inactiveRuleModel("child", "java:S3329")
	model.Impacts = types.MapUnknown(types.StringType)
	plan := rulePlan(t, model)
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(t.Context(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: plan.Schema, Raw: plan.Raw}}, resp)
	if resp.Diagnostics.HasError() {
		t.Error(resp.Diagnostics)
	}
}

// With values, the server makes a deactivated inherited rule the profile's
// own rule. The create must keep it an override of the rule of the parent,
// so that a destroy gives it the values of the parent again.
func TestQualityProfileRuleResourceCreateOnDeactivatedInheritedRule(t *testing.T) {
	t.Parallel()
	f, r := newFakeRules(t)
	delete(f.active["child"], "java:S3329")
	create := createRule(t, r, ruleModel("child", "java:S3329", map[string]string{"SECURITY": "LOW"}, nil))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	if got := f.active["child"]["java:S3329"]; got == nil || got.inherit != "OVERRIDES" || got.impacts["SECURITY"] != "LOW" {
		t.Fatalf("active = %+v, want an override", got)
	}
	deleteRule(t, r, create.State)
	if got := f.active["child"]["java:S3329"]; got == nil || got.inherit != "INHERITED" || got.impacts["SECURITY"] != "HIGH" {
		t.Errorf("active = %+v, want the rule of the parent", got)
	}
}

// When the values fail after the activation without values, the create
// deactivates the rule again, so that nothing changes.
func TestQualityProfileRuleResourceCreateRollsBackFailedValues(t *testing.T) {
	t.Parallel()
	f, r := newFakeRules(t)
	delete(f.active["child"], "java:S3329")
	f.refuseValues = true
	resp := createRule(t, r, ruleModel("child", "java:S3329", map[string]string{"SECURITY": "LOW"}, nil))
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "must be an integer")
	if f.active["child"]["java:S3329"] != nil {
		t.Errorf("active = %+v, want the rule deactivated again", f.active["child"]["java:S3329"])
	}

	// An own rule needs one activation only, so a failure leaves nothing.
	f.calls = nil
	resp = createRule(t, r, ruleModel("child", "java:S107", nil, map[string]string{"max": "abc"}))
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "must be an integer")
	activations := 0
	for _, call := range f.calls {
		if strings.HasPrefix(call, "activate") {
			activations++
		}
	}
	if f.active["child"]["java:S107"] != nil || activations != 1 {
		t.Errorf("active = %+v, calls = %v", f.active["child"]["java:S107"], f.calls)
	}
}

// A destroy without a refresh of a rule that someone deactivated outside of
// Terraform still gives it the values of the parent.
func TestQualityProfileRuleResourceDeleteActivatesDeactivatedInheritedRule(t *testing.T) {
	t.Parallel()
	f, r := newFakeRules(t)
	r.readRetries = nil
	create := createRule(t, r, ruleModel("child", "java:S3329", map[string]string{"SECURITY": "LOW"}, nil))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	delete(f.active["child"], "java:S3329")
	deleteRule(t, r, create.State)
	if got := f.active["child"]["java:S3329"]; got == nil || got.inherit != "INHERITED" {
		t.Errorf("active = %+v, want the rule of the parent", got)
	}
}

// The update reads the server, because the state can be out of date, for
// example with -refresh=false.
func TestQualityProfileRuleResourceUpdateWithOutOfDateState(t *testing.T) {
	t.Parallel()
	t.Run("state active, rule deactivated", func(t *testing.T) {
		t.Parallel()
		f, r := newFakeRules(t)
		create := createRule(t, r, ruleModel("child", "java:S3329", map[string]string{"SECURITY": "LOW"}, nil))
		if create.Diagnostics.HasError() {
			t.Fatal(create.Diagnostics)
		}
		delete(f.active["child"], "java:S3329")
		update := updateRule(t, r, create.State, ruleModel("child", "java:S3329", map[string]string{"SECURITY": "MEDIUM"}, nil))
		if update.Diagnostics.HasError() {
			t.Fatal(update.Diagnostics)
		}
		if got := f.active["child"]["java:S3329"]; got == nil || got.inherit != "OVERRIDES" || got.impacts["SECURITY"] != "MEDIUM" {
			t.Errorf("active = %+v, want an override", got)
		}
	})
	t.Run("state inactive, rule active again", func(t *testing.T) {
		t.Parallel()
		f, r := newFakeRules(t)
		create := createRule(t, r, inactiveRuleModel("child", "java:S3329"))
		if create.Diagnostics.HasError() {
			t.Fatal(create.Diagnostics)
		}
		f.active["child"]["java:S3329"] = copyActivation(*f.active["core"]["java:S3329"], "INHERITED")
		f.refuseValues = true
		update := updateRule(t, r, create.State, ruleModel("child", "java:S3329", map[string]string{"SECURITY": "LOW"}, nil))
		providertest.AssertDiagnosticsContain(t, update.Diagnostics, "must be an integer")
		if f.active["child"]["java:S3329"] == nil {
			t.Error("the failed update deactivated a rule that was active before it")
		}
	})
}
