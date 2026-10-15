package shared

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func permissionTemplateSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewPermissionTemplateResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func TestPermissionTemplateResourceSchema(t *testing.T) {
	t.Parallel()
	s := permissionTemplateSchema(t)
	if err := s.ValidateImplementation(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !s.Attributes["organization"].IsRequired() || !s.Attributes["name"].IsRequired() {
		t.Error("organization and name must be required")
	}
	if !providertest.ValidateString(t, s, "name", strings.Repeat("a", 101)).HasError() {
		t.Error("a name of 101 characters was accepted")
	}
	if !providertest.ValidateString(t, s, "description", "").HasError() {
		t.Error("an empty description was accepted")
	}
}

// fakeTemplate is a template of fakeTemplates. The grant maps go from the
// group name or the login to the permissions.
type fakeTemplate struct {
	name, description, pattern string
	creator                    []string
	groups, users              map[string][]string
}

// fakeTemplates serves the permission template web services.
type fakeTemplates struct {
	t         *testing.T
	templates map[string]*fakeTemplate
	members   []string
	groups    []string
	nextID    int
	calls     []string
	// failGrants makes each grant change answer 403.
	failGrants bool
}

func newFakeTemplates(t *testing.T) (*fakeTemplates, *permissionTemplateResource) {
	t.Helper()
	f := &fakeTemplates{t: t, templates: map[string]*fakeTemplate{},
		members: []string{"alice", "bob"}, groups: []string{"Developers", "Owners"}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, &permissionTemplateResource{client: providertest.NewCloudClient(srv)}
}

func (f *fakeTemplates) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	action := strings.TrimPrefix(r.URL.Path, "/api/permissions/")
	f.calls = append(f.calls, action)
	switch action {
	case "create_template":
		f.nextID++
		id := fmt.Sprintf("template-%d", f.nextID)
		f.templates[id] = &fakeTemplate{name: r.Form.Get("name"), description: r.Form.Get("description"),
			pattern: r.Form.Get("projectKeyPattern"), groups: map[string][]string{}, users: map[string][]string{}}
		writeJSON(w, map[string]any{"permissionTemplate": map[string]string{"id": id, "name": r.Form.Get("name")}})
		return
	case "search_templates":
		f.writeTemplates(w)
		return
	}
	id := r.Form.Get("templateId")
	if action == "update_template" {
		id = r.Form.Get("id")
	}
	template := f.templates[id]
	if template == nil {
		writeError(w, http.StatusNotFound, "Permission template with id '"+id+"' is not found")
		return
	}
	switch {
	case action == "update_template":
		template.name, template.description, template.pattern = r.Form.Get("name"), r.Form.Get("description"), r.Form.Get("projectKeyPattern")
	case action == "delete_template":
		delete(f.templates, id)
	case action == "template_groups":
		f.writeGrants(w, "groups", "name", f.groups, template.groups)
		return
	case action == "template_users":
		f.writeGrants(w, "users", "login", f.members, template.users)
		return
	case f.failGrants:
		writeError(w, http.StatusForbidden, "Organization 'my-org' is not allowed to use custom permission templates")
		return
	default:
		f.changeGrant(action, r.Form.Get("groupName"), r.Form.Get("login"), r.Form.Get("permission"), template)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeTemplates) changeGrant(action, group, login, permission string, template *fakeTemplate) {
	change := func(permissions []string) []string {
		permissions = slices.DeleteFunc(slices.Clone(permissions), func(p string) bool { return p == permission })
		if strings.HasPrefix(action, "add_") {
			permissions = append(permissions, permission)
		}
		return permissions
	}
	switch {
	case strings.Contains(action, "_group_"):
		template.groups[group] = change(template.groups[group])
	case strings.Contains(action, "_user_"):
		template.users[login] = change(template.users[login])
	case strings.Contains(action, "_project_creator_"):
		template.creator = change(template.creator)
	default:
		f.t.Errorf("unexpected action %s", action)
	}
}

func (f *fakeTemplates) writeTemplates(w http.ResponseWriter) {
	templates := []map[string]any{}
	for id, template := range f.templates {
		permissions := []map[string]any{}
		for _, key := range []string{"admin", "codeviewer", "user"} {
			permissions = append(permissions, map[string]any{"key": key, "withProjectCreator": slices.Contains(template.creator, key)})
		}
		templates = append(templates, map[string]any{"id": id, "name": template.name, "description": template.description,
			"projectKeyPattern": template.pattern, "permissions": permissions})
	}
	writeJSON(w, map[string]any{"permissionTemplates": templates, "defaultTemplates": []map[string]string{{"templateId": "template-0", "qualifier": "TRK"}}})
}

// writeGrants lists every principal, also the ones without permissions, as
// the server does.
func (f *fakeTemplates) writeGrants(w http.ResponseWriter, list, key string, principals []string, grants map[string][]string) {
	entries := []map[string]any{}
	for _, principal := range principals {
		permissions := grants[principal]
		if permissions == nil {
			permissions = []string{}
		}
		entries = append(entries, map[string]any{key: principal, "permissions": permissions})
	}
	writeJSON(w, map[string]any{list: entries, "paging": map[string]int{"total": len(entries)}})
}

func writeJSON(w http.ResponseWriter, value any) {
	_ = json.NewEncoder(w).Encode(value)
}

func templatePlan(t *testing.T, s schema.Schema, model permissionTemplateResourceModel) tfsdk.Plan {
	t.Helper()
	plan := tfsdk.Plan{Schema: s, Raw: providertest.EmptyState(t, s).Raw}
	if diagnostics := plan.Set(t.Context(), &model); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	return plan
}

func permissionSet(values ...string) types.Set {
	if len(values) == 0 {
		return types.SetNull(types.StringType)
	}
	return types.SetValueMust(types.StringType, toValues(values))
}

func toValues(values []string) []attr.Value {
	out := make([]attr.Value, 0, len(values))
	for _, value := range values {
		out = append(out, types.StringValue(value))
	}
	return out
}

func groupSet(t *testing.T, groups map[string][]string) types.Set {
	t.Helper()
	models := []templateGroupModel{}
	for name, permissions := range groups {
		models = append(models, templateGroupModel{Name: types.StringValue(name), Permissions: permissionSet(permissions...)})
	}
	set, diagnostics := types.SetValueFrom(t.Context(), templateGroupType, models)
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	return set
}

func userSet(t *testing.T, users map[string][]string) types.Set {
	t.Helper()
	models := []templateUserModel{}
	for login, permissions := range users {
		models = append(models, templateUserModel{Login: types.StringValue(login), Permissions: permissionSet(permissions...)})
	}
	set, diagnostics := types.SetValueFrom(t.Context(), templateUserType, models)
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	return set
}

func backendTemplate(t *testing.T) permissionTemplateResourceModel {
	t.Helper()
	return permissionTemplateResourceModel{
		ID:                        types.StringUnknown(),
		Organization:              types.StringValue("my-org"),
		Name:                      types.StringValue("Backend"),
		Description:               types.StringValue("Backend projects"),
		ProjectKeyPattern:         types.StringValue("backend-.*"),
		ProjectCreatorPermissions: permissionSet("admin"),
		Groups:                    groupSet(t, map[string][]string{"Developers": {"user", "codeviewer"}}),
		Users:                     userSet(t, map[string][]string{"alice": {"admin"}}),
	}
}

func createTemplate(t *testing.T, r *permissionTemplateResource, model permissionTemplateResourceModel) *resource.CreateResponse {
	t.Helper()
	s := permissionTemplateSchema(t)
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: templatePlan(t, s, model)}, resp)
	return resp
}

