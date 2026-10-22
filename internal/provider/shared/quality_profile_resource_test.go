package shared

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func qualityProfileSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewQualityProfileResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func TestQualityProfileResourceSchema(t *testing.T) {
	t.Parallel()
	s := qualityProfileSchema(t)
	if err := s.ValidateImplementation(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"organization", "language", "name"} {
		if !s.Attributes[name].IsRequired() {
			t.Errorf("%s must be required", name)
		}
	}
	if !s.Attributes["parent_key"].IsOptional() || !s.Attributes["key"].IsComputed() {
		t.Error("parent_key must be optional and key computed")
	}
	if !providertest.ValidateString(t, s, "name", strings.Repeat("a", 101)).HasError() {
		t.Error("a name of 101 characters was accepted")
	}
}

// fakeProfiles serves the quality profile web services of the organization
// my-org. It deletes the descendants of a deleted profile, as the server does.
type fakeProfiles struct {
	t        *testing.T
	profiles map[string]*client.QualityProfile
	nextKey  int
	calls    []string
	// failParent makes each change_parent answer 400, and failDelete each
	// delete.
	failParent, failDelete bool
}

func newFakeProfiles(t *testing.T) (*fakeProfiles, *client.Client) {
	t.Helper()
	f := &fakeProfiles{t: t, profiles: map[string]*client.QualityProfile{
		"core": {Key: "core", Name: "Sonar way core", Language: "java", IsBuiltIn: true},
	}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, providertest.NewCloudClient(srv)
}

func (f *fakeProfiles) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	action := strings.TrimPrefix(r.URL.Path, "/api/qualityprofiles/")
	f.calls = append(f.calls, action)
	switch action {
	case "search":
		f.search(w, r)
		return
	case "create":
		f.nextKey++
		profile := &client.QualityProfile{Key: fmt.Sprintf("profile-%d", f.nextKey), Name: r.Form.Get("name"), Language: r.Form.Get("language")}
		f.profiles[profile.Key] = profile
		writeJSON(w, map[string]any{"profile": profile})
		return
	}
	profile := f.profiles[r.Form.Get("key")]
	switch {
	case profile == nil:
		writeError(w, http.StatusNotFound, "Quality Profile with key '"+r.Form.Get("key")+"' does not exist")
		return
	case action == "rename":
		profile.Name = r.Form.Get("name")
	case action == "change_parent" && f.failParent:
		writeError(w, http.StatusBadRequest, "Cannot set the profile as the parent since their languages differ")
		return
	case action == "change_parent" && r.Form.Get("parentKey") != "" && f.profiles[r.Form.Get("parentKey")] == nil:
		writeError(w, http.StatusNotFound, "Quality Profile with key '"+r.Form.Get("parentKey")+"' does not exist")
		return
	case action == "change_parent":
		profile.ParentKey = r.Form.Get("parentKey")
	case action == "delete" && f.failDelete:
		writeError(w, http.StatusBadRequest, "Profile '"+profile.Name+"' cannot be deleted because it is marked as default")
		return
	case action == "delete":
		f.deleteWithDescendants(profile.Key)
	default:
		f.t.Errorf("unexpected action %s", action)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeProfiles) search(w http.ResponseWriter, r *http.Request) {
	if r.Form.Get("organization") != "my-org" {
		writeError(w, http.StatusNotFound, "No organization with key '"+r.Form.Get("organization")+"'")
		return
	}
	profiles := []*client.QualityProfile{}
	for _, profile := range f.profiles {
		if (r.Form.Get("language") == "" || profile.Language == r.Form.Get("language")) &&
			(r.Form.Get("qualityProfile") == "" || profile.Name == r.Form.Get("qualityProfile")) {
			profiles = append(profiles, profile)
		}
	}
	writeJSON(w, map[string]any{"profiles": profiles})
}

func (f *fakeProfiles) deleteWithDescendants(key string) {
	delete(f.profiles, key)
	for childKey, child := range f.profiles {
		if child.ParentKey == key {
			f.deleteWithDescendants(childKey)
		}
	}
}

func backendProfile() qualityProfileResourceModel {
	return qualityProfileResourceModel{
		ID: types.StringUnknown(), Key: types.StringUnknown(),
		Organization: types.StringValue("my-org"), Language: types.StringValue("java"),
		Name: types.StringValue("Backend"), ParentKey: types.StringValue("core"),
	}
}

func profilePlan(t *testing.T, model qualityProfileResourceModel) tfsdk.Plan {
	t.Helper()
	s := qualityProfileSchema(t)
	plan := tfsdk.Plan{Schema: s, Raw: providertest.EmptyState(t, s).Raw}
	if diagnostics := plan.Set(t.Context(), &model); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	return plan
}

func createProfile(t *testing.T, r *qualityProfileResource, model qualityProfileResourceModel) *resource.CreateResponse {
	t.Helper()
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, qualityProfileSchema(t))}
	r.Create(t.Context(), resource.CreateRequest{Plan: profilePlan(t, model)}, resp)
	return resp
}

