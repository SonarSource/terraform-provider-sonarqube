package provider

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

const (
	theProjectID        = "project-legacy-id"
	theProjectBindingID = "binding-id"
	// theNewProjectBindingID is the identifier that a bind gives, which
	// differs from the one of a binding that exists already.
	theNewProjectBindingID = "new-binding-id"
)

func cloudProjectBindingResourceSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewCloudProjectBindingResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func cloudProjectBindingDataSourceSchema(t *testing.T) datasourceschema.Schema {
	t.Helper()
	resp := &datasource.SchemaResponse{}
	NewCloudProjectBindingDataSource().Schema(context.Background(), datasource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

// fakeBindingInstance holds one project of the organization my-org and, when
// repository is not empty, its binding. GitHub gives every repository the
// number in repositoryIDs.
type fakeBindingInstance struct {
	projectExists bool
	repository    string
	bindingID     string
	calls         []string
	contentTypes  []string
	// bindStatus makes the bind call fail with this status when it is not 0.
	bindStatus int
}

var repositoryIDs = map[string]string{
	"my-github-org/my-repo":    "1001",
	"my-github-org/other-repo": "1002",
	"my-github-org/old-name":   "1001",
}

// currentSlugs holds the repositories that were renamed on GitHub. The server
// follows the redirect of GitHub and reports the current slug.
var currentSlugs = map[string]string{
	"my-github-org/old-name": "my-github-org/my-repo",
}

func newFakeBindingInstance() *fakeBindingInstance {
	return &fakeBindingInstance{projectExists: true, bindingID: theProjectBindingID}
}

func (f *fakeBindingInstance) binding() map[string]string {
	return map[string]string{
		"id": f.bindingID, "projectId": theProjectID, "devOpsPlatform": "github",
		"repositoryId": repositoryIDs[f.repository], "slug": cmp.Or(currentSlugs[f.repository], f.repository),
		"url": "https://github.com/" + f.repository,
	}
}

func (f *fakeBindingInstance) start(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		f.contentTypes = append(f.contentTypes, r.Header.Get("Content-Type"))
		var body map[string]string
		if raw, err := io.ReadAll(r.Body); err == nil && len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}

		switch r.Method + " " + r.URL.Path {
		case "GET /organizations/organizations":
			_, _ = w.Write([]byte(`[{"id":"organization-id","uuidV4":"` + fakeOrganizationUUID + `","key":"my-org"}]`))
		case "GET /projects/projects":
			f.searchProjects(w, r)
		case "GET /dop-translation/project-bindings":
			f.searchBindings(t, w, r)
		case "POST /dop-translation/project-bindings":
			f.bind(t, w, body)
		case "PATCH /dop-translation/project-bindings/" + f.bindingID:
			f.repository = body["repositoryId"]
			_ = json.NewEncoder(w).Encode(f.binding())
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return newTestCloudClient(srv)
}

func (f *fakeBindingInstance) searchProjects(w http.ResponseWriter, r *http.Request) {
	projects := []client.Project{}
	if f.projectExists && r.URL.Query().Get("keys") == "my-project" {
		projects = append(projects, client.Project{
			ID: "project-uuid", LegacyID: theProjectID, Key: "my-project", OrganizationID: fakeOrganizationUUID,
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"projects": projects})
}

func (f *fakeBindingInstance) searchBindings(t *testing.T, w http.ResponseWriter, r *http.Request) {
	if got := r.URL.Query().Get("projectId"); got != theProjectID {
		t.Errorf("projectId = %q, want %q", got, theProjectID)
	}
	bindings := []map[string]string{}
	if f.repository != "" {
		bindings = append(bindings, f.binding())
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"bindings": bindings})
}

func (f *fakeBindingInstance) bind(t *testing.T, w http.ResponseWriter, body map[string]string) {
	if f.bindStatus != 0 {
		w.WriteHeader(f.bindStatus)
		_, _ = w.Write([]byte(`{"message":"The repository is not accessible"}`))
		return
	}
	if body["projectId"] != theProjectID {
		t.Errorf("projectId = %q, want %q", body["projectId"], theProjectID)
	}
	f.repository = body["repositoryId"]
	f.bindingID = theNewProjectBindingID
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(f.binding())
}

func bindingState(t *testing.T, s schema.Schema, repository string) tfsdk.State {
	t.Helper()
	return tfsdk.State{Schema: s, Raw: schemaValue(t, s, map[string]string{
		"id": theProjectBindingID, "organization": "my-org", "project_key": "my-project",
		"repository": repository, "repository_id": repositoryIDs["my-github-org/my-repo"],
	})}
}

func bindingPlan(t *testing.T, s schema.Schema, repository string) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan{Schema: s, Raw: schemaValue(t, s, map[string]string{
		"organization": "my-org", "project_key": "my-project", "repository": repository,
	})}
}

func TestCloudProjectBindingResourceSchema(t *testing.T) {
	t.Parallel()

	s := cloudProjectBindingResourceSchema(t)
	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"organization", "project_key", "repository"} {
		if !s.Attributes[name].IsRequired() {
			t.Errorf("%s must be required", name)
		}
	}
	for _, name := range []string{"id", "repository_id"} {
		if !s.Attributes[name].IsComputed() {
			t.Errorf("%s must be computed", name)
		}
	}
	// An update can bind a replaced project again, which gives a new
	// identifier, so the plan must leave the identifier unknown.
	if id, ok := s.Attributes["id"].(schema.StringAttribute); !ok || len(id.PlanModifiers) != 0 {
		t.Error("id must not keep the prior value in the plan")
	}
	// A different project is a different binding.
	for _, name := range []string{"organization", "project_key"} {
		attribute, ok := s.Attributes[name].(schema.StringAttribute)
		if !ok || len(attribute.PlanModifiers) != 1 ||
			attribute.PlanModifiers[0].Description(context.Background()) !=
				stringplanmodifier.RequiresReplace().Description(context.Background()) {
			t.Errorf("%s must require a replacement", name)
		}
	}
	resp := &resource.MetadataResponse{}
	NewCloudProjectBindingResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "sonarqube"}, resp)
	if resp.TypeName != "sonarqube_cloud_project_binding" {
		t.Errorf("type name = %q, want sonarqube_cloud_project_binding", resp.TypeName)
	}
}

