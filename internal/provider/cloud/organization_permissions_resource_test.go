package cloud

import (
	"context"
	"encoding/json"
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

func organizationPermissionsSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewOrganizationPermissionsResource().Schema(t.Context(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func permissionModel(group string, permissions ...string) organizationPermissionsModel {
	elements := make([]attr.Value, len(permissions))
	for i, permission := range permissions {
		elements[i] = types.StringValue(permission)
	}
	return organizationPermissionsModel{
		ID: types.StringNull(), Organization: types.StringValue("my-org"), GroupName: types.StringValue(group),
		Login: types.StringNull(), Permissions: types.SetValueMust(types.StringType, elements),
	}
}

func permissionPlan(t *testing.T, s schema.Schema, model organizationPermissionsModel) tfsdk.Plan {
	t.Helper()
	plan := tfsdk.Plan{Schema: s}
	if diags := plan.Set(t.Context(), &model); diags.HasError() {
		t.Fatal(diags)
	}
	return plan
}

// fakePermissionInstance answers like the server: the read lists hold the
// principals without permissions too, and q keeps the names that hold it.
type fakePermissionInstance struct {
	groups map[string][]string
	users  map[string][]string
	calls  []string
}

func (f *fakePermissionInstance) start(t *testing.T) *organizationPermissionsResource {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	r := NewOrganizationPermissionsResource().(*organizationPermissionsResource)
	r.client = providertest.NewCloudClient(srv)
	return r
}

func (f *fakePermissionInstance) serve(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.Form.Get("organization") != "my-org" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/permissions/groups":
		f.serveList(w, r, "groups", "name", f.groups)
	case r.Method == http.MethodGet && r.URL.Path == "/api/permissions/users":
		f.serveList(w, r, "users", "login", f.users)
	case r.Method == http.MethodPost:
		f.serveWrite(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakePermissionInstance) serveList(w http.ResponseWriter, r *http.Request, field, key string, grants map[string][]string) {
	entries := []map[string]any{}
	for name, permissions := range grants {
		if strings.Contains(strings.ToLower(name), strings.ToLower(r.Form.Get("q"))) {
			entries = append(entries, map[string]any{key: name, "permissions": permissions})
		}
	}
	json.NewEncoder(w).Encode(map[string]any{field: entries, "paging": map[string]int{"total": len(entries)}})
}

func TestOrganizationPermissionsCreateForGroupWithoutGrants(t *testing.T) {
	instance := &fakePermissionInstance{groups: map[string][]string{"Developers": {}, "Developers-old": {"admin"}}}
	r := instance.start(t)
	s := organizationPermissionsSchema(t)
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: permissionPlan(t, s, permissionModel("Developers", "scan"))}, resp)
	if resp.Diagnostics.HasError() || !reflect.DeepEqual(instance.groups["Developers"], []string{"scan"}) {
		t.Fatalf("create: %v, grants = %v", resp.Diagnostics, instance.groups)
	}
	if !reflect.DeepEqual(instance.groups["Developers-old"], []string{"admin"}) {
		t.Errorf("a group with a similar name changed: %v", instance.groups)
	}
	read := &resource.ReadResponse{State: resp.State}
	r.Read(t.Context(), resource.ReadRequest{State: resp.State}, read)
	if read.Diagnostics.HasError() || read.State.Raw.IsNull() {
		t.Errorf("read after create: %v, state = %v", read.Diagnostics, read.State.Raw)
	}
}

func TestOrganizationPermissionsCreateForMissingPrincipal(t *testing.T) {
	instance := &fakePermissionInstance{groups: map[string][]string{}}
	r := instance.start(t)
	s := organizationPermissionsSchema(t)
	for _, organization := range []string{"my-org", "other-org"} {
		model := permissionModel("Developers", "scan")
		model.Organization = types.StringValue(organization)
		resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
		r.Create(t.Context(), resource.CreateRequest{Plan: permissionPlan(t, s, model)}, resp)
		if !resp.Diagnostics.HasError() || len(instance.calls) != 0 {
			t.Errorf("%s: diagnostics = %v, calls = %v", organization, resp.Diagnostics, instance.calls)
		}
	}
}

func (f *fakePermissionInstance) serveWrite(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	permission := r.Form.Get("permission")
	group := r.Form.Get("groupName")
	login := r.Form.Get("login")
	f.calls = append(f.calls, r.URL.Path+" "+group+login+" "+permission)
	grants, name := f.groups, group
	if login != "" {
		grants, name = f.users, login
	}
	if _, found := grants[name]; !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.URL.Path == "/api/permissions/add_group" || r.URL.Path == "/api/permissions/add_user" {
		if !slices.Contains(grants[name], permission) {
			grants[name] = append(grants[name], permission)
		}
	} else {
		grants[name] = slices.DeleteFunc(grants[name], func(value string) bool { return value == permission })
	}
	w.WriteHeader(http.StatusNoContent)
}

func TestOrganizationPermissionsSchema(t *testing.T) {
	t.Parallel()
	s := organizationPermissionsSchema(t)
	if err := s.ValidateImplementation(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !s.Attributes["organization"].IsRequired() || !s.Attributes["permissions"].IsRequired() ||
		!s.Attributes["group_name"].IsOptional() || !s.Attributes["login"].IsOptional() {
		t.Error("the permission resource has wrong required and optional attributes")
	}
	if len(NewOrganizationPermissionsResource().(*organizationPermissionsResource).ConfigValidators(t.Context())) == 0 {
		t.Error("the principal choice has no config validator")
	}
}

func TestOrganizationPermissionsPrincipalChoice(t *testing.T) {
	t.Parallel()
	r := NewOrganizationPermissionsResource().(*organizationPermissionsResource)
	s := organizationPermissionsSchema(t)
	for _, tc := range []struct {
		name  string
		group types.String
		login types.String
		error bool
	}{
		{"group", types.StringValue("Developers"), types.StringNull(), false},
		{"user", types.StringNull(), types.StringValue("alice"), false},
		{"neither", types.StringNull(), types.StringNull(), true},
		{"both", types.StringValue("Developers"), types.StringValue("alice"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := permissionModel("Developers", "scan")
			model.GroupName, model.Login = tc.group, tc.login
			config := tfsdk.Config{Schema: s, Raw: permissionPlan(t, s, model).Raw}
			resp := &resource.ValidateConfigResponse{}
			for _, validator := range r.ConfigValidators(t.Context()) {
				validator.ValidateResource(t.Context(), resource.ValidateConfigRequest{Config: config}, resp)
			}
			if resp.Diagnostics.HasError() != tc.error {
				t.Errorf("validation error = %v, want %t", resp.Diagnostics, tc.error)
			}
		})
	}
}

func TestOrganizationPermissionsValues(t *testing.T) {
	t.Parallel()
	s := organizationPermissionsSchema(t)
	group := s.Attributes["group_name"].(schema.StringAttribute)
	groupResponse := &validator.StringResponse{}
	for _, validate := range group.Validators {
		validate.ValidateString(t.Context(), validator.StringRequest{ConfigValue: types.StringValue("anyone")}, groupResponse)
	}
	if !groupResponse.Diagnostics.HasError() {
		t.Error("Anyone was accepted")
	}
	permissions := s.Attributes["permissions"].(schema.SetAttribute)
	for _, tc := range []struct {
		name   string
		values []attr.Value
	}{
		{"empty", nil},
		{"invalid", []attr.Value{types.StringValue("projectadmin")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := &validator.SetResponse{}
			for _, validate := range permissions.Validators {
				validate.ValidateSet(t.Context(), validator.SetRequest{
					ConfigValue: types.SetValueMust(types.StringType, tc.values),
				}, response)
			}
			if !response.Diagnostics.HasError() {
				t.Errorf("%s permission set was accepted", tc.name)
			}
		})
	}
}

func TestOrganizationPermissionsGroupLifecycleAndDrift(t *testing.T) {
	instance := &fakePermissionInstance{groups: map[string][]string{
		"Developers": {"scan", "gateadmin"}, "Owners": {"admin"},
	}}
	r := instance.start(t)
	s := organizationPermissionsSchema(t)
	ctx := t.Context()
	plan := permissionModel("Developers", "scan", "profileadmin")
	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(ctx, resource.CreateRequest{Plan: permissionPlan(t, s, plan)}, created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	if !reflect.DeepEqual(instance.calls, []string{
		"/api/permissions/add_group Developers profileadmin", "/api/permissions/remove_group Developers gateadmin",
	}) {
		t.Errorf("create calls = %v", instance.calls)
	}
	state := providertest.ReadModel[organizationPermissionsModel](t, created.State)
	if state.ID.ValueString() != "my-org/group/Developers" {
		t.Errorf("created state = %+v", state)
	}

	second := &resource.UpdateResponse{State: created.State}
	r.Update(ctx, resource.UpdateRequest{Plan: permissionPlan(t, s, plan), State: created.State}, second)
	if second.Diagnostics.HasError() || len(instance.calls) != 2 {
		t.Fatalf("second apply: %v, calls = %v", second.Diagnostics, instance.calls)
	}
	instance.groups["Developers"] = append(instance.groups["Developers"], "provisioning")
	refreshed := &resource.ReadResponse{State: second.State}
	r.Read(ctx, resource.ReadRequest{State: second.State}, refreshed)
	if refreshed.Diagnostics.HasError() {
		t.Fatal(refreshed.Diagnostics)
	}
	refreshState := providertest.ReadModel[organizationPermissionsModel](t, refreshed.State)
	var observed []string
	if diags := refreshState.Permissions.ElementsAs(ctx, &observed, false); diags.HasError() || !slices.Contains(observed, "provisioning") {
		t.Errorf("drift permissions = %v, diagnostics = %v", observed, diags)
	}

	changedPlan := permissionModel("Developers", "scan")
	updated := &resource.UpdateResponse{State: refreshed.State}
	r.Update(ctx, resource.UpdateRequest{Plan: permissionPlan(t, s, changedPlan), State: refreshed.State}, updated)
	if updated.Diagnostics.HasError() || !reflect.DeepEqual(instance.groups["Developers"], []string{"scan"}) {
		t.Fatalf("update: %v, grants = %v", updated.Diagnostics, instance.groups["Developers"])
	}
	deleted := &resource.DeleteResponse{State: updated.State}
	r.Delete(ctx, resource.DeleteRequest{State: updated.State}, deleted)
	if deleted.Diagnostics.HasError() || len(instance.groups["Developers"]) != 0 || !reflect.DeepEqual(instance.groups["Owners"], []string{"admin"}) {
		t.Errorf("destroy: %v, groups = %v", deleted.Diagnostics, instance.groups)
	}
}

func TestOrganizationPermissionsUserAndMissingGrant(t *testing.T) {
	instance := &fakePermissionInstance{users: map[string][]string{"alice": {}}}
	r := instance.start(t)
	s := organizationPermissionsSchema(t)
	plan := permissionModel("", "scan")
	plan.GroupName = types.StringNull()
	plan.Login = types.StringValue("alice")
	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: permissionPlan(t, s, plan)}, created)
	if created.Diagnostics.HasError() || !reflect.DeepEqual(instance.users["alice"], []string{"scan"}) {
		t.Fatalf("create: %v, grants = %v", created.Diagnostics, instance.users)
	}
	instance.users["alice"] = nil
	read := &resource.ReadResponse{State: created.State}
	r.Read(t.Context(), resource.ReadRequest{State: created.State}, read)
	if read.Diagnostics.HasError() || !read.State.Raw.IsNull() {
		t.Errorf("empty grants did not remove resource: %v, state = %v", read.Diagnostics, read.State.Raw)
	}
}

func TestOrganizationPermissionsImport(t *testing.T) {
	t.Parallel()
	r := NewOrganizationPermissionsResource().(*organizationPermissionsResource)
	s := organizationPermissionsSchema(t)
	for _, id := range []string{"my-org/group/Developers", "my-org/user/alice"} {
		resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("import %s: %v", id, resp.Diagnostics)
		}
		state := providertest.ReadModel[organizationPermissionsModel](t, resp.State)
		if state.ID.ValueString() != id || state.Organization.ValueString() != "my-org" {
			t.Errorf("import state = %+v", state)
		}
	}
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "my-org/group/anyone"}, resp)
	if !resp.Diagnostics.HasError() {
		t.Error("Anyone import was accepted")
	}
}

func TestOrganizationPermissionsReadMissingPrincipal(t *testing.T) {
	instance := &fakePermissionInstance{groups: map[string][]string{"Developers": {"scan"}}}
	r := instance.start(t)
	s := organizationPermissionsSchema(t)
	for _, tc := range []struct {
		name         string
		organization string
		warning      bool
	}{
		{"missing group", "my-org", false},
		{"missing organization", "other-org", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := permissionModel("Developers", "scan")
			model.Organization = types.StringValue(tc.organization)
			model.ID = types.StringValue(tc.organization + "/group/Developers")
			state := providertest.EmptyState(t, s)
			if diags := state.Set(t.Context(), &model); diags.HasError() {
				t.Fatal(diags)
			}
			delete(instance.groups, "Developers")
			resp := &resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, resp)
			if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() || (resp.Diagnostics.WarningsCount() > 0) != tc.warning {
				t.Errorf("diagnostics = %v, state = %v", resp.Diagnostics, resp.State.Raw)
			}
		})
	}
}