func readProfile(t *testing.T, r *qualityProfileResource, state tfsdk.State) *resource.ReadResponse {
	t.Helper()
	resp := &resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, resp)
	return resp
}

func updateProfile(t *testing.T, r *qualityProfileResource, state tfsdk.State, model qualityProfileResourceModel) *resource.UpdateResponse {
	t.Helper()
	resp := &resource.UpdateResponse{State: state}
	r.Update(t.Context(), resource.UpdateRequest{Plan: profilePlan(t, model), State: state}, resp)
	return resp
}

func TestQualityProfileResourceLifecycleAndDrift(t *testing.T) {
	t.Parallel()
	f, c := newFakeProfiles(t)
	r := &qualityProfileResource{client: c}
	create := createProfile(t, r, backendProfile())
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	created := f.profiles["profile-1"]
	if created == nil || created.Name != "Backend" || created.ParentKey != "core" {
		t.Fatalf("created = %+v", created)
	}
	providertest.AssertCallOrder(t, f.calls, "create", "change_parent")

	// A refresh right after the apply gives the plan back, so the next plan
	// shows no change.
	read := readProfile(t, r, create.State)
	if read.Diagnostics.HasError() || !read.State.Raw.Equal(create.State.Raw) {
		t.Fatalf("read changed the state: %v", read.Diagnostics)
	}

	// A rename and a removed parent outside Terraform show in the state.
	created.Name, created.ParentKey = "Backend (UI)", ""
	drift := readProfile(t, r, read.State)
	got := providertest.ReadModel[qualityProfileResourceModel](t, drift.State)
	if got.Name.ValueString() != "Backend (UI)" || !got.ParentKey.IsNull() {
		t.Fatalf("drift = %+v", got)
	}

	// The update renames the profile and sets the parent again.
	planned := backendProfile()
	planned.ID, planned.Key = types.StringValue("profile-1"), types.StringValue("profile-1")
	planned.Name = types.StringValue("Backend services")
	update := updateProfile(t, r, drift.State, planned)
	if update.Diagnostics.HasError() || created.Name != "Backend services" || created.ParentKey != "core" {
		t.Fatalf("update: %v, profile = %+v", update.Diagnostics, created)
	}
}

func TestQualityProfileResourceRemovesParentAndDeletes(t *testing.T) {
	t.Parallel()
	f, c := newFakeProfiles(t)
	r := &qualityProfileResource{client: c}
	create := createProfile(t, r, backendProfile())
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}

	// A configuration without parent_key removes the parent, and makes no
	// rename.
	f.calls = nil
	planned := backendProfile()
	planned.ID, planned.Key = types.StringValue("profile-1"), types.StringValue("profile-1")
	planned.ParentKey = types.StringNull()
	update := updateProfile(t, r, create.State, planned)
	if update.Diagnostics.HasError() || f.profiles["profile-1"].ParentKey != "" || strings.Join(f.calls, ",") != "change_parent" {
		t.Fatalf("remove parent: %v, calls = %v, profile = %+v", update.Diagnostics, f.calls, f.profiles["profile-1"])
	}
	if after := readProfile(t, r, update.State); !after.State.Raw.Equal(update.State.Raw) {
		t.Fatal("a refresh after the update changed the state")
	}

	del := &resource.DeleteResponse{State: update.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: update.State}, del)
	if del.Diagnostics.HasError() || f.profiles["profile-1"] != nil {
		t.Fatalf("delete: %v", del.Diagnostics)
	}
	if gone := readProfile(t, r, update.State); gone.Diagnostics.HasError() || !gone.State.Raw.IsNull() {
		t.Fatalf("a read of a deleted profile kept it: %v", gone.Diagnostics)
	}
}