func TestCloudProjectBindingResourceConfigureNeedsCloud(t *testing.T) {
	t.Parallel()

	r := &cloudProjectBindingResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: client.New(client.Config{
		URL: "https://dev.example.io", Product: client.ProductServer,
	})}, resp)
	assertDiagnosticsContain(t, resp.Diagnostics, "sonarqube_cloud_project_binding resource")
	if r.client != nil {
		t.Error("the resource retained a Server client")
	}
}

func TestCloudProjectBindingResourceLifecycle(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	r := &cloudProjectBindingResource{client: instance.start(t)}
	s := cloudProjectBindingResourceSchema(t)

	created := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: bindingPlan(t, s, "my-github-org/my-repo")}, created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	model := readModel[cloudProjectBindingModel](t, created.State)
	if model.ID.ValueString() != theNewProjectBindingID || model.RepositoryID.ValueString() != "1001" {
		t.Errorf("state = %+v, want the binding and the repository identifier", model)
	}
	assertCallOrder(t, instance.calls, "GET /projects/projects", "POST /dop-translation/project-bindings")

	refreshed := &resource.ReadResponse{State: created.State}
	r.Read(context.Background(), resource.ReadRequest{State: created.State}, refreshed)
	if refreshed.Diagnostics.HasError() {
		t.Fatal(refreshed.Diagnostics)
	}
	if !created.State.Raw.Equal(refreshed.State.Raw) {
		t.Errorf("unchanged refresh changed state: before %v, after %v", created.State.Raw, refreshed.State.Raw)
	}

	updated := &resource.UpdateResponse{State: refreshed.State}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan:  bindingPlan(t, s, "my-github-org/other-repo"),
		State: refreshed.State,
	}, updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	model = readModel[cloudProjectBindingModel](t, updated.State)
	if model.Repository.ValueString() != "my-github-org/other-repo" || model.RepositoryID.ValueString() != "1002" {
		t.Errorf("state = %+v, want the other repository", model)
	}
	if got := instance.contentTypes[len(instance.contentTypes)-1]; got != "application/merge-patch+json" {
		t.Errorf("Content-Type of the change = %q, want application/merge-patch+json", got)
	}

	calls := len(instance.calls)
	deleted := &resource.DeleteResponse{}
	r.Delete(context.Background(), resource.DeleteRequest{State: updated.State}, deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	if deleted.Diagnostics.WarningsCount() != 1 {
		t.Errorf("delete gave %d warnings, want 1", deleted.Diagnostics.WarningsCount())
	}
	if len(instance.calls) != calls {
		t.Errorf("delete called the API: %v", instance.calls[calls:])
	}
}

