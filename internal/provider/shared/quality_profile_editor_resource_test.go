package shared

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func qualityProfileEditorSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewQualityProfileEditorResource().Schema(t.Context(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func TestQualityProfileEditorResourceSchema(t *testing.T) {
	t.Parallel()
	s := qualityProfileEditorSchema(t)
	if err := s.ValidateImplementation(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"organization", "quality_profile_key"} {
		if !s.Attributes[name].IsRequired() {
			t.Errorf("%s must be required", name)
		}
	}
	for _, name := range []string{"group_name", "login"} {
		if !s.Attributes[name].IsOptional() {
			t.Errorf("%s must be optional", name)
		}
	}
	if !providertest.ValidateString(t, s, "quality_profile_key", "a/b").HasError() {
		t.Error("a profile key with / was accepted, but the import ID cannot hold it")
	}
}

// fakeProfile is a quality profile of the fake.
type fakeProfile struct {
	name, language string
	builtIn        bool
}

// fakeEditors serves the profiles of the organization my-org and their
// editors. Like the server, the editor web services name a profile by its name
// and language, the searches filter with q on a part of the name or login
// without regard to case, and the writes do nothing when nothing changes.
type fakeEditors struct {
	profiles map[string]*fakeProfile
	groups   map[string]bool
	users    map[string]bool
	// editors maps "<name>/<language>" to the editors, as "group:<name>" or
	// "user:<login>".
	editors map[string]map[string]bool
	// renameAfterLookup renames the profile "custom" after each of the next
	// profile searches, to the next name, as someone could do between two
	// calls.
	renameAfterLookup []string
	// organizationGone makes each call answer that my-org does not exist.
	// editorCallsOnly limits this to the editor calls, as if the organization
	// was deleted after the lookup of the profile.
	organizationGone, editorCallsOnly bool
}

func newFakeEditors(t *testing.T) (*fakeEditors, *qualityProfileEditorResource) {
	t.Helper()
	f := &fakeEditors{
		profiles: map[string]*fakeProfile{"custom": {name: "Back/end", language: "java"}, "core": {name: "Sonar way", language: "java", builtIn: true}},
		groups:   map[string]bool{"devs": true, "devs-old": true},
		users:    map[string]bool{"alice@github": true, "alice@github-2": true},
		editors:  map[string]map[string]bool{},
	}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, &qualityProfileEditorResource{client: providertest.NewCloudClient(srv)}
}

func (f *fakeEditors) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	action := strings.TrimPrefix(r.URL.Path, "/api/qualityprofiles/")
	if organization := r.Form.Get("organization"); organization != "my-org" ||
		(f.organizationGone && (action != "search" || !f.editorCallsOnly)) {
		writeError(w, http.StatusNotFound, "No organization with key '"+organization+"'")
		return
	}
	if action == "search" {
		profiles := []map[string]any{}
		for key, p := range f.profiles {
			profiles = append(profiles, map[string]any{"key": key, "name": p.name, "language": p.language, "isBuiltIn": p.builtIn})
		}
		writeJSON(w, map[string]any{"profiles": profiles})
		if len(f.renameAfterLookup) > 0 {
			f.profiles["custom"].name, f.renameAfterLookup = f.renameAfterLookup[0], f.renameAfterLookup[1:]
		}
		return
	}
	profile := f.find(r.Form.Get("qualityProfile"), r.Form.Get("language"))
	if profile == nil {
		writeError(w, http.StatusNotFound, "Quality Profile for language '"+r.Form.Get("language")+"' and name '"+
			r.Form.Get("qualityProfile")+"' does not exist in organization 'my-org'")
		return
	}
	if profile.builtIn {
		writeError(w, http.StatusBadRequest, "Operation forbidden for built-in Quality Profile '"+profile.name+"' with language 'java'")
		return
	}
	editors := f.editorsOf(profile)
	if strings.HasPrefix(action, "search_") {
		f.search(w, r, editors, strings.TrimPrefix(action, "search_"))
		return
	}
	editor, ok := f.editor(w, r)
	if !ok {
		return
	}
	if strings.HasPrefix(action, "add_") {
		editors[editor] = true
	} else {
		delete(editors, editor)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeEditors) find(name, language string) *fakeProfile {
	for _, p := range f.profiles {
		if p.name == name && p.language == language {
			return p
		}
	}
	return nil
}

func (f *fakeEditors) editorsOf(p *fakeProfile) map[string]bool {
	key := p.name + "/" + p.language
	if f.editors[key] == nil {
		f.editors[key] = map[string]bool{}
	}
	return f.editors[key]
}

// editor reads the group or the user of a write, and answers 404 when it does
// not exist.
func (f *fakeEditors) editor(w http.ResponseWriter, r *http.Request) (string, bool) {
	if group := r.Form.Get("group"); group != "" {
		if !f.groups[group] {
			writeError(w, http.StatusNotFound, "No group with name '"+group+"' in organization 'my-org'")
			return "", false
		}
		return "group:" + group, true
	}
	login := r.Form.Get("login")
	if !f.users[login] {
		writeError(w, http.StatusNotFound, "User with login '"+login+"' is not found'")
		return "", false
	}
	return "user:" + login, true
}

func (f *fakeEditors) search(w http.ResponseWriter, r *http.Request, editors map[string]bool, kind string) {
	field, prefix, nameKey := "groups", "group:", "name"
	if kind == "users" {
		field, prefix, nameKey = "users", "user:", "login"
	}
	q := strings.ToLower(r.Form.Get("q"))
	items := []map[string]any{}
	for editor := range editors {
		name, ok := strings.CutPrefix(editor, prefix)
		if ok && strings.Contains(strings.ToLower(name), q) {
			items = append(items, map[string]any{nameKey: name, "selected": true})
		}
	}
	writeJSON(w, map[string]any{"paging": map[string]any{"total": len(items)}, field: items})
}

func editorModel(key, group, login string) qualityProfileEditorResourceModel {
	m := qualityProfileEditorResourceModel{ID: types.StringUnknown(), Organization: types.StringValue("my-org"),
		QualityProfileKey: types.StringValue(key), GroupName: types.StringNull(), Login: types.StringNull()}
	if group != "" {
		m.GroupName = types.StringValue(group)
	}
	if login != "" {
		m.Login = types.StringValue(login)
	}
	return m
}

func createEditor(t *testing.T, r *qualityProfileEditorResource, model qualityProfileEditorResourceModel) *resource.CreateResponse {
	t.Helper()
	s := qualityProfileEditorSchema(t)
	plan := tfsdk.Plan{Schema: s, Raw: providertest.EmptyState(t, s).Raw}
	if diagnostics := plan.Set(t.Context(), &model); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, resp)
	return resp
}

func readEditor(t *testing.T, r *qualityProfileEditorResource, state tfsdk.State) tfsdk.State {
	t.Helper()
	resp := &resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.State
}

func deleteEditor(t *testing.T, r *qualityProfileEditorResource, state tfsdk.State) {
	t.Helper()
	resp := &resource.DeleteResponse{State: state}
	r.Delete(t.Context(), resource.DeleteRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
}

func TestQualityProfileEditorResourceLifecycle(t *testing.T) {
	t.Parallel()
	t.Run("group", func(t *testing.T) {
		t.Parallel()
		checkEditorLifecycle(t, editorModel("custom", "devs", ""), "group:devs", "group:devs-old", "my-org/custom/group/devs")
	})
	t.Run("user", func(t *testing.T) {
		t.Parallel()
		checkEditorLifecycle(t, editorModel("custom", "", "alice@github"), "user:alice@github", "user:alice@github-2",
			"my-org/custom/user/alice@github")
	})
}

// checkEditorLifecycle creates the editor, renames the profile, removes the
// editor outside of Terraform, and deletes it. similar is an editor that the
// q filter also finds.
func checkEditorLifecycle(t *testing.T, model qualityProfileEditorResourceModel, editor, similar, id string) {
	f, r := newFakeEditors(t)
	create := createEditor(t, r, model)
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	if !f.editors["Back/end/java"][editor] {
		t.Fatalf("editors = %v", f.editors)
	}
	if got := providertest.ReadModel[qualityProfileEditorResourceModel](t, create.State); got.ID.ValueString() != id {
		t.Errorf("id = %s", got.ID)
	}
	if !readEditor(t, r, create.State).Raw.Equal(create.State.Raw) {
		t.Error("the read changed the state")
	}

	// A rename does not break the resource.
	f.profiles["custom"].name = "Backend"
	editors := f.editors["Back/end/java"]
	f.editors["Backend/java"] = editors
	delete(f.editors, "Back/end/java")
	if readEditor(t, r, create.State).Raw.IsNull() {
		t.Fatal("the read did not find the editor of the renamed profile")
	}

	// A similar editor that q also finds does not hide the removal.
	delete(editors, editor)
	editors[similar] = true
	if !readEditor(t, r, create.State).Raw.IsNull() {
		t.Error("the read did not remove the editor that was removed outside of Terraform")
	}

	editors[editor] = true
	deleteEditor(t, r, create.State)
	if editors[editor] || !editors[similar] {
		t.Errorf("editors = %v after the delete", editors)
	}
}

func TestQualityProfileEditorResourceCreateErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		model qualityProfileEditorResourceModel
		want  string
	}{
		"built-in profile": {editorModel("core", "devs", ""), "a built-in profile cannot have editors"},
		"missing profile":  {editorModel("nope", "devs", ""), "No quality profile with this key is in the organization my-org"},
		"missing group":    {editorModel("custom", "nope", ""), "the group or the user was not found in the organization"},
		"missing user":     {editorModel("custom", "", "nope"), "the group or the user was not found in the organization"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, r := newFakeEditors(t)
			providertest.AssertDiagnosticsContain(t, createEditor(t, r, tt.model).Diagnostics, tt.want)
		})
	}
}

