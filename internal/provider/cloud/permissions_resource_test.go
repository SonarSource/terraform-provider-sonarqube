package cloud

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

// assertImportRoundTrip checks that the ID that the resource writes imports
// to the same scope and principal, so that scopeAttributes and the scope of
// the model stay in the same order.
func assertImportRoundTrip[M any, P permissionsModelPointer[M]](t *testing.T, r *permissionsResource[M, P], s schema.Schema, model P) {
	t.Helper()
	id := r.importID(model)
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: id}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("import %s: %v", id, resp.Diagnostics)
	}
	imported := providertest.ReadModel[M](t, resp.State)
	got := P(&imported)
	if got.scope() != model.scope() || got.principal() != model.principal() || r.importID(got) != id {
		t.Errorf("import of %q = %+v, %+v, want %+v, %+v", id, got.scope(), got.principal(), model.scope(), model.principal())
	}
}

func TestOrganizationPermissionsImportRoundTrip(t *testing.T) {
	t.Parallel()
	r := NewOrganizationPermissionsResource().(*organizationPermissionsResource)
	s := organizationPermissionsSchema(t)
	group := permissionModel("team/backend", "scan")
	assertImportRoundTrip(t, &r.permissionsResource, s, &group)
	user := permissionModel("", "scan")
	user.GroupName, user.Login = types.StringNull(), types.StringValue("alice")
	assertImportRoundTrip(t, &r.permissionsResource, s, &user)
}

func TestParsePermissionImportID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		id         string
		scopeParts int
		scope      []string
		kind, name string
	}{
		{"my-org/group/Developers", 1, []string{"my-org"}, "group", "Developers"},
		{"my-org/user/alice", 1, []string{"my-org"}, "user", "alice"},
		{"my-org/group/team/backend", 1, []string{"my-org"}, "group", "team/backend"},
		{"my-org/my-project/group/Developers", 2, []string{"my-org", "my-project"}, "group", "Developers"},
		{"my-org/my-project/user/alice", 2, []string{"my-org", "my-project"}, "user", "alice"},
	} {
		scope, kind, name, ok := parsePermissionImportID(tc.id, tc.scopeParts)
		if !ok || !reflect.DeepEqual(scope, tc.scope) || kind != tc.kind || name != tc.name {
			t.Errorf("parse %q = %v, %q, %q, %t", tc.id, scope, kind, name, ok)
		}
	}
	for _, tc := range []struct {
		id         string
		scopeParts int
	}{
		{"", 1}, {"my-org/group", 1}, {"/group/Developers", 1}, {"my-org/other/name", 1}, {"my-org/group/", 1},
		{"my-org/group/Developers", 2}, {"my-org//group/Developers", 2}, {"my-org/my-project/other/name", 2},
	} {
		if _, _, _, ok := parsePermissionImportID(tc.id, tc.scopeParts); ok {
			t.Errorf("accepted invalid ID %q with %d scope parts", tc.id, tc.scopeParts)
		}
	}
}

func TestPermissionImportID(t *testing.T) {
	t.Parallel()
	group := client.PermissionPrincipal{Group: true, Name: "Developers"}
	user := client.PermissionPrincipal{Name: "alice"}
	for _, tc := range []struct {
		scope      client.PermissionScope
		principal  client.PermissionPrincipal
		scopeParts int
		want       string
	}{
		{client.PermissionScope{Organization: "my-org"}, group, 1, "my-org/group/Developers"},
		{client.PermissionScope{Organization: "my-org"}, user, 1, "my-org/user/alice"},
		{client.PermissionScope{Organization: "my-org", ProjectKey: "my-project"}, group, 2, "my-org/my-project/group/Developers"},
		{client.PermissionScope{Organization: "my-org", ProjectKey: "my-project"}, user, 2, "my-org/my-project/user/alice"},
	} {
		if got := permissionImportID(tc.scope, tc.principal, tc.scopeParts); got != tc.want {
			t.Errorf("import ID = %q, want %q", got, tc.want)
		}
	}
}

func TestPermissionDiff(t *testing.T) {
	t.Parallel()
	adds, removes := permissionDiff([]string{"gateadmin", "scan"}, []string{"scan", "admin"})
	if !reflect.DeepEqual(adds, []string{"admin"}) || !reflect.DeepEqual(removes, []string{"gateadmin"}) {
		t.Errorf("diff = %v, %v", adds, removes)
	}
}
