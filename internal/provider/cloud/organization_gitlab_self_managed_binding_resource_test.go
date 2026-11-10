package cloud

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

// plannedCreate is the plan of a new binding of my-org.
func plannedCreate() map[string]any {
	return map[string]any{
		"organization_key": "my-org",
		"server_url":       theGitLabServer,
		"api_url":          theGitLabServer + gitlabAPIPath,
		"group":            "my-group",
		"access_token":     "test-token",
	}
}

func createGitLabBinding(t *testing.T, instance *fakeGitLabInstance) *resource.CreateResponse {
	t.Helper()

	r := &organizationGitLabSelfManagedBindingResource{client: instance.start(t)}
	s := gitlabBindingResourceSchema(t)
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, plannedCreate())},
	}, resp)
	return resp
}

func TestGitLabBindingResourceCreate(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	resp := createGitLabBinding(t, instance)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	// The server needs the configuration before it accepts the binding.
	providertest.AssertCallOrder(t, instance.calls,
		"POST /gitlab-configurations", "POST /dop-translation/organization-bindings")

	// The configuration takes the UUID of the organization, and the binding
	// takes the internal identifier. Each API refuses the other one.
	if got := instance.bodies["POST /gitlab-configurations"]["organizationId"]; got != theGitLabOrganizationUUID {
		t.Errorf("the configuration call carried organizationId %v, want %q", got, theGitLabOrganizationUUID)
	}
	bind := instance.bodies["POST /dop-translation/organization-bindings"]
	for field, want := range map[string]string{
		"organizationId":               theOrganizationID,
		"devOpsPlatform":               client.PlatformGitLab,
		"dopFlavor":                    client.FlavorGitLabSelfManaged,
		"accessToken":                  "test-token",
		"devOpsPlatformOrganizationId": "my-group",
	} {
		if got := bind[field]; got != want {
			t.Errorf("the bind call carried %s = %v, want %q", field, got, want)
		}
	}

	state := providertest.ReadModel[organizationGitLabSelfManagedBindingModel](t, resp.State)
	for name, pair := range map[string][2]string{
		"id":               {state.ID.ValueString(), theBindingID},
		"configuration_id": {state.ConfigurationID.ValueString(), theConfigurationID},
		// The read reports the number of the group, and the state keeps its
		// path, so the next plan finds no difference.
		"group":        {state.Group.ValueString(), "my-group"},
		"group_id":     {state.GroupID.ValueString(), theGitLabGroupID},
		"access_token": {state.AccessToken.ValueString(), "test-token"},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", name, pair[0], pair[1])
		}
	}
}

// A refused bind keeps the configuration. It does no harm, and the next
// create takes it over.
func TestGitLabBindingResourceCreateKeepsTheConfigurationAfterARefusedBind(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.refuseBind = true
	resp := createGitLabBinding(t, instance)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a refused bind was accepted")
	}
	if len(instance.configurations) != 1 {
		t.Errorf("got %d configurations, want the one for the next create", len(instance.configurations))
	}
	// The server names the token, so the error goes to access_token.
	if got := resp.Diagnostics.Errors()[0]; !isAttributeError(got, path.Root("access_token")) {
		t.Errorf("the error is not on access_token: %v", got)
	}
}

// A bind refused because the organization is already bound to the planned
// group takes that binding over, for example after a bind that timed out
// but was stored. The planned token goes to the binding.
func TestGitLabBindingResourceCreateTakesOverABindingOfTheGroup(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.bound()
	instance.refuseBind = true
	resp := createGitLabBinding(t, instance)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if got := instance.bodies["PATCH "+bindingPath]["accessToken"]; got != "test-token" {
		t.Errorf("the binding got accessToken %v, want the planned token", got)
	}
}

// A binding to another group is not taken over.
func TestGitLabBindingResourceCreateRefusesABindingOfAnotherGroup(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.bound()
	instance.bindings[theBindingID].DevOpsPlatformURL = theGitLabServer + "/groups/other-group"
	instance.refuseBind = true
	resp := createGitLabBinding(t, instance)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a binding of another group was taken over")
	}
	if _, patched := instance.bodies["PATCH "+bindingPath]; patched {
		t.Error("the binding of another group got the token")
	}
}

