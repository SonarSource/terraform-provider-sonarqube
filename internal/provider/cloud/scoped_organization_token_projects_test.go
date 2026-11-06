package cloud

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const missingProjectID = "00000000-0000-4000-8000-0000000000ff"

func projectTokenModel(id string, keys ...string) scopedOrganizationTokenModel {
	return scopedOrganizationTokenModel{
		ID: types.StringValue(id), Organization: types.StringValue(providertest.TokenOrganization), Name: types.StringValue("test"),
		Token: types.StringValue("test-secret"), CreatedAt: types.StringValue("2026-01-01T00:00:00Z"),
		OrganizationPermissions: types.SetNull(types.StringType), ProjectPermissions: tokenSet("scan"), ProjectKeys: tokenSet(keys...),
	}
}

func tokenState(t *testing.T, model scopedOrganizationTokenModel) tfsdk.State {
	t.Helper()
	state := providertest.EmptyState(t, tokenSchema(t))
	if diagnostics := state.Set(t.Context(), &model); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	return state
}

func createProjectToken(t *testing.T, server *providertest.TokenServer, keys ...string) resource.CreateResponse {
	t.Helper()
	model := projectTokenModel("", keys...)
	model.ID, model.Token, model.CreatedAt = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
	plan := tfsdk.Plan(tokenState(t, model))
	resp := resource.CreateResponse{State: providertest.EmptyState(t, tokenSchema(t))}
	r := &scopedOrganizationTokenResource{client: providertest.NewCloudClient(server.Server)}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &resp)
	return resp
}

func TestTokenCreateLinksProjects(t *testing.T) {
	server := providertest.NewTokenServer(t)
	resp := createProjectToken(t, server, providertest.FirstProjectKey, providertest.SecondProjectKey)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	token, _ := server.TokenNamed("test")
	if got := server.LinkedResources(token.ID); !slices.Equal(got, []string{providertest.FirstProjectID, providertest.SecondProjectID}) {
		t.Fatalf("links=%v", got)
	}
	if token.AutoGrantProjectPermissions || len(token.RoleAssignments) != 1 || token.RoleAssignments[0].ResourceType != client.TokenResourceProject {
		t.Fatalf("token=%+v", token)
	}
}

func TestTokenCreateRevokesTokenWhenLinkFails(t *testing.T) {
	for _, tc := range []struct {
		name       string
		failRevoke bool
	}{{"revoked", false}, {"revocation fails", true}} {
		t.Run(tc.name, func(t *testing.T) {
			server := providertest.NewTokenServer(t)
			server.FailLinkCreate, server.FailRevoke = true, tc.failRevoke
			resp := createProjectToken(t, server, providertest.FirstProjectKey)
			_, remains := server.TokenNamed("test")
			if !resp.Diagnostics.HasError() || remains != tc.failRevoke || resp.State.Raw.IsNull() == tc.failRevoke {
				t.Fatalf("diagnostics=%v, token remains=%v, state kept=%v", resp.Diagnostics, remains, !resp.State.Raw.IsNull())
			}
		})
	}
}

func TestTokenCreateStopsForMissingProject(t *testing.T) {
	server := providertest.NewTokenServer(t)
	resp := createProjectToken(t, server, providertest.FirstProjectKey, "missing-project")
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "missing-project") || !resp.State.Raw.IsNull() {
		t.Fatalf("diagnostics=%v", resp.Diagnostics)
	}
	server.AssertEmpty(t)
}