// A project that was bound outside Terraform to the same repository is
// adopted, and a slug that differs in case only is the same repository.
func TestCloudProjectBindingResourceCreateAdoptsTheSameRepository(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	instance.repository = "my-github-org/my-repo"
	r := &cloudProjectBindingResource{client: instance.start(t)}
	s := cloudProjectBindingResourceSchema(t)

	resp := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: bindingPlan(t, s, "My-GitHub-Org/My-Repo")}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if slices.Contains(instance.calls, "POST /dop-translation/project-bindings") {
		t.Errorf("the project was bound again: %v", instance.calls)
	}
	model := readModel[cloudProjectBindingModel](t, resp.State)
	if model.Repository.ValueString() != "My-GitHub-Org/My-Repo" {
		t.Errorf("repository = %q, want the value of the configuration", model.Repository.ValueString())
	}
	if model.ID.ValueString() != theProjectBindingID {
		t.Errorf("id = %q, want %q", model.ID.ValueString(), theProjectBindingID)
	}
}

func TestCloudProjectBindingResourceCreateRefusesADifferentRepository(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	instance.repository = "my-github-org/other-repo"
	r := &cloudProjectBindingResource{client: instance.start(t)}
	s := cloudProjectBindingResourceSchema(t)

	resp := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: bindingPlan(t, s, "my-github-org/my-repo")}, resp)
	assertDiagnosticsContain(t, resp.Diagnostics, "already bound to my-github-org/other-repo")
	assertDiagnosticsContain(t, resp.Diagnostics, `"my-org/my-project"`)
	if instance.repository != "my-github-org/other-repo" {
		t.Errorf("the binding changed to %q", instance.repository)
	}
}

func TestCloudProjectBindingResourceCreateExplainsAFailedBind(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	instance.bindStatus = http.StatusBadRequest
	r := &cloudProjectBindingResource{client: instance.start(t)}
	s := cloudProjectBindingResourceSchema(t)

	resp := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: bindingPlan(t, s, "my-github-org/my-repo")}, resp)
	assertDiagnosticsContain(t, resp.Diagnostics, "The repository is not accessible")
	assertDiagnosticsContain(t, resp.Diagnostics, "monorepo")
	if !resp.State.Raw.IsNull() {
		t.Error("a failed bind wrote state")
	}
}

func TestCloudProjectBindingResourceCreateNeedsTheProject(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	instance.projectExists = false
	r := &cloudProjectBindingResource{client: instance.start(t)}
	s := cloudProjectBindingResourceSchema(t)

	resp := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: bindingPlan(t, s, "my-github-org/my-repo")}, resp)
	assertDiagnosticsContain(t, resp.Diagnostics, "Project my-project not found")
}

func TestCloudProjectBindingResourceReadAfterOutsideDeletion(t *testing.T) {
	t.Parallel()

	for name, instance := range map[string]*fakeBindingInstance{
		"project deleted":   {projectExists: false},
		"binding not found": {projectExists: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := &cloudProjectBindingResource{client: instance.start(t)}
			s := cloudProjectBindingResourceSchema(t)
			state := bindingState(t, s, "my-github-org/my-repo")
			resp := &resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			if !resp.State.Raw.IsNull() {
				t.Error("a binding that is gone stayed in state")
			}
			// A project that the token may not see gives the same answer as
			// a deleted one, so the removal is reported.
			if want := !instance.projectExists; (resp.Diagnostics.WarningsCount() == 1) != want {
				t.Errorf("warnings = %v, want a warning: %v", resp.Diagnostics.Warnings(), want)
			}
		})
	}
}