// A bind that fails after the server stored the binding keeps the
// configuration, because the binding cannot reach GitLab without it, and the
// create goes on with the stored binding.
func TestGitLabBindingResourceCreateKeepsAStoredBinding(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.failBindAfterStore = true
	resp := createGitLabBinding(t, instance)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if len(instance.configurations) != 1 {
		t.Errorf("got %d configurations, want 1", len(instance.configurations))
	}
	if got := providertest.ReadModel[organizationGitLabSelfManagedBindingModel](t, resp.State).ID.ValueString(); got != theBindingID {
		t.Errorf("id = %q, want %q", got, theBindingID)
	}
	if got := instance.bodies["PATCH "+bindingPath]["accessToken"]; got != "test-token" {
		t.Errorf("the stored binding got accessToken %v, want the planned token", got)
	}
}

// A configuration that an earlier failed create left behind is taken over
// when its addresses are the planned ones.
func TestGitLabBindingResourceCreateTakesOverALeftConfiguration(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.configurations[theConfigurationID] = &client.GitLabConfiguration{
		ID:             theConfigurationID,
		OrganizationID: theGitLabOrganizationUUID,
		APIURL:         theGitLabServer + gitlabAPIPath,
		WebURL:         theGitLabServer,
	}
	resp := createGitLabBinding(t, instance)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if got := providertest.ReadModel[organizationGitLabSelfManagedBindingModel](t, resp.State).ConfigurationID.ValueString(); got != theConfigurationID {
		t.Errorf("configuration_id = %q, want %q", got, theConfigurationID)
	}
}

// A left configuration for another instance is not taken over.
func TestGitLabBindingResourceCreateRefusesAnotherConfiguration(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.configurations[theConfigurationID] = &client.GitLabConfiguration{
		ID:             theConfigurationID,
		OrganizationID: theGitLabOrganizationUUID,
		APIURL:         "https://other.example.com/api/v4",
		WebURL:         "https://other.example.com",
	}
	resp := createGitLabBinding(t, instance)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a configuration of another instance was taken over")
	}
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "https://other.example.com")
	if slices.Contains(instance.calls, "POST /dop-translation/organization-bindings") {
		t.Error("the bind was called after the configuration was refused")
	}
}

// The server names the field that it refuses, and the error goes to the
// matching attribute.
func TestGitLabBindingResourceCreateShowsTheRefusedField(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.refuseConfiguration = "apiUrl"
	resp := createGitLabBinding(t, instance)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a refused configuration was accepted")
	}
	if got := resp.Diagnostics.Errors()[0]; !isAttributeError(got, path.Root("api_url")) {
		t.Errorf("the error is not on api_url: %v", got)
	}
}

// isAttributeError reports a diagnostic that belongs to the attribute.
func isAttributeError(d diag.Diagnostic, attribute path.Path) bool {
	withPath, ok := d.(diag.DiagnosticWithPath)
	return ok && withPath.Path().Equal(attribute)
}

func readGitLabBinding(t *testing.T, instance *fakeGitLabInstance, prior map[string]any) *resource.ReadResponse {
	t.Helper()

	r := &organizationGitLabSelfManagedBindingResource{client: instance.start(t)}
	s := gitlabBindingResourceSchema(t)
	state := tfsdk.State{Schema: s, Raw: bindingValue(t, s, prior)}
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	return resp
}

func TestGitLabBindingResourceRead(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.bound()
	instance.configurations[theConfigurationID].WebURL = "https://moved.example.com"

	resp := readGitLabBinding(t, instance, boundState())

	state := providertest.ReadModel[organizationGitLabSelfManagedBindingModel](t, resp.State)
	if got, want := state.ServerURL.ValueString(), "https://moved.example.com"; got != want {
		t.Errorf("server_url = %q, want %q, the value that the server holds", got, want)
	}
	// The API never returns the token, so the state keeps it.
	if got, want := state.AccessToken.ValueString(), "test-token"; got != want {
		t.Errorf("access_token = %q, want %q", got, want)
	}
}

// A configuration that somebody deleted shows as null addresses, so that the
// plan reports the difference and the update stores it again.
func TestGitLabBindingResourceReadWithoutConfiguration(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.bound()
	delete(instance.configurations, theConfigurationID)

	resp := readGitLabBinding(t, instance, boundState())

	state := providertest.ReadModel[organizationGitLabSelfManagedBindingModel](t, resp.State)
	if !state.ServerURL.IsNull() || !state.ConfigurationID.IsNull() {
		t.Errorf("server_url = %v, configuration_id = %v, want both null", state.ServerURL, state.ConfigurationID)
	}
	if state.ID.IsNull() {
		t.Error("the binding left the state")
	}
}

