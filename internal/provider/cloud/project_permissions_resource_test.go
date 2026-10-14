package cloud

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func projectPermissionsSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewProjectPermissionsResource().Schema(t.Context(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func projectPermissionModel(group string, permissions ...string) projectPermissionsModel {
	elements := make([]attr.Value, len(permissions))
	for i, permission := range permissions {
		elements[i] = types.StringValue(permission)
	}
	return projectPermissionsModel{
		ID: types.StringNull(), Organization: types.StringValue("my-org"), ProjectKey: types.StringValue("my-project"),
		GroupName: types.StringValue(group), Login: types.StringNull(), Permissions: types.SetValueMust(types.StringType, elements),
	}
}

func projectPermissionPlan(t *testing.T, s schema.Schema, model projectPermissionsModel) tfsdk.Plan {
	t.Helper()
	plan := tfsdk.Plan{Schema: s}
	if diags := plan.Set(t.Context(), &model); diags.HasError() {
		t.Fatal(diags)
	}
	return plan
}

// fakeProjectPermissionInstance holds the grants of the project my-project.
// The permission web services answer 404 with the server's message for any
// other project, and the v2 project read reports the visibility.
type fakeProjectPermissionInstance struct {
	fakePermissionInstance
	public        bool
	projectReads  int
	projectExists bool
}

func (f *fakeProjectPermissionInstance) start(t *testing.T) *projectPermissionsResource {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serveProject))
	t.Cleanup(srv.Close)
	r := NewProjectPermissionsResource().(*projectPermissionsResource)
	r.client = providertest.NewCloudClient(srv)
	return r
}

