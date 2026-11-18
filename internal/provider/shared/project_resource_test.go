package shared

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func projectResourceSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewProjectResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func TestProjectResourceSchema(t *testing.T) {
	t.Parallel()

	s := projectResourceSchema(t)
	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"key", "name", "organization"} {
		if !s.Attributes[name].IsRequired() {
			t.Errorf("%s must be required", name)
		}
	}
	if !s.Attributes["id"].IsComputed() {
		t.Error("id must be computed")
	}
	// Computed keeps the main branch of a project whose configuration omits it.
	if mainBranch := s.Attributes["main_branch"]; !mainBranch.IsOptional() || !mainBranch.IsComputed() {
		t.Error("main_branch must be optional and computed")
	}
	resp := &resource.MetadataResponse{}
	NewProjectResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "sonarqube"}, resp)
	if resp.TypeName != "sonarqube_project" {
		t.Errorf("type name = %q, want sonarqube_project", resp.TypeName)
	}
}

func TestProjectResourceConfigureNeedsCloud(t *testing.T) {
	t.Parallel()

	r := &projectResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: client.New(client.Config{
		URL: "https://dev.example.io", Product: client.ProductServer,
	})}, resp)
	// Every resource shares the summary, so it must name no resource, and the
	// detail must say which one asked.
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "sonarqube_project resource")
	if got := resp.Diagnostics.Errors()[0].Summary(); got != "This needs SonarQube Cloud" {
		t.Errorf("summary = %q, want This needs SonarQube Cloud", got)
	}
	if r.client != nil {
		t.Error("the resource retained a Server client")
	}
}

type fakeProjectInstance struct {
	projects map[string]string
	// mainBranches holds the main branch of each project. A new project has
	// "master".
	mainBranches map[string]string
	// failBranches makes the branch actions fail with a 403.
	failBranches bool
	calls        []string
}

func newFakeProjectInstance() *fakeProjectInstance {
	return &fakeProjectInstance{projects: map[string]string{}, mainBranches: map[string]string{}}
}

// addProject adds a project with the main branch "master".
func (f *fakeProjectInstance) addProject(key, name string) {
	f.projects[key] = name
	f.mainBranches[key] = "master"
}

func (f *fakeProjectInstance) start(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls = append(f.calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/projects/create":
			_ = r.ParseForm()
			if r.PostForm.Get("organization") != "my-org" {
				t.Errorf("organization = %q, want my-org", r.PostForm.Get("organization"))
			}
			f.addProject(r.PostForm.Get("project"), r.PostForm.Get("name"))
			w.WriteHeader(http.StatusNoContent)
		case "/organizations/organizations":
			if r.URL.Query().Get("organizationKey") != "my-org" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`[{"id":"organization-id","uuidV4":"` + providertest.FakeOrganizationUUID + `","key":"my-org"}]`))
		case "/projects/projects":
			if r.URL.Query().Get("organizationIds") != providertest.FakeOrganizationUUID {
				t.Errorf("organizationIds = %q, want %q", r.URL.Query().Get("organizationIds"), providertest.FakeOrganizationUUID)
			}
			key := r.URL.Query().Get("keys")
			projects := []client.Project{}
			if name, found := f.projects[key]; found {
				projects = append(projects, client.Project{Key: key, Name: name, OrganizationID: providertest.FakeOrganizationUUID})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"projects": projects})
		case "/api/projects/delete":
			_ = r.ParseForm()
			delete(f.projects, r.PostForm.Get("project"))
			delete(f.mainBranches, r.PostForm.Get("project"))
			w.WriteHeader(http.StatusNoContent)
		case "/api/project_branches/list", "/api/project_branches/rename":
			f.serveBranches(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return providertest.NewCloudClient(srv)
}