func TestGitLabBindingResourceReadOfARemovedBinding(t *testing.T) {
	t.Parallel()

	resp := readGitLabBinding(t, newFakeGitLabInstance(), boundState())
	if !resp.State.Raw.IsNull() {
		t.Error("a binding that the server does not know stayed in the state")
	}
}

func updateGitLabBinding(t *testing.T, instance *fakeGitLabInstance, prior, planned map[string]any) *resource.UpdateResponse {
	t.Helper()

	r := &organizationGitLabSelfManagedBindingResource{client: instance.start(t)}
	s := gitlabBindingResourceSchema(t)
	resp := &resource.UpdateResponse{State: providertest.EmptyState(t, s)}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, planned)},
		State: tfsdk.State{Schema: s, Raw: bindingValue(t, s, prior)},
	}, resp)
	return resp
}

// A new token goes to the binding only. The configuration does not store a
// token, so it does not change.
func TestGitLabBindingResourceUpdateOfTheToken(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.bound()
	resp := updateGitLabBinding(t, instance, boundState(),
		withChanges(boundState(), map[string]any{"access_token": "new-token"}))
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	patch := instance.bodies["PATCH /dop-translation/organization-bindings/"+theBindingID]
	if len(patch) != 1 || patch["accessToken"] != "new-token" {
		t.Errorf("the binding patch = %v, want only accessToken", patch)
	}
	if slices.Contains(instance.calls, "PATCH /gitlab-configurations/"+theConfigurationID) {
		t.Error("the configuration changed for a new token")
	}
	if got := providertest.ReadModel[organizationGitLabSelfManagedBindingModel](t, resp.State).AccessToken.ValueString(); got != "new-token" {
		t.Errorf("access_token = %q, want new-token", got)
	}
}

// A new instance moves the binding first, then the configuration, as the web
// interface does.
func TestGitLabBindingResourceUpdateOfTheInstance(t *testing.T) {
	t.Parallel()

	const moved = "https://moved.example.com"
	instance := newFakeGitLabInstance()
	instance.bound()
	resp := updateGitLabBinding(t, instance, boundState(),
		withChanges(boundState(), map[string]any{"server_url": moved, "api_url": moved + gitlabAPIPath}))
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	providertest.AssertCallOrder(t, instance.calls,
		"PATCH /dop-translation/organization-bindings/"+theBindingID,
		"PATCH /gitlab-configurations/"+theConfigurationID)
	patch := instance.bodies["PATCH /dop-translation/organization-bindings/"+theBindingID]
	if patch["tenantId"] != moved || patch["devOpsPlatformUrl"] != moved+"/groups/my-group" {
		t.Errorf("the binding patch = %v, want the new tenant and group address", patch)
	}
	if got := instance.configurations[theConfigurationID].APIURL; got != moved+gitlabAPIPath {
		t.Errorf("the configuration holds apiUrl %q, want %q", got, moved+gitlabAPIPath)
	}
}

// An update after the configuration was deleted stores it again.
func TestGitLabBindingResourceUpdateStoresAMissingConfiguration(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.bound()
	delete(instance.configurations, theConfigurationID)
	prior := withChanges(boundState(), map[string]any{"server_url": nil, "api_url": nil, "configuration_id": nil})

	resp := updateGitLabBinding(t, instance, prior, boundState())
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if len(instance.configurations) != 1 {
		t.Errorf("got %d configurations, want 1", len(instance.configurations))
	}
	if _, patched := instance.bodies["PATCH /dop-translation/organization-bindings/"+theBindingID]; patched {
		t.Error("the binding moved, although only the configuration was missing")
	}
}

// An update without a configuration in the state takes over the one that
// the server still holds, as a create does.
func TestGitLabBindingResourceUpdateTakesOverTheConfiguration(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.bound()
	prior := withChanges(boundState(), map[string]any{"server_url": nil, "api_url": nil, "configuration_id": nil})

	resp := updateGitLabBinding(t, instance, prior, boundState())
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if got := providertest.ReadModel[organizationGitLabSelfManagedBindingModel](t, resp.State).ConfigurationID.ValueString(); got != theConfigurationID {
		t.Errorf("configuration_id = %q, want %q", got, theConfigurationID)
	}
}