func (f *fakeProjectPermissionInstance) serveProject(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/organizations/organizations":
		fmt.Fprint(w, `[{"id":"organization-id","uuidV4":"`+providertest.FakeOrganizationUUID+`","key":"my-org"}]`)
		return
	case "/projects/projects":
		f.projectReads++
		visibility := "private"
		if f.public {
			visibility = "public"
		}
		fmt.Fprintf(w, `{"projects":[{"id":"project-uuid","key":"my-project","visibility":%q}]}`, visibility)
		return
	}
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !f.projectExists || r.Form.Get("projectKey") != "my-project" {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"errors":[{"msg":"Project key '%s' not found"}]}`, r.Form.Get("projectKey"))
		return
	}
	f.serve(w, r)
}

func newFakeProjectPermissionInstance(groups map[string][]string) *fakeProjectPermissionInstance {
	return &fakeProjectPermissionInstance{
		fakePermissionInstance: fakePermissionInstance{groups: groups, users: map[string][]string{}},
		projectExists:          true,
	}
}

func TestProjectPermissionsSchema(t *testing.T) {
	t.Parallel()
	s := projectPermissionsSchema(t)
	if err := s.ValidateImplementation(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"organization", "project_key", "permissions"} {
		if !s.Attributes[name].IsRequired() {
			t.Errorf("%s is not required", name)
		}
	}
	if !s.Attributes["group_name"].IsOptional() || !s.Attributes["login"].IsOptional() {
		t.Error("the principal attributes are not optional")
	}
	if len(NewProjectPermissionsResource().(*projectPermissionsResource).ConfigValidators(t.Context())) == 0 {
		t.Error("the principal choice has no config validator")
	}
	resp := &resource.MetadataResponse{}
	NewProjectPermissionsResource().Metadata(t.Context(), resource.MetadataRequest{ProviderTypeName: "sonarqube"}, resp)
	if resp.TypeName != "sonarqube_cloud_project_permissions" {
		t.Errorf("type name = %q", resp.TypeName)
	}
}

func TestProjectPermissionsValues(t *testing.T) {
	t.Parallel()
	s := projectPermissionsSchema(t)
	permissions := s.Attributes["permissions"].(schema.SetAttribute)
	validate := func(values ...string) bool {
		elements := make([]attr.Value, len(values))
		for i, value := range values {
			elements[i] = types.StringValue(value)
		}
		response := &validator.SetResponse{}
		for _, v := range permissions.Validators {
			v.ValidateSet(t.Context(), validator.SetRequest{ConfigValue: types.SetValueMust(types.StringType, elements)}, response)
		}
		return !response.Diagnostics.HasError()
	}
	if !validate(projectPermissionValues...) {
		t.Error("the valid project permissions were refused")
	}
	for _, invalid := range [][]string{nil, {"gateadmin"}, {"provisioning"}} {
		if validate(invalid...) {
			t.Errorf("%v was accepted", invalid)
		}
	}
	if diags := providertest.ValidateString(t, s, "group_name", "ANYONE"); !diags.HasError() {
		t.Error("Anyone was accepted")
	}
	if diags := providertest.ValidateString(t, s, "project_key", "my project"); !diags.HasError() {
		t.Error("an invalid project key was accepted")
	}
}

func TestProjectPermissionsGroupLifecycleAndDrift(t *testing.T) {
	instance := newFakeProjectPermissionInstance(map[string][]string{
		"Members": {"user", "codeviewer", "issueadmin"}, "Owners": {"admin"},
	})
	r := instance.start(t)
	s := projectPermissionsSchema(t)
	ctx := t.Context()
	plan := projectPermissionModel("Members", "user", "scan")
	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(ctx, resource.CreateRequest{Plan: projectPermissionPlan(t, s, plan)}, created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	if !reflect.DeepEqual(instance.calls, []string{
		"/api/permissions/add_group Members scan",
		"/api/permissions/remove_group Members codeviewer", "/api/permissions/remove_group Members issueadmin",
	}) {
		t.Errorf("create calls = %v", instance.calls)
	}
	state := providertest.ReadModel[projectPermissionsModel](t, created.State)
	if state.ID.ValueString() != "my-org/my-project/group/Members" {
		t.Errorf("created state = %+v", state)
	}

	second := &resource.UpdateResponse{State: created.State}
	r.Update(ctx, resource.UpdateRequest{Plan: projectPermissionPlan(t, s, plan), State: created.State}, second)
	if second.Diagnostics.HasError() || len(instance.calls) != 3 {
		t.Fatalf("second apply: %v, calls = %v", second.Diagnostics, instance.calls)
	}
	instance.groups["Members"] = append(instance.groups["Members"], "securityhotspotadmin")
	refreshed := &resource.ReadResponse{State: second.State}
	r.Read(ctx, resource.ReadRequest{State: second.State}, refreshed)
	refreshState := providertest.ReadModel[projectPermissionsModel](t, refreshed.State)
	var observed []string
	if diags := refreshState.Permissions.ElementsAs(ctx, &observed, false); diags.HasError() || !slices.Contains(observed, "securityhotspotadmin") {
		t.Errorf("drift permissions = %v, diagnostics = %v %v", observed, diags, refreshed.Diagnostics)
	}

	deleted := &resource.DeleteResponse{State: refreshed.State}
	r.Delete(ctx, resource.DeleteRequest{State: refreshed.State}, deleted)
	if deleted.Diagnostics.HasError() || len(instance.groups["Members"]) != 0 || !reflect.DeepEqual(instance.groups["Owners"], []string{"admin"}) {
		t.Errorf("destroy: %v, groups = %v", deleted.Diagnostics, instance.groups)
	}
}

func TestProjectPermissionsPublicProject(t *testing.T) {
	for _, tc := range []struct {
		name        string
		public      bool
		granted     []string
		permissions []string
		error       string
		reads       int
	}{
		{"public with user and codeviewer", true, nil, []string{"user", "codeviewer", "scan"}, "`user` and `codeviewer`", 1},
		{"public with codeviewer", true, nil, []string{"codeviewer"}, "`codeviewer` on a public project", 1},
		{"public without them", true, nil, []string{"issueadmin"}, "", 0},
		{"private with user", false, nil, []string{"user"}, "", 1},
		{"user already granted", true, []string{"user"}, []string{"user", "scan"}, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instance := newFakeProjectPermissionInstance(map[string][]string{"Developers": tc.granted})
			instance.public = tc.public
			r := instance.start(t)
			s := projectPermissionsSchema(t)
			resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
			r.Create(t.Context(), resource.CreateRequest{Plan: projectPermissionPlan(t, s, projectPermissionModel("Developers", tc.permissions...))}, resp)
			if tc.error == "" && resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			if tc.error != "" {
				providertest.AssertDiagnosticsContain(t, resp.Diagnostics, tc.error)
				if len(instance.calls) != 0 {
					t.Errorf("the refused plan sent changes: %v", instance.calls)
				}
			}
			if instance.projectReads != tc.reads {
				t.Errorf("project reads = %d, want %d", instance.projectReads, tc.reads)
			}
		})
	}
}

func TestProjectPermissionsMissingProject(t *testing.T) {
	instance := newFakeProjectPermissionInstance(map[string][]string{"Developers": {"scan"}})
	instance.projectExists = false
	r := instance.start(t)
	s := projectPermissionsSchema(t)
	model := projectPermissionModel("Developers", "scan")

	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: projectPermissionPlan(t, s, model)}, created)
	providertest.AssertDiagnosticsContain(t, created.Diagnostics, "The project my-project was not found. Check the key")

	model.ID = types.StringValue("my-org/my-project/group/Developers")
	state := providertest.EmptyState(t, s)
	if diags := state.Set(t.Context(), &model); diags.HasError() {
		t.Fatal(diags)
	}
	read := &resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, read)
	if read.Diagnostics.HasError() || !read.State.Raw.IsNull() || read.Diagnostics.WarningsCount() != 1 ||
		!strings.Contains(read.Diagnostics[0].Summary(), "project my-project") {
		t.Errorf("read: %v, state = %v", read.Diagnostics, read.State.Raw)
	}
	deleted := &resource.DeleteResponse{State: state}
	r.Delete(t.Context(), resource.DeleteRequest{State: state}, deleted)
	if deleted.Diagnostics.HasError() {
		t.Errorf("destroy of a missing project: %v", deleted.Diagnostics)
	}
}

func TestProjectPermissionsUser(t *testing.T) {
	instance := newFakeProjectPermissionInstance(map[string][]string{})
	instance.users["alice"] = nil
	r := instance.start(t)
	s := projectPermissionsSchema(t)
	plan := projectPermissionModel("", "issueadmin")
	plan.GroupName, plan.Login = types.StringNull(), types.StringValue("alice")
	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: projectPermissionPlan(t, s, plan)}, created)
	if created.Diagnostics.HasError() || !reflect.DeepEqual(instance.users["alice"], []string{"issueadmin"}) {
		t.Fatalf("create: %v, grants = %v", created.Diagnostics, instance.users)
	}
	if id := providertest.ReadModel[projectPermissionsModel](t, created.State).ID.ValueString(); id != "my-org/my-project/user/alice" {
		t.Errorf("id = %q", id)
	}
}

func TestProjectPermissionsImport(t *testing.T) {
	t.Parallel()
	r := NewProjectPermissionsResource().(*projectPermissionsResource)
	s := projectPermissionsSchema(t)
	for _, tc := range []struct {
		id, group, login string
	}{
		{"my-org/my-project/group/Developers", "Developers", ""},
		{"my-org/my-project/user/alice", "", "alice"},
	} {
		resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
		r.ImportState(t.Context(), resource.ImportStateRequest{ID: tc.id}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("import %s: %v", tc.id, resp.Diagnostics)
		}
		state := providertest.ReadModel[projectPermissionsModel](t, resp.State)
		if state.ID.ValueString() != tc.id || state.Organization.ValueString() != "my-org" || state.ProjectKey.ValueString() != "my-project" ||
			state.GroupName.ValueString() != tc.group || state.Login.ValueString() != tc.login {
			t.Errorf("import state = %+v", state)
		}
	}
	for _, id := range []string{"my-org/group/Developers", "my-org/my-project/group/anyone"} {
		resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
		r.ImportState(t.Context(), resource.ImportStateRequest{ID: id}, resp)
		if !resp.Diagnostics.HasError() {
			t.Errorf("import %q was accepted", id)
		}
	}
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "my-org/Developers"}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "<organization>/<project_key>/group/<name>")
}

func TestProjectPermissionsImportRoundTrip(t *testing.T) {
	t.Parallel()
	r := NewProjectPermissionsResource().(*projectPermissionsResource)
	s := projectPermissionsSchema(t)
	group := projectPermissionModel("team/backend", "scan")
	assertImportRoundTrip(t, &r.permissionsResource, s, &group)
	user := projectPermissionModel("", "scan")
	user.GroupName, user.Login = types.StringNull(), types.StringValue("alice")
	assertImportRoundTrip(t, &r.permissionsResource, s, &user)
}
