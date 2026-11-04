package shared

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func projectQualityProfileSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewProjectQualityProfileResource().Schema(t.Context(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func TestProjectQualityProfileResourceSchema(t *testing.T) {
	t.Parallel()
	s := projectQualityProfileSchema(t)
	if err := s.ValidateImplementation(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"organization", "project_key", "quality_profile_key"} {
		if !s.Attributes[name].IsRequired() {
			t.Errorf("%s must be required", name)
		}
	}
	if !s.Attributes["language"].IsComputed() || s.Attributes["language"].IsOptional() {
		t.Error("language must be computed only")
	}
}

// fakeProjectProfiles serves the quality profiles of the organization my-org
// and the assignments of the project my-project. Like the server, add_project
// replaces the profile of the same language, remove_project of a profile
// that the project does not use does nothing, and the search with a project
// gives the default profile when the language has no assignment.
type fakeProjectProfiles struct {
	// languages maps a profile key to its language.
	languages map[string]string
	defaults  map[string]string
	// assigned maps a language to the profile key that the project uses.
	assigned map[string]string
	calls    []string
	// otherOrganization makes the search with a project answer the 404 of a
	// project of a different organization.
	otherOrganization bool
	// planRefused makes remove_project answer the refusal of an organization
	// whose plan does not permit custom profiles.
	planRefused bool
}

func newFakeProjectProfiles(t *testing.T) (*fakeProjectProfiles, *projectQualityProfileResource) {
	t.Helper()
	f := &fakeProjectProfiles{
		languages: map[string]string{"java-default": "java", "java-a": "java", "java-b": "java", "py-a": "py", "py-default": "py"},
		defaults:  map[string]string{"java": "java-default", "py": "py-default"},
		assigned:  map[string]string{},
	}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, &projectQualityProfileResource{client: providertest.NewCloudClient(srv)}
}

func (f *fakeProjectProfiles) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	key, project := r.Form.Get("key"), r.Form.Get("project")
	if r.URL.Path == "/api/qualityprofiles/search" {
		f.search(w, r)
		return
	}
	f.calls = append(f.calls, strings.TrimPrefix(r.URL.Path, "/api/qualityprofiles/")+" "+key)
	if project != "my-project" {
		writeError(w, http.StatusNotFound, "Component key '"+project+"' not found")
		return
	}
	language, ok := f.languages[key]
	if !ok {
		writeError(w, http.StatusNotFound, "Quality Profile with key '"+key+"' does not exist")
		return
	}
	if f.planRefused && strings.HasSuffix(r.URL.Path, "remove_project") {
		writeError(w, http.StatusBadRequest, "Organization 'organization-uuid' is not allowed to perform this action")
		return
	}
	switch r.URL.Path {
	case "/api/qualityprofiles/add_project":
		f.assigned[language] = key
	case "/api/qualityprofiles/remove_project":
		if f.assigned[language] == key {
			delete(f.assigned, language)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeProjectProfiles) search(w http.ResponseWriter, r *http.Request) {
	if r.Form.Get("organization") != "my-org" {
		writeError(w, http.StatusNotFound, "No organization with key '"+r.Form.Get("organization")+"'")
		return
	}
	profiles := []map[string]any{}
	if project := r.Form.Get("project"); project != "" {
		if project != "my-project" {
			writeError(w, http.StatusNotFound, "Component key '"+project+"' not found")
			return
		}
		if f.otherOrganization {
			writeError(w, http.StatusNotFound, "Project '"+project+"' not found")
			return
		}
		language := r.Form.Get("language")
		key, ok := f.assigned[language]
		if !ok {
			key = f.defaults[language]
		}
		if key != "" {
			profiles = append(profiles, map[string]any{"key": key, "language": language, "isDefault": key == f.defaults[language]})
		}
		writeJSON(w, map[string]any{"profiles": profiles})
		return
	}
	for key, language := range f.languages {
		profiles = append(profiles, map[string]any{"key": key, "language": language})
	}
	writeJSON(w, map[string]any{"profiles": profiles})
}

func projectProfileModel(project, key string) projectQualityProfileResourceModel {
	return projectQualityProfileResourceModel{
		ID: types.StringUnknown(), Organization: types.StringValue("my-org"), ProjectKey: types.StringValue(project),
		QualityProfileKey: types.StringValue(key), Language: types.StringUnknown(),
	}
}

func projectProfilePlan(t *testing.T, model projectQualityProfileResourceModel) tfsdk.Plan {
	t.Helper()
	s := projectQualityProfileSchema(t)
	plan := tfsdk.Plan{Schema: s, Raw: providertest.EmptyState(t, s).Raw}
	if diagnostics := plan.Set(t.Context(), &model); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	return plan
}

func createProjectProfile(t *testing.T, r *projectQualityProfileResource, model projectQualityProfileResourceModel) *resource.CreateResponse {
	t.Helper()
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, projectQualityProfileSchema(t))}
	r.Create(t.Context(), resource.CreateRequest{Plan: projectProfilePlan(t, model)}, resp)
	return resp
}

func readProjectProfile(t *testing.T, r *projectQualityProfileResource, state tfsdk.State) tfsdk.State {
	t.Helper()
	resp := &resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.State
}

func updateProjectProfile(t *testing.T, r *projectQualityProfileResource, state tfsdk.State, model projectQualityProfileResourceModel) *resource.UpdateResponse {
	t.Helper()
	resp := &resource.UpdateResponse{State: state}
	r.Update(t.Context(), resource.UpdateRequest{Plan: projectProfilePlan(t, model), State: state}, resp)
	return resp
}

func modifyProjectProfilePlan(t *testing.T, r *projectQualityProfileResource, state tfsdk.State, model projectQualityProfileResourceModel) *resource.ModifyPlanResponse {
	t.Helper()
	plan := projectProfilePlan(t, model)
	resp := &resource.ModifyPlanResponse{Plan: plan}
	r.ModifyPlan(t.Context(), resource.ModifyPlanRequest{Plan: plan, State: state}, resp)
	return resp
}

func TestProjectQualityProfileResourceLifecycleAndDrift(t *testing.T) {
	t.Parallel()
	f, r := newFakeProjectProfiles(t)
	create := createProjectProfile(t, r, projectProfileModel("my-project", "java-a"))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	if f.assigned["java"] != "java-a" {
		t.Fatalf("assigned = %v", f.assigned)
	}
	state := providertest.ReadModel[projectQualityProfileResourceModel](t, create.State)
	if state.ID.ValueString() != "my-org/my-project/java" || state.Language.ValueString() != "java" {
		t.Errorf("state = %+v", state)
	}
	read := readProjectProfile(t, r, create.State)
	if !read.Raw.Equal(create.State.Raw) {
		t.Fatalf("read changed the state: %v", read.Raw)
	}

	// A different profile in the UI, and a reset to the default, show as
	// drift.
	f.assigned["java"] = "java-b"
	if got := providertest.ReadModel[projectQualityProfileResourceModel](t, readProjectProfile(t, r, read)); got.QualityProfileKey.ValueString() != "java-b" {
		t.Errorf("quality_profile_key = %s, want java-b", got.QualityProfileKey)
	}
	delete(f.assigned, "java")
	drift := readProjectProfile(t, r, read)
	if got := providertest.ReadModel[projectQualityProfileResourceModel](t, drift); got.QualityProfileKey.ValueString() != "java-default" {
		t.Errorf("quality_profile_key = %s, want java-default", got.QualityProfileKey)
	}

	update := updateProjectProfile(t, r, drift, projectProfileModel("my-project", "java-b"))
	if update.Diagnostics.HasError() {
		t.Fatal(update.Diagnostics)
	}
	if f.assigned["java"] != "java-b" {
		t.Errorf("assigned = %v", f.assigned)
	}

	deleteResp := &resource.DeleteResponse{State: update.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: update.State}, deleteResp)
	if deleteResp.Diagnostics.HasError() {
		t.Fatal(deleteResp.Diagnostics)
	}
	if _, ok := f.assigned["java"]; ok {
		t.Errorf("assigned = %v after the delete", f.assigned)
	}
}