func TestQualityProfileResourceDeletesProfileWhenParentFails(t *testing.T) {
	t.Parallel()
	f, c := newFakeProfiles(t)
	f.failParent = true
	resp := createProfile(t, &qualityProfileResource{client: c}, backendProfile())
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "languages differ")
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "The provider deleted the new profile")
	if f.profiles["profile-1"] != nil || !resp.State.Raw.IsNull() {
		t.Fatalf("the profile stays: %+v", f.profiles["profile-1"])
	}
}

func TestQualityProfileResourceKeepsProfileWhenRollbackFails(t *testing.T) {
	t.Parallel()
	f, c := newFakeProfiles(t)
	f.failParent, f.failDelete = true, true
	resp := createProfile(t, &qualityProfileResource{client: c}, backendProfile())
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "the state keeps it without a parent")
	got := providertest.ReadModel[qualityProfileResourceModel](t, resp.State)
	if got.Key.ValueString() != "profile-1" || !got.ParentKey.IsNull() {
		t.Fatalf("state = %+v", got)
	}
}

func TestQualityProfileResourceKeepsRenameWhenParentFails(t *testing.T) {
	t.Parallel()
	f, c := newFakeProfiles(t)
	r := &qualityProfileResource{client: c}
	model := backendProfile()
	model.ParentKey = types.StringNull()
	create := createProfile(t, r, model)
	f.failParent = true
	planned := backendProfile()
	planned.ID, planned.Key = types.StringValue("profile-1"), types.StringValue("profile-1")
	planned.Name = types.StringValue("Backend services")
	update := updateProfile(t, r, create.State, planned)
	providertest.AssertDiagnosticsContain(t, update.Diagnostics, "Cannot change the parent")
	got := providertest.ReadModel[qualityProfileResourceModel](t, update.State)
	if got.Name.ValueString() != "Backend services" || !got.ParentKey.IsNull() {
		t.Fatalf("state = %+v", got)
	}
}

// A replacement of the parent deletes the child on the server, and the
// update of the child then finds no profile.
func TestQualityProfileResourceExplainsProfileDeletedWithParent(t *testing.T) {
	t.Parallel()
	f, c := newFakeProfiles(t)
	r := &qualityProfileResource{client: c}
	create := createProfile(t, r, backendProfile())
	planned := backendProfile()
	planned.ID, planned.Key = types.StringValue("profile-1"), types.StringValue("profile-1")
	planned.ParentKey = types.StringValue("new-parent")

	// A parent that does not exist keeps the error of the server.
	update := updateProfile(t, r, create.State, planned)
	providertest.AssertDiagnosticsContain(t, update.Diagnostics, "Quality Profile with key 'new-parent' does not exist")

	f.profiles["new-parent"] = &client.QualityProfile{Key: "new-parent", Name: "Backend 2", Language: "java"}
	delete(f.profiles, "profile-1")
	update = updateProfile(t, r, create.State, planned)
	providertest.AssertDiagnosticsContain(t, update.Diagnostics, "replace_triggered_by")
}

func TestQualityProfileResourceExplainsDefaultOnDelete(t *testing.T) {
	t.Parallel()
	f, c := newFakeProfiles(t)
	r := &qualityProfileResource{client: c}
	create := createProfile(t, r, backendProfile())
	f.failDelete = true
	resp := &resource.DeleteResponse{State: create.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: create.State}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Set a different default profile first")
}

func TestQualityProfileResourceWarnsWhenOrganizationIsGone(t *testing.T) {
	t.Parallel()
	_, c := newFakeProfiles(t)
	r := &qualityProfileResource{client: c}
	model := backendProfile()
	model.ID, model.Key = types.StringValue("profile-1"), types.StringValue("profile-1")
	model.Organization = types.StringValue("other-org")
	s := qualityProfileSchema(t)
	state := tfsdk.State{Schema: s, Raw: profilePlan(t, model).Raw}
	resp := readProfile(t, r, state)
	if resp.Diagnostics.HasError() || len(resp.Diagnostics.Warnings()) != 1 || !resp.State.Raw.IsNull() {
		t.Fatalf("diagnostics = %v", resp.Diagnostics)
	}
}