// A binding changed outside Terraform shows in the state, so that the next
// plan changes it back.
func TestCloudProjectBindingResourceReadReportsAnotherRepository(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	instance.repository = "my-github-org/other-repo"
	r := &cloudProjectBindingResource{client: instance.start(t)}
	s := cloudProjectBindingResourceSchema(t)
	state := bindingState(t, s, "my-github-org/my-repo")

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := readModel[cloudProjectBindingModel](t, resp.State).Repository.ValueString(); got != "my-github-org/other-repo" {
		t.Errorf("repository = %q, want my-github-org/other-repo", got)
	}
}

// A repository that was renamed on GitHub keeps its identifier. The state
// keeps the slug of the configuration, or the next plan would change the
// binding back to the old name.
func TestCloudProjectBindingResourceReadKeepsTheSlugOfARenamedRepository(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	instance.repository = "my-github-org/old-name"
	r := &cloudProjectBindingResource{client: instance.start(t)}
	s := cloudProjectBindingResourceSchema(t)
	state := bindingState(t, s, "my-github-org/old-name")

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !resp.State.Raw.Equal(state.Raw) {
		t.Errorf("a rename on GitHub changed the state: before %v, after %v", state.Raw, resp.State.Raw)
	}
}

// The server reports the current slug of a renamed repository after a bind.
// The state keeps the slug of the plan, or Terraform reports an inconsistent
// result.
func TestCloudProjectBindingResourceCreateKeepsTheSlugOfARenamedRepository(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	r := &cloudProjectBindingResource{client: instance.start(t)}
	s := cloudProjectBindingResourceSchema(t)

	resp := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: bindingPlan(t, s, "my-github-org/old-name")}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	model := readModel[cloudProjectBindingModel](t, resp.State)
	if model.Repository.ValueString() != "my-github-org/old-name" || model.RepositoryID.ValueString() != "1001" {
		t.Errorf("state = %+v, want the slug of the plan and the identifier of the server", model)
	}
}

func TestCloudProjectBindingResourceReadRetainsStateOnFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	r := &cloudProjectBindingResource{client: newTestCloudClient(srv)}
	s := cloudProjectBindingResourceSchema(t)
	state := bindingState(t, s, "my-github-org/my-repo")

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	assertDiagnosticsContain(t, resp.Diagnostics, "Cannot read the binding of the project")
	if !resp.State.Raw.Equal(state.Raw) {
		t.Error("a failed read changed the prior state")
	}
}