// An update without a configuration that also moves to a new instance moves
// the binding too. The state has no server_url, so the binding tells the old
// instance.
func TestGitLabBindingResourceUpdateMovesWithoutConfiguration(t *testing.T) {
	t.Parallel()

	const moved = "https://moved.example.com"
	instance := newFakeGitLabInstance()
	instance.bound()
	delete(instance.configurations, theConfigurationID)
	prior := withChanges(boundState(), map[string]any{"server_url": nil, "api_url": nil, "configuration_id": nil})

	resp := updateGitLabBinding(t, instance, prior,
		withChanges(boundState(), map[string]any{"server_url": moved, "api_url": moved + gitlabAPIPath}))
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if got := instance.bindings[theBindingID].TenantID; got != moved {
		t.Errorf("the binding uses tenant %q, want %q", got, moved)
	}
}

// A destroy writes nothing, because the API has no operation to remove a
// binding, and it keeps the configuration that the binding needs.
func TestGitLabBindingResourceDeleteOnlyWarns(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.bound()
	r := &organizationGitLabSelfManagedBindingResource{client: instance.start(t)}
	s := gitlabBindingResourceSchema(t)
	resp := &resource.DeleteResponse{}
	r.Delete(context.Background(), resource.DeleteRequest{
		State: tfsdk.State{Schema: s, Raw: bindingValue(t, s, boundState())},
	}, resp)

	if resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("diagnostics = %v, want one warning", resp.Diagnostics)
	}
	if len(instance.calls) != 0 {
		t.Errorf("the destroy called the API: %v", instance.calls)
	}
}

func importGitLabBinding(t *testing.T, instance *fakeGitLabInstance) *resource.ImportStateResponse {
	t.Helper()

	r := &organizationGitLabSelfManagedBindingResource{client: instance.start(t)}
	s := gitlabBindingResourceSchema(t)
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "my-org"}, resp)
	return resp
}

func TestGitLabBindingResourceImport(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.bound()
	resp := importGitLabBinding(t, instance)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	state := providertest.ReadModel[organizationGitLabSelfManagedBindingModel](t, resp.State)
	if state.ID.ValueString() != theBindingID || state.OrganizationUUIDV4.ValueString() != theGitLabOrganizationUUID {
		t.Errorf("id = %v, organization_uuid_v4 = %v", state.ID, state.OrganizationUUIDV4)
	}
}

// A binding to gitlab.com has no flavor. This resource cannot manage it.
func TestGitLabBindingResourceImportOfABindingToGitLabDotCom(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.bound()
	instance.bindings[theBindingID].DopFlavor = ""

	resp := importGitLabBinding(t, instance)
	if !resp.Diagnostics.HasError() {
		t.Fatal("a binding to gitlab.com was imported")
	}
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "not bound to GitLab Self-Managed")
}

func TestGroupFromBinding(t *testing.T) {
	t.Parallel()

	binding := &client.OrganizationBinding{
		TenantID:          theGitLabServer,
		DevOpsPlatformURL: theGitLabServer + "/groups/my-group",
	}
	for name, tc := range map[string]struct {
		prior types.String
		want  string
	}{
		"after an import":           {types.StringNull(), "my-group"},
		"the same path":             {types.StringValue("my-group"), "my-group"},
		"a path in another case":    {types.StringValue("My-Group"), "My-Group"},
		"a group changed elsewhere": {types.StringValue("old-group"), "my-group"},
	} {
		if got := groupFromBinding(tc.prior, binding).ValueString(); got != tc.want {
			t.Errorf("%s: group = %q, want %q", name, got, tc.want)
		}
	}

	// An address that the provider cannot read keeps the prior value.
	other := &client.OrganizationBinding{TenantID: theGitLabServer, DevOpsPlatformURL: "https://elsewhere.example.com/x"}
	if got := groupFromBinding(types.StringValue("my-group"), other).ValueString(); got != "my-group" {
		t.Errorf("group = %q, want my-group", got)
	}
}