func readTemplate(t *testing.T, r *permissionTemplateResource, state tfsdk.State) *resource.ReadResponse {
	t.Helper()
	resp := &resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, resp)
	return resp
}

func TestPermissionTemplateResourceLifecycleAndDrift(t *testing.T) {
	t.Parallel()
	f, r := newFakeTemplates(t)
	create := createTemplate(t, r, backendTemplate(t))
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	created := f.templates["template-1"]
	if created == nil || !slices.Equal(created.creator, []string{"admin"}) || !slices.Equal(created.users["alice"], []string{"admin"}) ||
		len(created.groups["Developers"]) != 2 {
		t.Fatalf("created = %+v", created)
	}

	// A refresh right after the apply gives the plan back, so the next plan
	// shows no change.
	read := readTemplate(t, r, create.State)
	if read.Diagnostics.HasError() || !read.State.Raw.Equal(create.State.Raw) {
		t.Fatalf("read changed the state: %v\nread  %v\nplan  %v", read.Diagnostics, read.State.Raw, create.State.Raw)
	}

	// A grant added outside Terraform shows in the state.
	created.groups["Owners"] = []string{"admin"}
	drift := providertest.ReadModel[permissionTemplateResourceModel](t, readTemplate(t, r, read.State).State)
	if !drift.Groups.Equal(groupSet(t, map[string][]string{"Developers": {"user", "codeviewer"}, "Owners": {"admin"}})) {
		t.Fatalf("drift groups = %v", drift.Groups)
	}

	// The update removes the grants that are not in the plan, and an
	// omitted description or pattern removes the value.
	s := permissionTemplateSchema(t)
	planned := backendTemplate(t)
	planned.ID = types.StringValue("template-1")
	planned.Name = types.StringValue("Backend services")
	planned.Description = types.StringNull()
	planned.ProjectKeyPattern = types.StringNull()
	planned.ProjectCreatorPermissions = permissionSet()
	planned.Users = userSet(t, map[string][]string{"bob": {"user"}})
	update := &resource.UpdateResponse{State: read.State}
	r.Update(t.Context(), resource.UpdateRequest{Plan: templatePlan(t, s, planned), State: read.State}, update)
	if update.Diagnostics.HasError() {
		t.Fatal(update.Diagnostics)
	}
	if created.name != "Backend services" || created.description != "" || created.pattern != "" || len(created.creator) != 0 ||
		len(created.groups["Owners"]) != 0 || len(created.users["alice"]) != 0 || !slices.Equal(created.users["bob"], []string{"user"}) {
		t.Fatalf("updated = %+v", created)
	}
	if after := readTemplate(t, r, update.State); !after.State.Raw.Equal(update.State.Raw) {
		t.Fatalf("read after update changed the state\nread  %v\nplan  %v", after.State.Raw, update.State.Raw)
	}

	del := &resource.DeleteResponse{State: update.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: update.State}, del)
	if del.Diagnostics.HasError() || len(f.templates) != 0 {
		t.Fatalf("delete: %v, templates = %v", del.Diagnostics, f.templates)
	}
	if gone := readTemplate(t, r, update.State); gone.Diagnostics.HasError() || !gone.State.Raw.IsNull() {
		t.Fatalf("a deleted template kept the state: %v", gone.Diagnostics)
	}
}

