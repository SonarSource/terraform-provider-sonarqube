package shared

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
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
	// hiddenReads is the number of project reads that do not show a project,
	// as the project list can do after a change of its key.
	hiddenReads int
	// hiddenBranchReads is the number of branch list reads that answer 404,
	// as the branch list can do after a change of the key.
	hiddenBranchReads int
	calls             []string
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
		case "/api/projects/update_key":
			f.serveUpdateKey(w, r)
		case "/projects/projects":
			if r.URL.Query().Get("organizationIds") != providertest.FakeOrganizationUUID {
				t.Errorf("organizationIds = %q, want %q", r.URL.Query().Get("organizationIds"), providertest.FakeOrganizationUUID)
			}
			key := r.URL.Query().Get("keys")
			projects := []client.Project{}
			if f.hiddenReads > 0 {
				f.hiddenReads--
			} else if name, found := f.projects[key]; found {
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

// serveUpdateKey moves the project and its main branch to the new key, and
// refuses a key that another project has, as the server does.
func (f *fakeProjectInstance) serveUpdateKey(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	from, to := r.PostForm.Get("from"), r.PostForm.Get("to")
	if _, taken := f.projects[to]; taken {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"msg":"Impossible to update key: a component with key \"` + to + `\" already exists."}]}`))
		return
	}
	f.projects[to], f.mainBranches[to] = f.projects[from], f.mainBranches[from]
	delete(f.projects, from)
	delete(f.mainBranches, from)
	w.WriteHeader(http.StatusNoContent)
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
	if f.hiddenBranchReads > 0 {
		f.hiddenBranchReads--
		found = false
	}
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
	return projectStateWithKey(t, s, "my-project", mainBranch)
}

func projectStateWithKey(t *testing.T, s schema.Schema, key, mainBranch string) tfsdk.State {
	t.Helper()

	attributes := map[string]string{
		"id": key, "key": key, "name": "My Project", "organization": "my-org",
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
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "cannot read the main branch")
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

func TestProjectResourceUpdateChangesTheKeyInPlace(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		mainBranch        string
		hiddenReads       int
		hiddenBranchReads int
		wantCalls         []string
	}{
		"branch list shows the key late": {mainBranch: "master", hiddenBranchReads: 1, wantCalls: []string{"/api/projects/update_key",
			"/organizations/organizations", "/projects/projects", "/api/project_branches/list",
			"/organizations/organizations", "/projects/projects", "/api/project_branches/list"}},
		"key only": {mainBranch: "master", wantCalls: []string{"/api/projects/update_key", "/organizations/organizations", "/projects/projects",
			"/api/project_branches/list"}},
		"key and main branch": {mainBranch: "main", wantCalls: []string{"/api/projects/update_key", "/organizations/organizations", "/projects/projects",
			"/api/project_branches/list", "/api/project_branches/rename"}},
		"list shows the key late": {mainBranch: "master", hiddenReads: 2, wantCalls: []string{"/api/projects/update_key",
			"/organizations/organizations", "/projects/projects", "/organizations/organizations", "/projects/projects",
			"/organizations/organizations", "/projects/projects", "/api/project_branches/list"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			instance := newFakeProjectInstance()
			instance.addProject("my-project", "My Project")
			instance.hiddenReads, instance.hiddenBranchReads = tc.hiddenReads, tc.hiddenBranchReads
			r := &projectResource{client: instance.start(t), keyWaits: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}}
			s := projectResourceSchema(t)

			resp := &resource.UpdateResponse{State: projectState(t, s, "master")}
			r.Update(t.Context(), resource.UpdateRequest{
				Plan:  tfsdk.Plan{Schema: s, Raw: projectStateWithKey(t, s, "new-key", tc.mainBranch).Raw},
				State: projectState(t, s, "master"),
			}, resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			state := providertest.ReadModel[projectResourceModel](t, resp.State)
			if state.Key.ValueString() != "new-key" || state.ID.ValueString() != "new-key" || state.MainBranch.ValueString() != tc.mainBranch {
				t.Errorf("state = %v", state)
			}
			if _, found := instance.projects["new-key"]; !found || instance.mainBranches["new-key"] != tc.mainBranch {
				t.Errorf("projects = %v, main branches = %v", instance.projects, instance.mainBranches)
			}
			if !slices.Equal(instance.calls, tc.wantCalls) {
				t.Errorf("calls = %v, want %v: the project must not be replaced", instance.calls, tc.wantCalls)
			}
		})
	}
}

// A refused key keeps the old key in the state.
func TestProjectResourceUpdateReportsARefusedKey(t *testing.T) {
	t.Parallel()
	instance := newFakeProjectInstance()
	instance.addProject("my-project", "My Project")
	instance.addProject("taken-key", "Other")
	r := &projectResource{client: instance.start(t)}
	s := projectResourceSchema(t)

	prior := projectState(t, s, "master")
	resp := &resource.UpdateResponse{State: prior}
	r.Update(t.Context(), resource.UpdateRequest{Plan: tfsdk.Plan{Schema: s, Raw: projectStateWithKey(t, s, "taken-key", "master").Raw}, State: prior}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "already exists")
	if got := providertest.ReadModel[projectResourceModel](t, resp.State).Key.ValueString(); got != "my-project" {
		t.Errorf("key in state = %q, want my-project", got)
	}
}

// planAttribute runs the plan modifiers of one attribute of the project
// schema, from a project in the state with the prior values.
func planAttribute(t *testing.T, attribute string, planned types.String) *planmodifier.StringResponse {
	t.Helper()
	s := projectResourceSchema(t)
	prior := map[string]string{"id": "my-project", "key": "my-project", "name": "My project", "organization": "my-org", "main_branch": "main"}
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, prior)}
	stateValue := types.StringValue(prior[attribute])
	req := planmodifier.StringRequest{
		Path: path.Root(attribute), State: state, Plan: tfsdk.Plan{Schema: s, Raw: state.Raw},
		StateValue: stateValue, PlanValue: planned, ConfigValue: planned,
	}
	resp := &planmodifier.StringResponse{PlanValue: planned}
	for _, modifier := range s.Attributes[attribute].(schema.StringAttribute).PlanModifiers {
		modifier.PlanModifyString(t.Context(), req, resp)
	}
	return resp
}

// SonarQube Cloud cannot rename or move a project, and a replacement deletes
// its history, so the plan refuses a new name or organization.
func TestProjectResourceRefusesANewNameOrOrganization(t *testing.T) {
	t.Parallel()
	for attribute, want := range map[string]string{"name": "Cannot rename the project", "organization": "Cannot move the project"} {
		resp := planAttribute(t, attribute, types.StringValue("other"))
		providertest.AssertDiagnosticsContain(t, resp.Diagnostics, want)
		providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "terraform destroy -target")
		if resp.RequiresReplace {
			t.Errorf("a new %s replaces the project", attribute)
		}
	}
	providertest.AssertDiagnosticsContain(t, planAttribute(t, "organization", types.StringValue("other")).Diagnostics, "Change the key")
}

// A value that was unknown in the plan and differs at apply is refused too,
// because Update cannot rename or move a project.
func TestProjectResourceUpdateRefusesAMove(t *testing.T) {
	t.Parallel()
	instance := newFakeProjectInstance()
	instance.addProject("my-project", "My Project")
	r := &projectResource{client: instance.start(t)}
	s := projectResourceSchema(t)

	prior := projectState(t, s, "master")
	moved := providertest.SchemaValue(t, s, map[string]string{"id": "my-project", "key": "my-project", "name": "My Project", "organization": "other-org", "main_branch": "master"})
	resp := &resource.UpdateResponse{State: prior}
	r.Update(t.Context(), resource.UpdateRequest{Plan: tfsdk.Plan{Schema: s, Raw: moved}, State: prior}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Cannot rename or move the project")
	if len(instance.calls) != 0 {
		t.Errorf("calls = %v, want none", instance.calls)
	}
}

// A new key, a new main branch, the same name, an unknown name or
// organization, a create and a destroy replace nothing and refuse nothing.
func TestProjectResourcePlansOtherChangesInPlace(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		attribute string
		planned   types.String
	}{
		"new key":              {attribute: "key", planned: types.StringValue("other")},
		"new main branch":      {attribute: "main_branch", planned: types.StringValue("trunk")},
		"same name":            {attribute: "name", planned: types.StringValue("My project")},
		"unknown name":         {attribute: "name", planned: types.StringUnknown()},
		"unknown organization": {attribute: "organization", planned: types.StringUnknown()},
	} {
		if resp := planAttribute(t, tc.attribute, tc.planned); resp.RequiresReplace || len(resp.Diagnostics) != 0 {
			t.Errorf("%s: RequiresReplace = %v, diagnostics = %v", name, resp.RequiresReplace, resp.Diagnostics)
		}
	}
	s := projectResourceSchema(t)
	value := providertest.SchemaValue(t, s, map[string]string{"key": "my-project", "name": "My project", "organization": "my-org"})
	null := tftypes.NewValue(s.Type().TerraformType(t.Context()), nil)
	for name, raws := range map[string][2]tftypes.Value{"create": {null, value}, "destroy": {value, null}} {
		for _, attribute := range []string{"name", "organization"} {
			req := planmodifier.StringRequest{
				Path: path.Root(attribute), State: tfsdk.State{Schema: s, Raw: raws[0]}, Plan: tfsdk.Plan{Schema: s, Raw: raws[1]},
				StateValue: types.StringValue("Old"), PlanValue: types.StringValue("New"),
			}
			resp := &planmodifier.StringResponse{}
			for _, modifier := range s.Attributes[attribute].(schema.StringAttribute).PlanModifiers {
				modifier.PlanModifyString(t.Context(), req, resp)
			}
			if resp.RequiresReplace || len(resp.Diagnostics) != 0 {
				t.Errorf("%s %s: RequiresReplace = %v, diagnostics = %v", name, attribute, resp.RequiresReplace, resp.Diagnostics)
			}
		}
	}
}

// The id follows the planned key, so that a new key also plans a new id.
func TestProjectResourcePlansTheIDFromTheKey(t *testing.T) {
	t.Parallel()
	s := projectResourceSchema(t)
	plan := tfsdk.Plan{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{"id": "my-project", "key": "new-key", "name": "My project", "organization": "my-org"})}
	resp := &planmodifier.StringResponse{PlanValue: types.StringValue("my-project")}
	for _, modifier := range s.Attributes["id"].(schema.StringAttribute).PlanModifiers {
		modifier.PlanModifyString(t.Context(), planmodifier.StringRequest{Path: path.Root("id"), Plan: plan, PlanValue: types.StringUnknown()}, resp)
	}
	if resp.PlanValue.ValueString() != "new-key" {
		t.Errorf("planned id = %v, want new-key", resp.PlanValue)
	}
}

// When the project list does not show the new key in time, the key change
// stays in the state, with a warning.
func TestProjectResourceUpdateWarnsWhenTheNewKeyStaysHidden(t *testing.T) {
	t.Parallel()
	instance := newFakeProjectInstance()
	instance.addProject("my-project", "My Project")
	instance.hiddenReads = 10
	r := &projectResource{client: instance.start(t), keyWaits: []time.Duration{time.Millisecond}}
	s := projectResourceSchema(t)

	resp := &resource.UpdateResponse{State: projectState(t, s, "master")}
	r.Update(t.Context(), resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: s, Raw: projectStateWithKey(t, s, "new-key", "master").Raw},
		State: projectState(t, s, "master"),
	}, resp)
	if resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("diagnostics = %v, want one warning", resp.Diagnostics)
	}
	if got := providertest.ReadModel[projectResourceModel](t, resp.State).Key.ValueString(); got != "new-key" {
		t.Errorf("key in state = %q, want new-key", got)
	}
}
