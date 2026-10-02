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
	calls    []string
}

func newFakeProjectInstance() *fakeProjectInstance {
	return &fakeProjectInstance{projects: map[string]string{}}
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
			f.projects[r.PostForm.Get("project")] = r.PostForm.Get("name")
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
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return providertest.NewCloudClient(srv)
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
	if got := providertest.ReadModel[projectResourceModel](t, created.State).ID.ValueString(); got != "my-project" {
		t.Errorf("id = %q, want my-project", got)
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

// A read back would fail an apply whose create worked, and Terraform then
// taints the project and replaces it on the next apply.
func TestProjectResourceCreateDoesNotReadTheProjectBack(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectInstance()
	r := &projectResource{client: instance.start(t)}
	s := projectResourceSchema(t)
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"key": "my-project", "name": "My Project", "organization": "my-org",
	})}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := providertest.ReadModel[projectResourceModel](t, resp.State).ID.ValueString(); got != "my-project" {
		t.Errorf("id = %q, want my-project", got)
	}
	if want := []string{"/api/projects/create"}; !slices.Equal(instance.calls, want) {
		t.Errorf("calls = %v, want %v", instance.calls, want)
	}
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
	instance.projects["my-project"] = "Renamed in the user interface"
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
	instance.projects["my-project"] = "My Project"
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

func TestProjectResourceValidatesTheKeyAndTheName(t *testing.T) {
	t.Parallel()

	s := projectResourceSchema(t)
	refused := map[string][]string{
		"key":          {"", "my project", "my/project", "42", strings.Repeat("a", 401)},
		"name":         {"", strings.Repeat("a", 501)},
		"organization": {"", "My-Org", "-my-org", "my-org-", "my_org", strings.Repeat("a", 256)},
	}
	allowed := map[string][]string{
		"key":          {"a", "my-project", "my_project.one:two", "42a", strings.Repeat("a", 400)},
		"name":         {"My Project", strings.Repeat("a", 500)},
		"organization": {"a", "my-org", "my-org-1", strings.Repeat("a", 255)},
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
