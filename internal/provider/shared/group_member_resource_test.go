package shared

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func groupMemberSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewGroupMemberResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func TestGroupMemberResourceSchema(t *testing.T) {
	t.Parallel()
	s := groupMemberSchema(t)
	if err := s.ValidateImplementation(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"organization", "group_name", "login"} {
		if !s.Attributes[name].IsRequired() {
			t.Errorf("%s is not required", name)
		}
	}
	if !providertest.ValidateString(t, s, "login", "alice/bob").HasError() {
		t.Error("a login with \"/\" was accepted")
	}
	if !providertest.ValidateString(t, s, "organization", "My Org").HasError() {
		t.Error("an invalid organization key was accepted")
	}
}

// fakeMembers serves the v2 group search and the v1 membership web services.
type fakeMembers struct {
	// members holds the logins of each group. A group that is not a key does
	// not exist.
	members map[string]map[string]bool
	managed map[string]bool
	// hiddenReads is the number of member reads that answer with no member,
	// as the member list of SonarQube Cloud can do after a change.
	hiddenReads int
	// listHidden makes the v2 group search answer with no group.
	listHidden bool
	// removeStatus and removeMessage, when set, are the answer of remove_user.
	removeStatus  int
	removeMessage string
}

func newFakeMembers(t *testing.T) (*fakeMembers, *groupMemberResource) {
	t.Helper()
	f := &fakeMembers{members: map[string]map[string]bool{"team/a": {}}, managed: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, &groupMemberResource{client: providertest.NewCloudClient(srv)}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"errors":[{"msg":%q}]}`, message)
}

func (f *fakeMembers) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	group, login := r.Form.Get("name"), r.Form.Get("login")
	if r.URL.Path == "/organizations/organizations" {
		_, _ = w.Write([]byte(`[{"key":"my-org","uuidV4":"00000000-0000-4000-8000-000000000001"}]`))
		return
	}
	if r.URL.Path == "/users/groups" {
		groups := []map[string]any{}
		if name := r.Form.Get("q"); !f.listHidden && f.members[name] != nil {
			groups = append(groups, map[string]any{"id": "1", "name": name, "managed": f.managed[name]})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"groups": groups, "page": map[string]int{"total": len(groups)}})
		return
	}
	members := f.members[group]
	if members == nil {
		writeError(w, http.StatusNotFound, "No group with name '"+group+"' in organization 'my-org'")
		return
	}
	switch r.URL.Path {
	case "/api/user_groups/add_user":
		members[login] = true
		w.WriteHeader(http.StatusNoContent)
	case "/api/user_groups/remove_user":
		if f.removeStatus != 0 {
			writeError(w, f.removeStatus, f.removeMessage)
			return
		}
		delete(members, login)
		w.WriteHeader(http.StatusNoContent)
	case "/api/user_groups/users":
		users := []map[string]string{}
		if f.hiddenReads > 0 {
			f.hiddenReads--
			members = nil
		}
		for member := range members {
			users = append(users, map[string]string{"login": member})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"users": users, "total": len(users)})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func memberPlan(t *testing.T, s schema.Schema, group string) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan{Schema: s, Raw: providertest.SchemaValue(t, s,
		map[string]string{"organization": "my-org", "group_name": group, "login": "alice@github"})}
}

func createMember(t *testing.T, r *groupMemberResource, group string) *resource.CreateResponse {
	t.Helper()
	s := groupMemberSchema(t)
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: memberPlan(t, s, group)}, resp)
	return resp
}

func readMember(t *testing.T, r *groupMemberResource, state tfsdk.State) *resource.ReadResponse {
	t.Helper()
	resp := &resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, resp)
	return resp
}

func TestGroupMemberResourceLifecycleAndDrift(t *testing.T) {
	t.Parallel()
	f, r := newFakeMembers(t)
	f.members["team/a"]["bob@github"] = true
	create := createMember(t, r, "team/a")
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	if id := providertest.ReadModel[groupMemberResourceModel](t, create.State).ID.ValueString(); id != "my-org/team/a/alice@github" {
		t.Errorf("id = %q", id)
	}
	read := readMember(t, r, create.State)
	if read.Diagnostics.HasError() || read.State.Raw.IsNull() {
		t.Fatalf("read lost the membership: %v", read.Diagnostics)
	}
	delete(f.members["team/a"], "alice@github")
	if drift := readMember(t, r, read.State); drift.Diagnostics.HasError() || !drift.State.Raw.IsNull() {
		t.Fatalf("an outside removal did not remove the state: %v", drift.Diagnostics)
	}
	f.members["team/a"]["alice@github"] = true
	del := &resource.DeleteResponse{State: read.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: read.State}, del)
	if del.Diagnostics.HasError() || f.members["team/a"]["alice@github"] || !f.members["team/a"]["bob@github"] {
		t.Fatalf("delete: %v, members = %v", del.Diagnostics, f.members["team/a"])
	}
}

func TestGroupMemberResourceReadWaitsForNewMembership(t *testing.T) {
	t.Parallel()
	f, r := newFakeMembers(t)
	r.readRetries = []time.Duration{time.Millisecond, time.Millisecond}
	create := createMember(t, r, "team/a")
	f.hiddenReads = 2
	if read := readMember(t, r, create.State); read.Diagnostics.HasError() || read.State.Raw.IsNull() {
		t.Fatalf("a lagging member list removed the membership: %v", read.Diagnostics)
	}
	f.hiddenReads = 3
	if read := readMember(t, r, create.State); !read.State.Raw.IsNull() {
		t.Fatal("a membership that stays missing after the retries kept the state")
	}
}

func TestGroupMemberResourceRefusesManagedGroup(t *testing.T) {
	t.Parallel()
	f, r := newFakeMembers(t)
	f.managed["team/a"] = true
	providertest.AssertDiagnosticsContain(t, createMember(t, r, "team/a").Diagnostics, "SCIM")
	if f.members["team/a"]["alice@github"] {
		t.Error("the user was added to a managed group")
	}
}

func TestGroupMemberResourceAddsWhenGroupListLags(t *testing.T) {
	t.Parallel()
	f, r := newFakeMembers(t)
	f.listHidden = true
	if resp := createMember(t, r, "team/a"); resp.Diagnostics.HasError() || !f.members["team/a"]["alice@github"] {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
}

func TestGroupMemberResourceGroupGone(t *testing.T) {
	t.Parallel()
	f, r := newFakeMembers(t)
	create := createMember(t, r, "team/a")
	delete(f.members, "team/a")
	if read := readMember(t, r, create.State); read.Diagnostics.HasError() || !read.State.Raw.IsNull() {
		t.Fatalf("a deleted group did not remove the state: %v", read.Diagnostics)
	}
	del := &resource.DeleteResponse{State: create.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: create.State}, del)
	if del.Diagnostics.HasError() {
		t.Fatalf("delete of a membership in a deleted group: %v", del.Diagnostics)
	}
	providertest.AssertDiagnosticsContain(t, createMember(t, r, "team/a").Diagnostics, "No group with name")
}

func TestGroupMemberResourceDeleteExplainsLastAdministrator(t *testing.T) {
	t.Parallel()
	f, r := newFakeMembers(t)
	create := createMember(t, r, "team/a")
	f.removeStatus, f.removeMessage = http.StatusBadRequest, "The last administrator user cannot be removed"
	del := &resource.DeleteResponse{State: create.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: create.State}, del)
	providertest.AssertDiagnosticsContain(t, del.Diagnostics, "Give the admin permission directly")
}

func TestGroupMemberResourceWarnsWhenOrganizationIsGone(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "No organization with key 'my-org'")
	}))
	t.Cleanup(srv.Close)
	r := &groupMemberResource{client: providertest.NewCloudClient(srv)}
	s := groupMemberSchema(t)
	state := tfsdk.State{Schema: s, Raw: memberPlan(t, s, "team/a").Raw}
	resp := readMember(t, r, state)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() || resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("a missing organization: %v", resp.Diagnostics)
	}
}

func TestGroupMemberResourceImport(t *testing.T) {
	t.Parallel()
	s := groupMemberSchema(t)
	r := &groupMemberResource{}
	importMember := func(id string) *resource.ImportStateResponse {
		resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
		resp.State.Raw = providertest.SchemaValue(t, s, nil)
		r.ImportState(t.Context(), resource.ImportStateRequest{ID: id}, resp)
		return resp
	}
	resp := importMember("my-org/team/a/alice@github")
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	got := providertest.ReadModel[groupMemberResourceModel](t, resp.State)
	if got.Organization.ValueString() != "my-org" || got.GroupName.ValueString() != "team/a" || got.Login.ValueString() != "alice@github" {
		t.Errorf("imported = %+v", got)
	}
	for _, id := range []string{"my-org", "my-org/alice", "/team/alice", "My Org/team/alice", "my-org//alice", "my-org/team/"} {
		providertest.AssertDiagnosticsContain(t, importMember(id).Diagnostics, "<organization>/<group_name>/<login>")
	}
}