func TestTokenUpdateChangesOnlyLinks(t *testing.T) {
	server := providertest.NewTokenServer(t)
	id := server.AddToken("test")
	server.AddLink(id, client.TokenResourceProject, providertest.FirstProjectID)
	server.AddLink(id, client.TokenResourceProject, missingProjectID)
	server.AddLink(id, "portfolio", "portfolio-uuid")
	state := tokenState(t, projectTokenModel(id, providertest.FirstProjectKey))
	plan := tfsdk.Plan(tokenState(t, projectTokenModel(id, providertest.SecondProjectKey)))
	resp := resource.UpdateResponse{State: state}
	r := &scopedOrganizationTokenResource{client: providertest.NewCloudClient(server.Server)}
	r.Update(t.Context(), resource.UpdateRequest{Plan: plan, State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := server.LinkedResources(id); !slices.Equal(got, []string{providertest.SecondProjectID, "portfolio-uuid"}) {
		t.Fatalf("links=%v", got)
	}
	got := providertest.ReadModel[scopedOrganizationTokenModel](t, resp.State)
	if got.Token.ValueString() != "test-secret" || got.ID.ValueString() != id || len(got.ProjectKeys.Elements()) != 1 {
		t.Fatalf("state=%+v", got)
	}
}

func updateProjectToken(t *testing.T, server *providertest.TokenServer, prior, planned scopedOrganizationTokenModel) resource.UpdateResponse {
	t.Helper()
	state := tokenState(t, prior)
	resp := resource.UpdateResponse{State: state}
	r := &scopedOrganizationTokenResource{client: providertest.NewCloudClient(server.Server)}
	r.Update(t.Context(), resource.UpdateRequest{Plan: tfsdk.Plan(tokenState(t, planned)), State: state}, &resp)
	return resp
}

// An unknown project_keys that becomes null plans an update of an
// organization-wide token. Its links, such as automatic grants, must stay.
func TestTokenUpdateKeepsLinksOfOrganizationWideToken(t *testing.T) {
	server := providertest.NewTokenServer(t)
	id := server.AddToken("test", client.TokenRoleAssignment{ResourceType: client.TokenResourceOrganization, Roles: []string{"provisioning"}})
	server.AddLink(id, client.TokenResourceProject, providertest.FirstProjectID)
	model := projectTokenModel(id)
	model.OrganizationPermissions, model.ProjectKeys = tokenSet("provisioning"), types.SetNull(types.StringType)
	resp := updateProjectToken(t, server, model, model)
	if resp.Diagnostics.HasError() || !slices.Equal(server.LinkedResources(id), []string{providertest.FirstProjectID}) {
		t.Fatalf("diagnostics=%v, links=%v", resp.Diagnostics, server.LinkedResources(id))
	}
}

func TestTokenUpdateFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(*providertest.TokenServer)
		keys   []string
		detail string
	}{
		{"missing project", func(*providertest.TokenServer) {}, []string{"missing-project"}, "missing-project"},
		{"token revoked", func(s *providertest.TokenServer) { s.LinksGone = true }, []string{providertest.SecondProjectKey}, "revoked"},
		{"link fails", func(s *providertest.TokenServer) { s.FailLinkCreate = true }, []string{providertest.SecondProjectKey}, "apply again"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := providertest.NewTokenServer(t)
			id := server.AddToken("test")
			server.AddLink(id, client.TokenResourceProject, providertest.FirstProjectID)
			tc.setup(server)
			prior := projectTokenModel(id, providertest.FirstProjectKey)
			resp := updateProjectToken(t, server, prior, projectTokenModel(id, tc.keys...))
			if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), tc.detail) {
				t.Fatalf("diagnostics=%v", resp.Diagnostics)
			}
			if !resp.State.Raw.Equal(tokenState(t, prior).Raw) {
				t.Fatal("a failed update changed the state")
			}
		})
	}
}

func TestTokenReadProjectKeys(t *testing.T) {
	project := client.TokenResourceProject
	for _, tc := range []struct {
		name     string
		links    [][2]string
		keys     []string
		warnings int
	}{
		{"linked", [][2]string{{project, providertest.FirstProjectID}, {project, providertest.SecondProjectID}}, []string{providertest.FirstProjectKey, providertest.SecondProjectKey}, 0},
		{"deleted project", [][2]string{{project, providertest.FirstProjectID}, {project, missingProjectID}}, []string{providertest.FirstProjectKey}, 1},
		{"no links", nil, nil, 0},
		{"other resource type", [][2]string{{project, providertest.FirstProjectID}, {"portfolio", "portfolio-uuid"}}, []string{providertest.FirstProjectKey}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := providertest.NewTokenServer(t)
			id := server.AddToken("test")
			for _, link := range tc.links {
				server.AddLink(id, link[0], link[1])
			}
			state := tokenState(t, projectTokenModel(id, providertest.FirstProjectKey, providertest.SecondProjectKey))
			resp := resource.ReadResponse{State: state}
			r := &scopedOrganizationTokenResource{client: providertest.NewCloudClient(server.Server)}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != tc.warnings {
				t.Fatalf("diagnostics=%v", resp.Diagnostics)
			}
			var keys []string
			providertest.ReadModel[scopedOrganizationTokenModel](t, resp.State).ProjectKeys.ElementsAs(t.Context(), &keys, false)
			slices.Sort(keys)
			if !slices.Equal(keys, tc.keys) {
				t.Fatalf("project_keys=%v, want %v", keys, tc.keys)
			}
		})
	}
}