// A slug that differs in case only names the same repository, so nothing is
// written. The binding is still read, so that one that is gone is noticed.
func TestCloudProjectBindingResourceUpdateOfTheCaseOnlyWritesNothing(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	instance.repository = "my-github-org/my-repo"
	r := &cloudProjectBindingResource{client: instance.start(t)}
	s := cloudProjectBindingResourceSchema(t)
	state := bindingState(t, s, "my-github-org/my-repo")

	resp := &resource.UpdateResponse{State: state}
	r.Update(context.Background(), resource.UpdateRequest{Plan: bindingPlan(t, s, "My-GitHub-Org/My-Repo"), State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if want := []string{
		"GET /organizations/organizations", "GET /projects/projects", "GET /dop-translation/project-bindings",
	}; !slices.Equal(instance.calls, want) {
		t.Errorf("calls = %v, want only the reads %v", instance.calls, want)
	}
	model := readModel[cloudProjectBindingModel](t, resp.State)
	if model.Repository.ValueString() != "My-GitHub-Org/My-Repo" || model.ID.ValueString() != theProjectBindingID {
		t.Errorf("state = %+v, want the new case and the old identifier", model)
	}
}

// A replacement of the project for a new name, in the same apply as a change
// of case, removed the binding. The change of case binds the project again.
func TestCloudProjectBindingResourceUpdateOfTheCaseOnlyBindsAReplacedProject(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	r := &cloudProjectBindingResource{client: instance.start(t)}
	s := cloudProjectBindingResourceSchema(t)
	state := bindingState(t, s, "my-github-org/my-repo")

	resp := &resource.UpdateResponse{State: state}
	r.Update(context.Background(), resource.UpdateRequest{Plan: bindingPlan(t, s, "My-GitHub-Org/My-Repo"), State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !slices.Contains(instance.calls, "POST /dop-translation/project-bindings") {
		t.Errorf("the project was not bound again: %v", instance.calls)
	}
	if got := readModel[cloudProjectBindingModel](t, resp.State).ID.ValueString(); got != theNewProjectBindingID {
		t.Errorf("id = %q, want the identifier of the new binding %q", got, theNewProjectBindingID)
	}
}

func TestCloudProjectBindingResourceUpdateNeedsTheProject(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	instance.projectExists = false
	r := &cloudProjectBindingResource{client: instance.start(t)}
	s := cloudProjectBindingResourceSchema(t)
	state := bindingState(t, s, "my-github-org/my-repo")

	resp := &resource.UpdateResponse{State: state}
	r.Update(context.Background(), resource.UpdateRequest{Plan: bindingPlan(t, s, "my-github-org/other-repo"), State: state}, resp)
	assertDiagnosticsContain(t, resp.Diagnostics, "Project my-project not found")
}

func TestCloudProjectBindingResourceUpdateBindsAReplacedProject(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	r := &cloudProjectBindingResource{client: instance.start(t)}
	s := cloudProjectBindingResourceSchema(t)
	state := bindingState(t, s, "my-github-org/my-repo")

	resp := &resource.UpdateResponse{State: state}
	r.Update(context.Background(), resource.UpdateRequest{Plan: bindingPlan(t, s, "my-github-org/other-repo"), State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !slices.Contains(instance.calls, "POST /dop-translation/project-bindings") {
		t.Errorf("the project was not bound again: %v", instance.calls)
	}
	if instance.repository != "my-github-org/other-repo" {
		t.Errorf("repository = %q, want my-github-org/other-repo", instance.repository)
	}
	model := readModel[cloudProjectBindingModel](t, resp.State)
	if model.RepositoryID.ValueString() != "1002" {
		t.Errorf("repository_id = %q, want 1002", model.RepositoryID.ValueString())
	}
	// The new binding has a new identifier, so the plan must not have fixed
	// the old one. See the schema test.
	if model.ID.ValueString() != theNewProjectBindingID {
		t.Errorf("id = %q, want the identifier of the new binding %q", model.ID.ValueString(), theNewProjectBindingID)
	}
}

func TestCloudProjectBindingResourceImportState(t *testing.T) {
	t.Parallel()

	s := cloudProjectBindingResourceSchema(t)
	resp := &resource.ImportStateResponse{State: emptyState(t, s)}
	NewCloudProjectBindingResource().(*cloudProjectBindingResource).ImportState(context.Background(),
		resource.ImportStateRequest{ID: "my-org/my-project"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	model := readModel[cloudProjectBindingModel](t, resp.State)
	if model.Organization.ValueString() != "my-org" || model.ProjectKey.ValueString() != "my-project" {
		t.Errorf("state = %+v, want my-org and my-project", model)
	}

	for _, id := range []string{"my-project", "", "/my-project", "my-org/"} {
		refused := &resource.ImportStateResponse{State: emptyState(t, s)}
		NewCloudProjectBindingResource().(*cloudProjectBindingResource).ImportState(context.Background(),
			resource.ImportStateRequest{ID: id}, refused)
		assertDiagnosticsContain(t, refused.Diagnostics, "Cannot import the project binding")
	}
}

// The read that follows an import fills the repository from the server.
func TestCloudProjectBindingResourceReadAfterImport(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	instance.repository = "my-github-org/my-repo"
	r := &cloudProjectBindingResource{client: instance.start(t)}
	s := cloudProjectBindingResourceSchema(t)
	state := tfsdk.State{Schema: s, Raw: schemaValue(t, s, map[string]string{
		"organization": "my-org", "project_key": "my-project",
	})}

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	model := readModel[cloudProjectBindingModel](t, resp.State)
	if model.Repository.ValueString() != "my-github-org/my-repo" || model.ID.ValueString() != theProjectBindingID {
		t.Errorf("state = %+v, want the binding of the server", model)
	}
}

// The list of causes answers a refusal of the API, not a network failure.
func TestBindFailureDetail(t *testing.T) {
	t.Parallel()

	if got := bindFailureDetail(&client.APIError{StatusCode: http.StatusBadRequest}); !strings.Contains(got, "monorepo") {
		t.Errorf("detail of an API error = %q, want the list of causes", got)
	}
	if got := bindFailureDetail(errors.New("connection refused")); got != "connection refused" {
		t.Errorf("detail of a network error = %q, want the error only", got)
	}
}

func TestRepositoryIDPlanModifier(t *testing.T) {
	t.Parallel()

	s := cloudProjectBindingResourceSchema(t)
	state := bindingState(t, s, "my-github-org/my-repo")
	for repository, keeps := range map[string]bool{
		"my-github-org/my-repo":    true,
		"My-GitHub-Org/My-Repo":    true,
		"my-github-org/other-repo": false,
	} {
		resp := &planmodifier.StringResponse{PlanValue: types.StringUnknown()}
		repositoryIDPlanModifier{}.PlanModifyString(context.Background(), planmodifier.StringRequest{
			Path:       path.Root("repository_id"),
			State:      state,
			Plan:       bindingPlan(t, s, repository),
			StateValue: types.StringValue("1001"),
			PlanValue:  types.StringUnknown(),
		}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
		if got := !resp.PlanValue.IsUnknown(); got != keeps {
			t.Errorf("%s: kept = %v, want %v", repository, got, keeps)
		}
	}
}

func TestCloudProjectBindingResourceValidatesTheRepository(t *testing.T) {
	t.Parallel()

	s := cloudProjectBindingResourceSchema(t)
	for _, value := range []string{"", "my-repo", "owner/", "/repo", "owner/repo/more", "https://github.com/owner/repo", "own er/repo"} {
		if diags := validateString(t, s, "repository", value); !diags.HasError() {
			t.Errorf("repository = %q was allowed", value)
		}
	}
	for _, value := range []string{"owner/repo", "My-Org/my.repo_1", "a/b"} {
		if diags := validateString(t, s, "repository", value); diags.HasError() {
			t.Errorf("repository = %q was refused: %v", value, diags)
		}
	}
}

func TestCloudProjectBindingDataSourceRead(t *testing.T) {
	t.Parallel()

	instance := newFakeBindingInstance()
	instance.repository = "my-github-org/my-repo"
	d := &cloudProjectBindingDataSource{client: instance.start(t)}
	s := cloudProjectBindingDataSourceSchema(t)
	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Fatal(err)
	}

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(context.Background(), datasource.ReadRequest{Config: projectBindingConfig(t, s)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	model := readModel[cloudProjectBindingModel](t, resp.State)
	if model.ID.ValueString() != theProjectBindingID || model.Repository.ValueString() != "my-github-org/my-repo" ||
		model.RepositoryID.ValueString() != "1001" || model.ProjectKey.ValueString() != "my-project" {
		t.Errorf("state = %+v, want the binding of the server", model)
	}
}

func TestCloudProjectBindingDataSourceReadOfAProjectThatIsNotBound(t *testing.T) {
	t.Parallel()

	d := &cloudProjectBindingDataSource{client: newFakeBindingInstance().start(t)}
	s := cloudProjectBindingDataSourceSchema(t)

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(context.Background(), datasource.ReadRequest{Config: projectBindingConfig(t, s)}, resp)
	assertDiagnosticsContain(t, resp.Diagnostics, "has no binding")
}

func projectBindingConfig(t *testing.T, s datasourceschema.Schema) tfsdk.Config {
	t.Helper()
	objectType, ok := s.Type().TerraformType(context.Background()).(tftypes.Object)
	if !ok {
		t.Fatal("the schema of the data source is not an object type")
	}
	values := map[string]tftypes.Value{}
	for name := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(tftypes.String, nil)
	}
	values["organization"] = tftypes.NewValue(tftypes.String, "my-org")
	values["project_key"] = tftypes.NewValue(tftypes.String, "my-project")
	return tfsdk.Config{Schema: s, Raw: tftypes.NewValue(objectType, values)}
}