func (f *fakeProjectInstance) serveBranches(w http.ResponseWriter, r *http.Request) {
	if f.failBranches {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if r.URL.Path == "/api/project_branches/rename" {
		_ = r.ParseForm()
		f.mainBranches[r.PostForm.Get("project")] = r.PostForm.Get("name")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	name, found := f.mainBranches[r.URL.Query().Get("project")]
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	branches := []map[string]any{{"name": "feature", "isMain": false}, {"name": name, "isMain": true}}
	_ = json.NewEncoder(w).Encode(map[string]any{"branches": branches})
}

func TestProjectResourceLifecycle(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectInstance()
	r := &projectResource{client: instance.start(t)}
	s := projectResourceSchema(t)
	configured := providertest.SchemaValue(t, s, map[string]string{
		"key": "my-project", "name": "My Project", "organization": "my-org",
	})
	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: configured}}, created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	model := providertest.ReadModel[projectResourceModel](t, created.State)
	if model.ID.ValueString() != "my-project" || model.MainBranch.ValueString() != "master" {
		t.Errorf("id = %q, main_branch = %q, want my-project and master", model.ID.ValueString(), model.MainBranch.ValueString())
	}

	refreshed := &resource.ReadResponse{State: providertest.EmptyState(t, s)}
	r.Read(context.Background(), resource.ReadRequest{State: created.State}, refreshed)
	if refreshed.Diagnostics.HasError() {
		t.Fatal(refreshed.Diagnostics)
	}
	if !created.State.Raw.Equal(refreshed.State.Raw) {
		t.Errorf("unchanged refresh changed state: before %v, after %v", created.State.Raw, refreshed.State.Raw)
	}

	deleted := &resource.DeleteResponse{}
	r.Delete(context.Background(), resource.DeleteRequest{State: refreshed.State}, deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	if _, found := instance.projects["my-project"]; found {
		t.Error("the project remains after delete")
	}
	providertest.AssertCallOrder(t, instance.calls, "/api/projects/create", "/projects/projects")
	providertest.AssertCallOrder(t, instance.calls, "/projects/projects", "/api/projects/delete")
}

func TestProjectResourceReadAfterOutsideDeletion(t *testing.T) {
	t.Parallel()

	r := &projectResource{client: newFakeProjectInstance().start(t)}
	s := projectResourceSchema(t)
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "my-project", "key": "my-project", "name": "My Project", "organization": "my-org",
	})}
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("deleted project stayed in state")
	}
}

func TestProjectResourceReadAfterOrganizationDeletion(t *testing.T) {
	t.Parallel()

	r := &projectResource{client: newFakeProjectInstance().start(t)}
	s := projectResourceSchema(t)
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "my-project", "key": "my-project", "name": "My Project", "organization": "gone-org",
	})}
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("a project of a deleted organization stayed in state")
	}
}

// withUnknown returns the value with the attribute unknown, as a plan holds a
// computed attribute that the configuration omits.
func withUnknown(t *testing.T, raw tftypes.Value, attribute string) tftypes.Value {
	t.Helper()

	var values map[string]tftypes.Value
	if err := raw.As(&values); err != nil {
		t.Fatal(err)
	}
	values[attribute] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	return tftypes.NewValue(raw.Type(), values)
}

func createProject(t *testing.T, instance *fakeProjectInstance, attributes map[string]string) *resource.CreateResponse {
	t.Helper()

	r := &projectResource{client: instance.start(t)}
	s := projectResourceSchema(t)
	plan := providertest.SchemaValue(t, s, attributes)
	if _, configured := attributes["main_branch"]; !configured {
		plan = withUnknown(t, plan, "main_branch")
	}
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: plan}}, resp)
	return resp
}

func TestProjectResourceCreateReadsTheMainBranchOfAProjectWithoutOne(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectInstance()
	resp := createProject(t, instance, map[string]string{"key": "my-project", "name": "My Project", "organization": "my-org"})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := providertest.ReadModel[projectResourceModel](t, resp.State).MainBranch.ValueString(); got != "master" {
		t.Errorf("main_branch = %q, want master", got)
	}
	if want := []string{"/api/projects/create", "/api/project_branches/list"}; !slices.Equal(instance.calls, want) {
		t.Errorf("calls = %v, want %v", instance.calls, want)
	}
}

func TestProjectResourceCreateRenamesTheMainBranch(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectInstance()
	resp := createProject(t, instance, map[string]string{
		"key": "my-project", "name": "My Project", "organization": "my-org", "main_branch": "main",
	})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := providertest.ReadModel[projectResourceModel](t, resp.State).MainBranch.ValueString(); got != "main" {
		t.Errorf("main_branch = %q, want main", got)
	}
	if got := instance.mainBranches["my-project"]; got != "main" {
		t.Errorf("main branch of the project = %q, want main", got)
	}
	if want := []string{"/api/projects/create", "/api/project_branches/rename"}; !slices.Equal(instance.calls, want) {
		t.Errorf("calls = %v, want %v", instance.calls, want)
	}
}

// An error taints the resource when the state holds it, and then the next
// apply replaces the project. Without the state, the project would be
// left behind.
func TestProjectResourceCreateKeepsTheProjectWhenTheRenameFails(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectInstance()
	instance.failBranches = true
	resp := createProject(t, instance, map[string]string{
		"key": "my-project", "name": "My Project", "organization": "my-org", "main_branch": "main",
	})
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Cannot set the main branch of the project my-project")
	if resp.State.Raw.IsNull() {
		t.Error("the state lost a project that exists")
	}
}