// The resource does not manage the links of an organization-wide token, so
// they do not show as drift.
func TestTokenReadIgnoresLinksOfOrganizationWideToken(t *testing.T) {
	server := providertest.NewTokenServer(t)
	id := server.AddToken("test", client.TokenRoleAssignment{ResourceType: client.TokenResourceOrganization, Roles: []string{"scan"}})
	server.AddLink(id, client.TokenResourceProject, providertest.FirstProjectID)
	model := projectTokenModel(id)
	model.OrganizationPermissions, model.ProjectPermissions, model.ProjectKeys = tokenSet("scan"), types.SetNull(types.StringType), types.SetNull(types.StringType)
	state := tokenState(t, model)
	resp := resource.ReadResponse{State: state}
	r := &scopedOrganizationTokenResource{client: providertest.NewCloudClient(server.Server)}
	r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() || !providertest.ReadModel[scopedOrganizationTokenModel](t, resp.State).ProjectKeys.IsNull() {
		t.Fatalf("diagnostics=%v", resp.Diagnostics)
	}
}

// The token can disappear between the token list and the link list.
func TestTokenReadRemovesTokenWhenLinksAreGone(t *testing.T) {
	server := providertest.NewTokenServer(t)
	server.LinksGone = true
	state := tokenState(t, projectTokenModel(server.AddToken("test"), providertest.FirstProjectKey))
	resp := resource.ReadResponse{State: state}
	r := &scopedOrganizationTokenResource{client: providertest.NewCloudClient(server.Server)}
	r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Fatalf("diagnostics=%v, removed=%v", resp.Diagnostics, resp.State.Raw.IsNull())
	}
}

func TestLinkChanges(t *testing.T) {
	first := client.Project{ID: "uuid-1", Key: "first"}
	second := client.Project{ID: "uuid-2", Key: "second"}
	current := partitionLinks([]client.TokenResourceLink{
		{ID: "link-1", ResourceType: client.TokenResourceProject, ResourceID: "uuid-1"},
		{ID: "link-gone", ResourceType: client.TokenResourceProject, ResourceID: "uuid-gone"},
		{ID: "link-p", ResourceType: "portfolio", ResourceID: "uuid-p"},
	})
	add, remove := linkChanges([]client.Project{first, second}, current)
	if !slices.Equal(add, []client.Project{second}) || !slices.Equal(remove, []string{"link-gone"}) {
		t.Fatalf("add=%v, remove=%v", add, remove)
	}
	if add, remove := linkChanges(nil, partitionLinks(nil)); add != nil || remove != nil {
		t.Fatalf("no change expected: add=%v, remove=%v", add, remove)
	}
}

// A list answer can hold more projects than the values asked for.
func TestMatchProjectsIgnoresExtraProjects(t *testing.T) {
	projects := []client.Project{{ID: "uuid-1", Key: "first"}, {ID: "uuid-x", Key: "first-extra"}}
	found, missing := matchProjects([]string{"first", "absent"}, projects, func(p client.Project) string { return p.Key })
	if len(found) != 1 || found[0].ID != "uuid-1" || !slices.Equal(missing, []string{"absent"}) {
		t.Fatalf("found=%v, missing=%v", found, missing)
	}
}

// failingReadServer answers the organization and the token, and 500 for each
// project or link read.
func failingReadServer(t *testing.T, tokenID string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/organizations/organizations":
			fmt.Fprintf(w, `[{"key":%q,"uuidV4":%q}]`, providertest.TokenOrganization, providertest.TokenOrganizationID)
		case "/authentication/token-definitions":
			fmt.Fprintf(w, `{"tokenDefinitions":[{"id":%q,"organizationId":%q,"name":"test","roleAssignments":[{"resourceType":"project","roles":["scan"]}]}],"page":{"total":1}}`, tokenID, providertest.TokenOrganizationID)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTokenProjectReadErrorsKeepState(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000009"
	r := &scopedOrganizationTokenResource{client: providertest.NewCloudClient(failingReadServer(t, id))}
	state := tokenState(t, projectTokenModel(id, providertest.FirstProjectKey))

	read := resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, &read)
	if !read.Diagnostics.HasError() || !read.State.Raw.Equal(state.Raw) {
		t.Fatalf("read: diagnostics=%v", read.Diagnostics)
	}

	model := projectTokenModel("", providertest.FirstProjectKey)
	model.ID, model.Token, model.CreatedAt = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
	create := resource.CreateResponse{State: providertest.EmptyState(t, tokenSchema(t))}
	r.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(tokenState(t, model))}, &create)
	if !create.Diagnostics.HasError() || !create.State.Raw.IsNull() {
		t.Fatalf("create: diagnostics=%v", create.Diagnostics)
	}
}