func TestProjectQualityProfileResourceModifyPlan(t *testing.T) {
	t.Parallel()
	_, r := newFakeProjectProfiles(t)
	create := createProjectProfile(t, r, projectProfileModel("my-project", "java-a"))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}

	same := modifyProjectProfilePlan(t, r, create.State, projectProfileModel("my-project", "java-b"))
	if same.Diagnostics.HasError() || len(same.RequiresReplace) != 0 {
		t.Fatalf("diagnostics = %v, replace = %v", same.Diagnostics, same.RequiresReplace)
	}
	if got := providertest.ReadModel[projectQualityProfileResourceModel](t, tfsdk.State(same.Plan)); got.ID.ValueString() != "my-org/my-project/java" {
		t.Errorf("planned id = %s", got.ID)
	}

	other := modifyProjectProfilePlan(t, r, create.State, projectProfileModel("my-project", "py-a"))
	if other.Diagnostics.HasError() || !other.RequiresReplace.Contains(path.Root("quality_profile_key")) {
		t.Fatalf("diagnostics = %v, replace = %v", other.Diagnostics, other.RequiresReplace)
	}
	if got := providertest.ReadModel[projectQualityProfileResourceModel](t, tfsdk.State(other.Plan)); got.Language.ValueString() != "py" ||
		got.ID.ValueString() != "my-org/my-project/py" {
		t.Errorf("plan = %+v", got)
	}

	unknownKey := projectProfileModel("my-project", "")
	unknownKey.QualityProfileKey = types.StringUnknown()
	unknown := modifyProjectProfilePlan(t, r, create.State, unknownKey)
	if unknown.Diagnostics.HasError() {
		t.Fatal(unknown.Diagnostics)
	}
	if got := providertest.ReadModel[projectQualityProfileResourceModel](t, tfsdk.State(unknown.Plan)); !got.Language.IsUnknown() || len(unknown.RequiresReplace) != 0 {
		t.Errorf("plan = %+v, replace = %v", got, unknown.RequiresReplace)
	}

	missing := modifyProjectProfilePlan(t, r, providertest.EmptyState(t, projectQualityProfileSchema(t)), projectProfileModel("my-project", "nope"))
	providertest.AssertDiagnosticsContain(t, missing.Diagnostics, "Quality profile nope not found")
}