// A read back that failed would taint a project that Terraform made well.
func TestProjectResourceCreateWarnsWhenTheMainBranchReadFails(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectInstance()
	instance.failBranches = true
	resp := createProject(t, instance, map[string]string{"key": "my-project", "name": "My Project", "organization": "my-org"})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if len(resp.Diagnostics.Warnings()) != 1 {
		t.Errorf("diagnostics = %v, want one warning", resp.Diagnostics)
	}
	if model := providertest.ReadModel[projectResourceModel](t, resp.State); model.ID.ValueString() != "my-project" || !model.MainBranch.IsNull() {
		t.Errorf("id = %q, main_branch = %v, want my-project and null", model.ID.ValueString(), model.MainBranch)
	}
}

func projectState(t *testing.T, s schema.Schema, mainBranch string) tfsdk.State {
	t.Helper()

	attributes := map[string]string{
		"id": "my-project", "key": "my-project", "name": "My Project", "organization": "my-org",
	}
	if mainBranch != "" {
		attributes["main_branch"] = mainBranch
	}
	return tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, attributes)}
}

func TestProjectResourceReadRetainsStateOnFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	r := &projectResource{client: providertest.NewCloudClient(srv)}
	s := projectResourceSchema(t)
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "my-project", "key": "my-project", "name": "My Project", "organization": "my-org",
	})}
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Cannot read the project")
	if !resp.State.Raw.Equal(state.Raw) {
		t.Error("a failed read changed the prior state")
	}
}

func TestProjectResourceDeleteReportsFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	r := &projectResource{client: providertest.NewCloudClient(srv)}
	s := projectResourceSchema(t)
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "my-project", "key": "my-project", "name": "My Project", "organization": "my-org",
	})}
	resp := &resource.DeleteResponse{}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Cannot delete the project")
}

func TestProjectResourceReadKeepsTheConfiguredName(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectInstance()
	instance.addProject("my-project", "Renamed in the user interface")
	r := &projectResource{client: instance.start(t)}
	s := projectResourceSchema(t)
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "my-project", "key": "my-project", "name": "My Project", "organization": "my-org",
	})}

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := providertest.ReadModel[projectResourceModel](t, resp.State).Name.ValueString(); got != "My Project" {
		t.Errorf("name = %q, want My Project", got)
	}
}

func TestProjectResourceReadFillsTheNameOfAnImport(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectInstance()
	instance.addProject("my-project", "My Project")
	r := &projectResource{client: instance.start(t)}
	s := projectResourceSchema(t)
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "my-project", "key": "my-project", "organization": "my-org",
	})}

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := providertest.ReadModel[projectResourceModel](t, resp.State).Name.ValueString(); got != "My Project" {
		t.Errorf("name = %q, want My Project", got)
	}
}

func TestProjectResourceReadTakesTheMainBranchFromTheServer(t *testing.T) {
	t.Parallel()

	// An empty value is a state of an earlier version, which has no main branch.
	for _, inState := range []string{"", "main", "master"} {
		t.Run("state "+inState, func(t *testing.T) {
			t.Parallel()

			instance := newFakeProjectInstance()
			instance.addProject("my-project", "My Project")
			instance.mainBranches["my-project"] = "develop"
			r := &projectResource{client: instance.start(t)}
			s := projectResourceSchema(t)
			state := projectState(t, s, inState)

			resp := &resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			if got := providertest.ReadModel[projectResourceModel](t, resp.State).MainBranch.ValueString(); got != "develop" {
				t.Errorf("main_branch = %q, want develop", got)
			}
		})
	}
}

// The project list found the project, so a 404 of the branch list means that
// someone deleted the project in between.
func TestProjectResourceReadAfterOutsideDeletionDuringTheRead(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectInstance()
	instance.addProject("my-project", "My Project")
	delete(instance.mainBranches, "my-project")
	r := &projectResource{client: instance.start(t)}
	s := projectResourceSchema(t)
	state := projectState(t, s, "main")

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("a deleted project stayed in state")
	}
}

func TestProjectResourceReadRetainsStateWhenTheMainBranchReadFails(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectInstance()
	instance.addProject("my-project", "My Project")
	instance.failBranches = true
	r := &projectResource{client: instance.start(t)}
	s := projectResourceSchema(t)
	state := projectState(t, s, "main")

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Cannot read the main branch of the project my-project")
	if !resp.State.Raw.Equal(state.Raw) {
		t.Error("a failed read changed the prior state")
	}
}