func TestPermissionTemplateResourceKeepsTemplateWhenGrantFails(t *testing.T) {
	t.Parallel()
	f, r := newFakeTemplates(t)
	f.failGrants = true
	resp := createTemplate(t, r, backendTemplate(t))
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Team or the Enterprise plan")
	state := providertest.ReadModel[permissionTemplateResourceModel](t, resp.State)
	if state.ID.ValueString() != "template-1" || len(state.Groups.Elements()) != 0 || !state.ProjectCreatorPermissions.IsNull() {
		t.Fatalf("state = %+v", state)
	}
}

func TestPermissionTemplateResourceWarnsWhenOrganizationIsGone(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "No organization with key 'my-org'")
	}))
	t.Cleanup(srv.Close)
	r := &permissionTemplateResource{client: providertest.NewCloudClient(srv)}
	model := backendTemplate(t)
	model.ID = types.StringValue("template-1")
	s := permissionTemplateSchema(t)
	resp := readTemplate(t, r, tfsdk.State{Schema: s, Raw: templatePlan(t, s, model).Raw})
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() || resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("a missing organization: %v", resp.Diagnostics)
	}
}

func TestPermissionTemplateResourceImport(t *testing.T) {
	t.Parallel()
	f, r := newFakeTemplates(t)
	f.templates["template-7"] = &fakeTemplate{name: "Team/Backend"}
	s := permissionTemplateSchema(t)
	importTemplate := func(id string) *resource.ImportStateResponse {
		resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
		resp.State.Raw = providertest.SchemaValue(t, s, nil)
		r.ImportState(t.Context(), resource.ImportStateRequest{ID: id}, resp)
		return resp
	}
	resp := importTemplate("my-org/team/backend")
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	got := providertest.ReadModel[permissionTemplateResourceModel](t, resp.State)
	if got.ID.ValueString() != "template-7" || got.Organization.ValueString() != "my-org" {
		t.Errorf("imported %+v", got)
	}
	providertest.AssertDiagnosticsContain(t, importTemplate("my-org/Frontend").Diagnostics, "not found")
	providertest.AssertDiagnosticsContain(t, importTemplate("my-org").Diagnostics, "Use <organization>/<name>")
}

func TestPermissionTemplateResourceRefusesDuplicatePrincipals(t *testing.T) {
	t.Parallel()
	s := permissionTemplateSchema(t)
	model := backendTemplate(t)
	duplicate := []templateUserModel{
		{Login: types.StringValue("alice"), Permissions: permissionSet("admin")},
		{Login: types.StringValue("alice"), Permissions: permissionSet("user")},
	}
	var diagnostics diag.Diagnostics
	model.Users, diagnostics = types.SetValueFrom(t.Context(), templateUserType, duplicate)
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	config := tfsdk.Config{Schema: s, Raw: templatePlan(t, s, model).Raw}
	resp := &resource.ValidateConfigResponse{}
	(&permissionTemplateResource{}).ValidateConfig(t.Context(), resource.ValidateConfigRequest{Config: config}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, `"alice" is in more than one user block`)
}