func TestQualityProfileResourceImport(t *testing.T) {
	t.Parallel()
	f, c := newFakeProfiles(t)
	f.profiles["profile-7"] = &client.QualityProfile{Key: "profile-7", Name: "Team/Backend", Language: "java", ParentKey: "core"}
	r := &qualityProfileResource{client: c}
	s := qualityProfileSchema(t)
	importProfile := func(id string) *resource.ImportStateResponse {
		resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
		resp.State.Raw = providertest.SchemaValue(t, s, nil)
		r.ImportState(t.Context(), resource.ImportStateRequest{ID: id}, resp)
		return resp
	}
	resp := importProfile("my-org/java/Team/Backend")
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	got := providertest.ReadModel[qualityProfileResourceModel](t, readProfile(t, r, resp.State).State)
	if got.ID.ValueString() != "profile-7" || got.Key.ValueString() != "profile-7" || got.Organization.ValueString() != "my-org" ||
		got.Language.ValueString() != "java" || got.Name.ValueString() != "Team/Backend" || got.ParentKey.ValueString() != "core" {
		t.Errorf("imported %+v", got)
	}
	providertest.AssertDiagnosticsContain(t, importProfile("my-org/java/team/backend").Diagnostics, "with the same case")
	providertest.AssertDiagnosticsContain(t, importProfile("my-org/java/Sonar way core").Diagnostics, "built-in")
	providertest.AssertDiagnosticsContain(t, importProfile("other-org/java/Backend").Diagnostics, "other-org was not found")
	for _, id := range []string{"my-org/java", "my-org//Backend", "my-org/java/", "My Org/java/Backend"} {
		providertest.AssertDiagnosticsContain(t, importProfile(id).Diagnostics, "Use <organization>/<language>/<name>")
	}
}

func readProfileDataSource(t *testing.T, c *client.Client, name string) *datasource.ReadResponse {
	t.Helper()
	d := &qualityProfileDataSource{client: c}
	schemaResponse := &datasource.SchemaResponse{}
	d.Schema(t.Context(), datasource.SchemaRequest{}, schemaResponse)
	s := schemaResponse.Schema
	if err := s.ValidateImplementation(t.Context()); err != nil {
		t.Fatal(err)
	}
	objectType := s.Type().TerraformType(context.Background()).(tftypes.Object)
	values := map[string]tftypes.Value{}
	for attribute, attributeType := range objectType.AttributeTypes {
		values[attribute] = tftypes.NewValue(attributeType, nil)
	}
	values["organization"] = tftypes.NewValue(tftypes.String, "my-org")
	values["language"] = tftypes.NewValue(tftypes.String, "java")
	values["name"] = tftypes.NewValue(tftypes.String, name)
	config := tfsdk.Config{Schema: s, Raw: tftypes.NewValue(objectType, values)}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objectType, nil)}}
	d.Read(t.Context(), datasource.ReadRequest{Config: config}, resp)
	return resp
}

func TestQualityProfileDataSourceReadsBuiltInAndCustomProfiles(t *testing.T) {
	t.Parallel()
	f, c := newFakeProfiles(t)
	f.profiles["profile-7"] = &client.QualityProfile{Key: "profile-7", Name: "Backend", Language: "java", ParentKey: "core", IsDefault: true}

	resp := readProfileDataSource(t, c, "Sonar way core")
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	core := providertest.ReadModel[qualityProfileDataSourceModel](t, resp.State)
	if core.Key.ValueString() != "core" || core.ID.ValueString() != "core" || !core.IsBuiltIn.ValueBool() ||
		core.IsDefault.ValueBool() || !core.ParentKey.IsNull() {
		t.Errorf("built-in profile = %+v", core)
	}

	backend := providertest.ReadModel[qualityProfileDataSourceModel](t, readProfileDataSource(t, c, "Backend").State)
	if backend.IsBuiltIn.ValueBool() || !backend.IsDefault.ValueBool() || backend.ParentKey.ValueString() != "core" {
		t.Errorf("custom profile = %+v", backend)
	}

	providertest.AssertDiagnosticsContain(t, readProfileDataSource(t, c, "Frontend").Diagnostics, "No java quality profile")
}