func TestProjectResourceUpdateRenamesTheMainBranch(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectInstance()
	instance.addProject("my-project", "My Project")
	r := &projectResource{client: instance.start(t)}
	s := projectResourceSchema(t)

	resp := &resource.UpdateResponse{State: projectState(t, s, "master")}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: s, Raw: projectState(t, s, "main").Raw},
		State: projectState(t, s, "master"),
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := instance.mainBranches["my-project"]; got != "main" {
		t.Errorf("main branch of the project = %q, want main", got)
	}
	if got := providertest.ReadModel[projectResourceModel](t, resp.State).MainBranch.ValueString(); got != "main" {
		t.Errorf("main_branch in state = %q, want main", got)
	}
	if want := []string{"/api/project_branches/rename"}; !slices.Equal(instance.calls, want) {
		t.Errorf("calls = %v, want %v: the project must not be replaced", instance.calls, want)
	}
}

func TestProjectResourceUpdateKeepsTheStateWhenTheRenameFails(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectInstance()
	instance.addProject("my-project", "My Project")
	instance.failBranches = true
	r := &projectResource{client: instance.start(t)}
	s := projectResourceSchema(t)

	prior := projectState(t, s, "master")
	resp := &resource.UpdateResponse{State: prior}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: s, Raw: projectState(t, s, "main").Raw},
		State: prior,
	}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Cannot set the main branch of the project my-project")
	if got := providertest.ReadModel[projectResourceModel](t, resp.State).MainBranch.ValueString(); got != "master" {
		t.Errorf("main_branch in state = %q, want master", got)
	}
}

func TestProjectResourceImportState(t *testing.T) {
	t.Parallel()

	s := projectResourceSchema(t)
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	NewProjectResource().(*projectResource).ImportState(context.Background(),
		resource.ImportStateRequest{ID: "my-org/my-project"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}

	model := providertest.ReadModel[projectResourceModel](t, resp.State)
	if got := model.Organization.ValueString(); got != "my-org" {
		t.Errorf("organization = %q, want my-org", got)
	}
	if got := model.Key.ValueString(); got != "my-project" {
		t.Errorf("key = %q, want my-project", got)
	}
	if got := model.ID.ValueString(); got != "my-project" {
		t.Errorf("id = %q, want my-project", got)
	}
	// The read that follows fills it.
	if !model.Name.IsNull() {
		t.Errorf("name = %q, want null", model.Name.ValueString())
	}
}

func TestProjectResourceImportStateRefusesAMalformedIdentifier(t *testing.T) {
	t.Parallel()

	s := projectResourceSchema(t)
	for _, id := range []string{"my-project", "", "/my-project", "my-org/"} {
		resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
		NewProjectResource().(*projectResource).ImportState(context.Background(),
			resource.ImportStateRequest{ID: id}, resp)
		providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Cannot import the project")
	}
}

func TestProjectResourceValidatesTheAttributes(t *testing.T) {
	t.Parallel()

	s := projectResourceSchema(t)
	refused := map[string][]string{
		"key":          {"", "my project", "my/project", "42", strings.Repeat("a", 401)},
		"name":         {"", strings.Repeat("a", 501)},
		"main_branch":  {"", strings.Repeat("a", 256)},
		"organization": {"", "My-Org", "-my-org", "my-org-", "my--org", strings.Repeat("a", 256)},
	}
	allowed := map[string][]string{
		"key":          {"a", "my-project", "my_project.one:two", "42a", strings.Repeat("a", 400)},
		"name":         {"My Project", strings.Repeat("a", 500)},
		"main_branch":  {"main", "release/1.x", strings.Repeat("a", 255)},
		"organization": {"a", "my-org", "my-org-1", "my_org", strings.Repeat("a", 255)},
	}

	for name, values := range refused {
		for _, value := range values {
			if diags := providertest.ValidateString(t, s, name, value); !diags.HasError() {
				t.Errorf("%s = %q was allowed", name, truncate(value))
			}
		}
	}
	for name, values := range allowed {
		for _, value := range values {
			if diags := providertest.ValidateString(t, s, name, value); diags.HasError() {
				t.Errorf("%s = %q was refused: %v", name, truncate(value), diags)
			}
		}
	}
}

func truncate(value string) string {
	if len(value) <= 20 {
		return value
	}
	return value[:20] + "..."
}