func TestGitLabBindingModifyPlanGivesTheDefaultAPIURL(t *testing.T) {
	t.Parallel()

	r := &organizationGitLabSelfManagedBindingResource{}
	s := gitlabBindingResourceSchema(t)
	config := withChanges(plannedCreate(), map[string]any{"api_url": nil})
	planned := tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, config)}
	resp := &resource.ModifyPlanResponse{Plan: planned}
	r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: s, Raw: bindingValue(t, s, config)},
		Plan:   planned,
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var apiURL types.String
	resp.Plan.GetAttribute(context.Background(), path.Root("api_url"), &apiURL)
	if got, want := apiURL.ValueString(), theGitLabServer+gitlabAPIPath; got != want {
		t.Errorf("api_url = %q, want %q", got, want)
	}
}

func TestImmutableGroupPlanModifier(t *testing.T) {
	t.Parallel()

	s := gitlabBindingResourceSchema(t)
	state := tfsdk.State{Schema: s, Raw: bindingValue(t, s, boundState())}
	for name, tc := range map[string]struct {
		planned string
		refused bool
	}{
		"another group":              {"other-group", true},
		"the same group in capitals": {"MY-GROUP", false},
	} {
		plan := tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, withChanges(boundState(), map[string]any{"group": tc.planned}))}
		resp := &planmodifier.StringResponse{PlanValue: types.StringValue(tc.planned)}
		immutableGroup.PlanModifyString(context.Background(), planmodifier.StringRequest{
			Path:       path.Root("group"),
			StateValue: types.StringValue("my-group"),
			PlanValue:  types.StringValue(tc.planned),
			State:      state,
			Plan:       plan,
		}, resp)
		if got := resp.Diagnostics.HasError(); got != tc.refused {
			t.Errorf("%s: refused = %v, want %v", name, got, tc.refused)
		}
	}
}

const (
	bindingPath       = "/dop-translation/organization-bindings/" + theBindingID
	configurationPath = "/gitlab-configurations/" + theConfigurationID
)

func TestGitLabBindingResourceMetadata(t *testing.T) {
	t.Parallel()

	resp := &resource.MetadataResponse{}
	NewOrganizationGitLabSelfManagedBindingResource().Metadata(context.Background(),
		resource.MetadataRequest{ProviderTypeName: "sonarqube"}, resp)
	if got, want := resp.TypeName, "sonarqube_cloud_organization_gitlab_self_managed_binding"; got != want {
		t.Errorf("TypeName = %q, want %q", got, want)
	}
}

// The server refuses a configuration without the Enterprise plan with a 403.
// The error says what the organization and the token need.
func TestGitLabBindingResourceCreateExplainsA403(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.failures["POST /gitlab-configurations"] = http.StatusForbidden
	resp := createGitLabBinding(t, instance)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a refused configuration was accepted")
	}
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Enterprise plan")
}

func TestGitLabBindingResourceReadReportsAFailure(t *testing.T) {
	t.Parallel()

	for _, call := range []string{"GET " + bindingPath, "GET /gitlab-configurations"} {
		instance := newFakeGitLabInstance()
		instance.bound()
		instance.failures[call] = http.StatusInternalServerError

		r := &organizationGitLabSelfManagedBindingResource{client: instance.start(t)}
		s := gitlabBindingResourceSchema(t)
		state := tfsdk.State{Schema: s, Raw: bindingValue(t, s, boundState())}
		resp := &resource.ReadResponse{State: state}
		r.Read(context.Background(), resource.ReadRequest{State: state}, resp)

		if !resp.Diagnostics.HasError() {
			t.Errorf("a failed %s was not reported", call)
		}
	}
}

// A binding of the planned group on another instance is a different binding,
// and the create does not take it over.
func TestGitLabBindingResourceCreateRefusesABindingOnAnotherInstance(t *testing.T) {
	t.Parallel()

	const other = "https://other.example.com"
	instance := newFakeGitLabInstance()
	instance.bound()
	delete(instance.configurations, theConfigurationID)
	instance.bindings[theBindingID].TenantID = other
	instance.bindings[theBindingID].DevOpsPlatformURL = other + "/groups/my-group"
	instance.refuseBind = true
	resp := createGitLabBinding(t, instance)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a binding on another instance was taken over")
	}
	if _, patched := instance.bodies["PATCH "+bindingPath]; patched {
		t.Error("the binding on another instance got the token")
	}
}