// When the plan did not know the profile key, the language changes in the
// update, and the update removes the assignment of the earlier language.
func TestProjectQualityProfileResourceUpdateToOtherLanguageRemovesEarlierAssignment(t *testing.T) {
	t.Parallel()
	f, r := newFakeProjectProfiles(t)
	create := createProjectProfile(t, r, projectProfileModel("my-project", "java-a"))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	f.calls = nil
	update := updateProjectProfile(t, r, create.State, projectProfileModel("my-project", "py-a"))
	if update.Diagnostics.HasError() {
		t.Fatal(update.Diagnostics)
	}
	providertest.AssertCallOrder(t, f.calls, "add_project py-a", "remove_project java-a")
	if _, ok := f.assigned["java"]; ok || f.assigned["py"] != "py-a" {
		t.Errorf("assigned = %v", f.assigned)
	}
	if got := providertest.ReadModel[projectQualityProfileResourceModel](t, update.State); got.ID.ValueString() != "my-org/my-project/py" {
		t.Errorf("id = %s", got.ID)
	}
}

func TestProjectQualityProfileResourceErrorsAndGone(t *testing.T) {
	t.Parallel()
	f, r := newFakeProjectProfiles(t)
	providertest.AssertDiagnosticsContain(t, createProjectProfile(t, r, projectProfileModel("other-project", "java-a")).Diagnostics,
		"No project other-project is in the organization my-org")
	providertest.AssertDiagnosticsContain(t, createProjectProfile(t, r, projectProfileModel("my-project", "nope")).Diagnostics,
		"Quality profile nope not found")

	create := createProjectProfile(t, r, projectProfileModel("my-project", "java-a"))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	gone := create.State
	if diagnostics := gone.SetAttribute(t.Context(), path.Root("project_key"), "other-project"); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	if !readProjectProfile(t, r, gone).Raw.IsNull() {
		t.Error("the read did not remove the assignment of a missing project")
	}
	// A project of a different organization is not in the organization of
	// the state either.
	f.otherOrganization = true
	if !readProjectProfile(t, r, create.State).Raw.IsNull() {
		t.Error("the read did not remove the assignment of a project of a different organization")
	}
	f.otherOrganization = false

	deleteResp := &resource.DeleteResponse{State: gone}
	r.Delete(t.Context(), resource.DeleteRequest{State: gone}, deleteResp)
	if deleteResp.Diagnostics.HasError() {
		t.Errorf("the delete of a missing project failed: %v", deleteResp.Diagnostics)
	}
}

func TestProjectQualityProfileResourceImport(t *testing.T) {
	t.Parallel()
	f, r := newFakeProjectProfiles(t)
	f.assigned["py"] = "py-a"
	s := projectQualityProfileSchema(t)
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "my-org/my-project/py"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	got := providertest.ReadModel[projectQualityProfileResourceModel](t, readProjectProfile(t, r, resp.State))
	if got.QualityProfileKey.ValueString() != "py-a" || got.Language.ValueString() != "py" || got.ID.ValueString() != "my-org/my-project/py" {
		t.Errorf("state = %+v", got)
	}

	for _, id := range []string{"my-org/my-project", "my-org/my-project/", "/my-project/java", "my-org/my project/java", "my-org/my-project/java/x"} {
		resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
		r.ImportState(t.Context(), resource.ImportStateRequest{ID: id}, resp)
		providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Invalid project quality profile import ID")
	}
}

// When remove_project fails after add_project, the state keeps the earlier
// assignment, so that the next apply removes it again.
func TestProjectQualityProfileResourceUpdateKeepsEarlierAssignmentWhenRemoveFails(t *testing.T) {
	t.Parallel()
	f, r := newFakeProjectProfiles(t)
	create := createProjectProfile(t, r, projectProfileModel("my-project", "java-a"))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	f.planRefused = true
	update := updateProjectProfile(t, r, create.State, projectProfileModel("my-project", "py-a"))
	providertest.AssertDiagnosticsContain(t, update.Diagnostics, "The next apply tries again")
	if !update.State.Raw.Equal(create.State.Raw) {
		t.Errorf("state = %v, want the earlier assignment", update.State.Raw)
	}
}

func TestProjectQualityProfileResourceDeleteWarnsWhenPlanRefuses(t *testing.T) {
	t.Parallel()
	f, r := newFakeProjectProfiles(t)
	create := createProjectProfile(t, r, projectProfileModel("my-project", "java-a"))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	f.planRefused = true
	resp := &resource.DeleteResponse{State: create.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: create.State}, resp)
	if resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != 1 {
		t.Errorf("diagnostics = %v, want one warning", resp.Diagnostics)
	}
}