func TestQualityProfileEditorResourceGoneProfile(t *testing.T) {
	t.Parallel()
	f, r := newFakeEditors(t)
	create := createEditor(t, r, editorModel("custom", "devs", ""))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	delete(f.profiles, "custom")
	if !readEditor(t, r, create.State).Raw.IsNull() {
		t.Error("the read did not remove the editor of a deleted profile")
	}
	deleteEditor(t, r, create.State)
}

func TestQualityProfileEditorResourceImport(t *testing.T) {
	t.Parallel()
	f, r := newFakeEditors(t)
	f.groups["team/a"] = true
	f.editors["Back/end/java"] = map[string]bool{"group:team/a": true, "user:alice@github": true}
	s := qualityProfileEditorSchema(t)
	for id, want := range map[string]qualityProfileEditorResourceModel{
		"my-org/custom/group/team/a":      editorModel("custom", "team/a", ""),
		"my-org/custom/user/alice@github": editorModel("custom", "", "alice@github"),
	} {
		resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
		r.ImportState(t.Context(), resource.ImportStateRequest{ID: id}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
		got := providertest.ReadModel[qualityProfileEditorResourceModel](t, readEditor(t, r, resp.State))
		want.ID = types.StringValue(id)
		if got != want {
			t.Errorf("%s: state = %+v, want %+v", id, got, want)
		}
	}
	for _, id := range []string{"my-org/custom/group", "my-org/custom/team/devs", "my-org//group/devs", "my-org/custom/user/", "My_Org/custom/group/devs"} {
		resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
		r.ImportState(t.Context(), resource.ImportStateRequest{ID: id}, resp)
		providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Invalid quality profile editor import ID")
	}
}

// A rename between the lookup of the profile and the editor call makes the
// name out of date. The resource reads the profile again and calls once more.
func TestQualityProfileEditorResourceRenameDuringCall(t *testing.T) {
	t.Parallel()
	f, r := newFakeEditors(t)
	create := createEditor(t, r, editorModel("custom", "devs", ""))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	f.renameAfterLookup = []string{"Renamed"}
	f.editors["Renamed/java"] = f.editors["Back/end/java"]
	if !readEditor(t, r, create.State).Raw.Equal(create.State.Raw) {
		t.Error("the read did not find the editor after the rename")
	}

	f.renameAfterLookup = []string{"Renamed again"}
	f.editors["Renamed again/java"] = f.editors["Renamed/java"]
	deleteEditor(t, r, create.State)
	if f.editors["Renamed again/java"]["group:devs"] {
		t.Error("the delete did not take the right after the rename")
	}
}

// When the name changes before each try, the call fails, and the read keeps
// the editor in the state.
func TestQualityProfileEditorResourceRenameDuringEachCall(t *testing.T) {
	t.Parallel()
	f, r := newFakeEditors(t)
	create := createEditor(t, r, editorModel("custom", "devs", ""))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	f.renameAfterLookup = []string{"First", "Second"}
	read := &resource.ReadResponse{State: create.State}
	r.Read(t.Context(), resource.ReadRequest{State: create.State}, read)
	providertest.AssertDiagnosticsContain(t, read.Diagnostics, "changed its name during the call")
	if read.State.Raw.IsNull() {
		t.Error("the read removed the editor")
	}

	f.renameAfterLookup = []string{"Third", "Fourth"}
	deleteResp := &resource.DeleteResponse{State: create.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: create.State}, deleteResp)
	providertest.AssertDiagnosticsContain(t, deleteResp.Diagnostics, "changed its name during the call")
}

func TestQualityProfileEditorResourceMissingOrganization(t *testing.T) {
	t.Parallel()
	for name, editorCallsOnly := range map[string]bool{"before the lookup": false, "after the lookup": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, r := newFakeEditors(t)
			create := createEditor(t, r, editorModel("custom", "devs", ""))
			if create.Diagnostics.HasError() {
				t.Fatal(create.Diagnostics)
			}
			f.organizationGone, f.editorCallsOnly = true, editorCallsOnly

			providertest.AssertDiagnosticsContain(t, createEditor(t, r, editorModel("custom", "devs", "")).Diagnostics,
				"The organization my-org was not found")

			read := &resource.ReadResponse{State: create.State}
			r.Read(t.Context(), resource.ReadRequest{State: create.State}, read)
			if read.Diagnostics.HasError() || read.Diagnostics.WarningsCount() != 1 || !read.State.Raw.IsNull() {
				t.Errorf("read: diagnostics = %v, removed = %v, want one warning and a removed editor",
					read.Diagnostics, read.State.Raw.IsNull())
			}

			deleteEditor(t, r, create.State)
		})
	}
}