// A failed read of the configuration names the configuration, not the
// binding, which answered.
func TestGitLabBindingResourceReadNamesTheConfiguration(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.bound()
	instance.failures["GET /gitlab-configurations"] = http.StatusForbidden

	r := &organizationGitLabSelfManagedBindingResource{client: instance.start(t)}
	s := gitlabBindingResourceSchema(t)
	state := tfsdk.State{Schema: s, Raw: bindingValue(t, s, boundState())}
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)

	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "GitLab configuration")
}

// The configuration API takes the UUID of the organization, and the binding
// always reports it. A binding without it is an error, not a guess.
func TestGitLabBindingResourceReadRefusesABindingWithoutUUID(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.bound()
	instance.bindings[theBindingID].OrganizationUUIDV4 = ""

	r := &organizationGitLabSelfManagedBindingResource{client: instance.start(t)}
	s := gitlabBindingResourceSchema(t)
	state := tfsdk.State{Schema: s, Raw: bindingValue(t, s, boundState())}
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)

	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "no organization UUID")
}

// A token that the server refuses gives an error on access_token, and the
// configuration does not change.
func TestGitLabBindingResourceUpdateWithARefusedToken(t *testing.T) {
	t.Parallel()

	instance := newFakeGitLabInstance()
	instance.bound()
	instance.failures["PATCH "+bindingPath] = http.StatusBadRequest
	instance.failureFields["PATCH "+bindingPath] = "accessToken"
	resp := updateGitLabBinding(t, instance, boundState(),
		withChanges(boundState(), map[string]any{"access_token": "bad-token"}))

	if !resp.Diagnostics.HasError() {
		t.Fatal("a refused token was accepted")
	}
	if got := resp.Diagnostics.Errors()[0]; !isAttributeError(got, path.Root("access_token")) {
		t.Errorf("the error is not on access_token: %v", got)
	}
}

// A refused change of the configuration keeps the configuration that the
// state knows, so that the next plan starts from it.
func TestGitLabBindingResourceUpdateWithARefusedConfiguration(t *testing.T) {
	t.Parallel()

	const moved = "https://moved.example.com"
	instance := newFakeGitLabInstance()
	instance.bound()
	instance.failures["PATCH "+configurationPath] = http.StatusBadRequest
	resp := updateGitLabBinding(t, instance, boundState(),
		withChanges(boundState(), map[string]any{"server_url": moved, "api_url": moved + gitlabAPIPath}))

	if !resp.Diagnostics.HasError() {
		t.Fatal("a refused configuration was accepted")
	}
	if got := resp.Diagnostics.Errors()[0]; isAttributeError(got, path.Root("access_token")) {
		t.Errorf("an answer that names no field gave an error on access_token: %v", got)
	}
	state := providertest.ReadModel[organizationGitLabSelfManagedBindingModel](t, resp.State)
	if state.ConfigurationID.ValueString() != theConfigurationID || state.ServerURL.ValueString() != theGitLabServer {
		t.Errorf("configuration_id = %v, server_url = %v, want the prior configuration", state.ConfigurationID, state.ServerURL)
	}
}

func modifyGitLabPlan(t *testing.T, config map[string]any) types.String {
	t.Helper()

	r := &organizationGitLabSelfManagedBindingResource{}
	s := gitlabBindingResourceSchema(t)
	planned := tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, config)}
	resp := &resource.ModifyPlanResponse{Plan: planned}
	r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: s, Raw: bindingValue(t, s, config)},
		Plan:   planned,
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var apiURL types.String
	resp.Plan.GetAttribute(context.Background(), path.Root("api_url"), &apiURL)
	return apiURL
}

// A configured api_url stays, and a server_url that is not known yet gives
// an api_url that is not known either.
func TestGitLabBindingModifyPlanKeepsAConfiguredAPIURL(t *testing.T) {
	t.Parallel()

	if got := modifyGitLabPlan(t, withChanges(plannedCreate(), map[string]any{"api_url": "https://proxy.example.com/api/v4"})); got.ValueString() != "https://proxy.example.com/api/v4" {
		t.Errorf("api_url = %v, want the configured value", got)
	}
}

func TestGitLabBindingModifyPlanWithAnUnknownServer(t *testing.T) {
	t.Parallel()

	got := modifyGitLabPlan(t, withChanges(plannedCreate(), map[string]any{"api_url": nil, "server_url": tftypes.UnknownValue}))
	if !got.IsUnknown() {
		t.Errorf("api_url = %v, want unknown", got)
	}
}
