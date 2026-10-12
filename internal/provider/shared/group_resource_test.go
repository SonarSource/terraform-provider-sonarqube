package shared

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func groupSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewGroupResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func TestGroupResourceSchema(t *testing.T) {
	t.Parallel()
	s := groupSchema(t)
	if err := s.ValidateImplementation(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !s.Attributes["organization"].IsRequired() || !s.Attributes["name"].IsRequired() || !s.Attributes["id"].IsComputed() {
		t.Fatal("required and computed group attributes are incorrect")
	}
	if !providertest.ValidateString(t, s, "description", string(make([]byte, 201))).HasError() {
		t.Error("a description over 200 characters was accepted")
	}
}

type fakeGroup struct {
	ID          string
	Name        string
	Description string
	BuiltIn     bool
	Managed     bool
}

// fakeGroups serves the organization read and the group API of Web API v2.
type fakeGroups struct {
	groups map[string]*fakeGroup
	nextID int
	// searches counts the group searches. hiddenSearches is the number of
	// searches that answer with no group, as the eventually consistent search
	// of SonarQube Cloud can do after a write.
	searches       int
	hiddenSearches int
	// lastUpdate is the body of the last update call.
	lastUpdate map[string]string
}

func newFakeGroups(t *testing.T) (*fakeGroups, *client.Client) {
	t.Helper()
	f := &fakeGroups{groups: map[string]*fakeGroup{}, nextID: 1}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, providertest.NewCloudClient(srv)
}

func (f *fakeGroups) search(w http.ResponseWriter, r *http.Request) {
	f.searches++
	groups := make([]map[string]any, 0, len(f.groups))
	if f.hiddenSearches > 0 {
		f.hiddenSearches--
	} else {
		ids, q := r.URL.Query().Get("ids"), r.URL.Query().Get("q")
		for _, group := range f.groups {
			if (ids == "" || ids == group.ID) && strings.Contains(group.Name, q) {
				groups = append(groups, map[string]any{"id": group.ID, "name": group.Name,
					"description": group.Description, "builtIn": group.BuiltIn, "managed": group.Managed})
			}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"groups": groups, "page": map[string]int{"total": len(groups)}})
}

func (f *fakeGroups) refuseManaged(w http.ResponseWriter, id string) bool {
	if group := f.groups[id]; group != nil && group.Managed {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"IdP-managed group cannot be changed"}`))
		return true
	}
	return false
}

func (f *fakeGroups) serve(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	_ = json.NewDecoder(r.Body).Decode(&body)
	id := strings.TrimPrefix(r.URL.Path, "/users/groups/")
	switch {
	case r.URL.Path == "/organizations/organizations":
		_, _ = w.Write([]byte(`[{"key":"my-org","uuidV4":"00000000-0000-4000-8000-000000000001"}]`))
	case r.URL.Path == "/users/groups" && r.Method == http.MethodGet:
		f.search(w, r)
	case r.URL.Path == "/users/groups" && r.Method == http.MethodPost:
		group := &fakeGroup{ID: strconv.Itoa(f.nextID), Name: body["name"], Description: body["description"]}
		f.nextID++
		f.groups[group.ID] = group
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": group.ID, "name": group.Name, "description": group.Description})
	case r.Method == http.MethodPatch:
		f.lastUpdate = body
		group := f.groups[id]
		if group == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if f.refuseManaged(w, id) {
			return
		}
		group.Name, group.Description = body["name"], body["description"]
		_ = json.NewEncoder(w).Encode(map[string]any{"id": group.ID, "name": group.Name})
	case r.Method == http.MethodDelete:
		if f.refuseManaged(w, id) {
			return
		}
		delete(f.groups, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestGroupResourceLifecycleAndDrift(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	r := &groupResource{client: c}
	s := groupSchema(t)
	create := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: providertest.SchemaValue(t, s,
		map[string]string{"organization": "my-org", "name": "First", "description": ""})}}, create)
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	created := providertest.ReadModel[groupResourceModel](t, create.State)
	if created.ID.ValueString() != "1" || created.Description.ValueString() != "" {
		t.Errorf("created state = %+v", created)
	}
	read := &resource.ReadResponse{State: create.State}
	r.Read(t.Context(), resource.ReadRequest{State: create.State}, read)
	if read.Diagnostics.HasError() || providertest.ReadModel[groupResourceModel](t, read.State).Name.ValueString() != "First" {
		t.Fatalf("second read did not retain the created group: %v", read.Diagnostics)
	}
	update := &resource.UpdateResponse{State: read.State}
	r.Update(t.Context(), resource.UpdateRequest{State: read.State, Plan: tfsdk.Plan{Schema: s,
		Raw: providertest.SchemaValue(t, s, map[string]string{"id": "1", "organization": "my-org",
			"name": "Second", "description": "Changed"})}}, update)
	if update.Diagnostics.HasError() || f.groups["1"].Name != "Second" {
		t.Fatalf("rename failed: %v, group=%+v", update.Diagnostics, f.groups["1"])
	}
	if providertest.ReadModel[groupResourceModel](t, update.State).ID.ValueString() != "1" {
		t.Error("rename changed the group ID")
	}
	f.groups["1"].Name = "Outside rename"
	drift := &resource.ReadResponse{State: update.State}
	r.Read(t.Context(), resource.ReadRequest{State: update.State}, drift)
	if drift.Diagnostics.HasError() || providertest.ReadModel[groupResourceModel](t, drift.State).Name.ValueString() != "Outside rename" {
		t.Fatalf("outside rename did not appear in state: %v", drift.Diagnostics)
	}
	delete(f.groups, "1")
	missing := &resource.ReadResponse{State: drift.State}
	r.Read(t.Context(), resource.ReadRequest{State: drift.State}, missing)
	if missing.Diagnostics.HasError() || !missing.State.Raw.IsNull() {
		t.Fatalf("outside deletion did not remove state: %v", missing.Diagnostics)
	}
}

func importGroup(t *testing.T, r *groupResource, id string) *resource.ImportStateResponse {
	t.Helper()
	s := groupSchema(t)
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	resp.State.Raw = providertest.SchemaValue(t, s, nil)
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: id}, resp)
	return resp
}

func TestGroupResourceImportFindsID(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	f.groups["7"] = &fakeGroup{ID: "7", Name: "Imported"}
	r := &groupResource{client: c}
	imported := importGroup(t, r, "my-org/Imported")
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	if id := providertest.ReadModel[groupResourceModel](t, imported.State).ID.ValueString(); id != "7" {
		t.Errorf("imported id = %q, want 7", id)
	}
	providertest.AssertDiagnosticsContain(t, importGroup(t, r, "no-slash").Diagnostics, "Use <organization>/<name>")
	providertest.AssertDiagnosticsContain(t, importGroup(t, r, "my-org/Absent").Diagnostics, "must match exactly")
}

func TestGroupResourceRefusesBuiltInGroupImport(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	f.groups["8"] = &fakeGroup{ID: "8", Name: "Members"}
	f.groups["9"] = &fakeGroup{ID: "9", Name: "Owners"}
	r := &groupResource{client: c}
	for _, name := range []string{"Members", "Owners"} {
		providertest.AssertDiagnosticsContain(t, importGroup(t, r, "my-org/"+name).Diagnostics, name+" group")
	}
	if f.searches != 0 {
		t.Errorf("searches = %d, want 0", f.searches)
	}
}

func TestGroupResourceClearsDescription(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	f.groups["3"] = &fakeGroup{ID: "3", Name: "Developers", Description: "Old"}
	r := &groupResource{client: c}
	s := groupSchema(t)
	prior := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "3", "organization": "my-org", "name": "Developers", "description": "Old",
	})}
	resp := &resource.UpdateResponse{State: prior}
	r.Update(t.Context(), resource.UpdateRequest{State: prior, Plan: tfsdk.Plan{Schema: s,
		Raw: providertest.SchemaValue(t, s, map[string]string{
			"id": "3", "organization": "my-org", "name": "Developers", "description": "",
		})}}, resp)
	if resp.Diagnostics.HasError() || f.groups["3"].Description != "" {
		t.Fatalf("description was not cleared: %v, group=%+v", resp.Diagnostics, f.groups["3"])
	}
	if d := providertest.ReadModel[groupResourceModel](t, resp.State).Description; d.ValueString() != "" || d.IsNull() {
		t.Errorf("description = %v, want empty", d)
	}
}

func TestGroupResourceWarnsWhenOrganizationIsGone(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	r := &groupResource{client: providertest.NewCloudClient(srv)}
	s := groupSchema(t)
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "3", "organization": "my-org", "name": "Developers",
	})}
	resp := &resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Fatalf("a missing organization did not remove the group: %v", resp.Diagnostics)
	}
	if resp.Diagnostics.WarningsCount() != 1 {
		t.Errorf("warnings = %d, want 1", resp.Diagnostics.WarningsCount())
	}
}

func TestGroupResourceWritesStateWithoutSearch(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	r := &groupResource{client: c}
	s := groupSchema(t)
	create := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: providertest.SchemaValue(t, s,
		map[string]string{"organization": "my-org", "name": "First", "description": "Initial"})}}, create)
	update := &resource.UpdateResponse{State: create.State}
	r.Update(t.Context(), resource.UpdateRequest{State: create.State, Plan: tfsdk.Plan{Schema: s,
		Raw: providertest.SchemaValue(t, s, map[string]string{"id": "1", "organization": "my-org",
			"name": "Second", "description": "Changed"})}}, update)
	if create.Diagnostics.HasError() || update.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics, update.Diagnostics)
	}
	if f.searches != 0 {
		t.Errorf("searches = %d, want 0: a search after a write can answer with old values", f.searches)
	}
	if id := providertest.ReadModel[groupResourceModel](t, create.State).ID.ValueString(); id != "1" {
		t.Errorf("created id = %q, want 1", id)
	}
	got := providertest.ReadModel[groupResourceModel](t, update.State)
	if got.ID.ValueString() != "1" || got.Name.ValueString() != "Second" || got.Description.ValueString() != "Changed" {
		t.Errorf("state = %+v", got)
	}
}

func TestGroupResourceUpdateSendsNameWithDescriptionChange(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	f.groups["3"] = &fakeGroup{ID: "3", Name: "Developers", Description: "Old"}
	r := &groupResource{client: c}
	s := groupSchema(t)
	prior := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "3", "organization": "my-org", "name": "Developers", "description": "Old",
	})}
	resp := &resource.UpdateResponse{State: prior}
	r.Update(t.Context(), resource.UpdateRequest{State: prior, Plan: tfsdk.Plan{Schema: s,
		Raw: providertest.SchemaValue(t, s, map[string]string{
			"id": "3", "organization": "my-org", "name": "Developers", "description": "New",
		})}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	// The server writes back the fields that an update does not send, from a
	// copy that can be old. The name must thus go with each update.
	if f.lastUpdate["name"] != "Developers" || f.lastUpdate["description"] != "New" {
		t.Errorf("update form = %v, want the name and the description", f.lastUpdate)
	}
}

func TestGroupResourceReadSearchesAgainForNewGroup(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	f.groups["4"] = &fakeGroup{ID: "4", Name: "New"}
	f.hiddenSearches = 2
	r := &groupResource{client: c, readRetries: []time.Duration{0, 0, 0}}
	s := groupSchema(t)
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "4", "organization": "my-org", "name": "New",
	})}
	resp := &resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
		t.Fatalf("a group that the search showed late was removed: %v", resp.Diagnostics)
	}
	if f.searches != 3 {
		t.Errorf("searches = %d, want 3", f.searches)
	}
}

func TestGroupResourceReadWaitsForStaleValues(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	f.groups["4"] = &fakeGroup{ID: "4", Name: "Old"}
	r := &groupResource{client: c, readRetries: []time.Duration{0, 0}}
	s := groupSchema(t)
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "4", "organization": "my-org", "name": "New", "description": "",
	})}
	resp := &resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	// The server keeps the old name after all retries, so this is real drift.
	if got := providertest.ReadModel[groupResourceModel](t, resp.State).Name.ValueString(); got != "Old" || f.searches != 3 {
		t.Errorf("name = %q after %d searches, want Old after 3", got, f.searches)
	}
}

func TestGroupResourceReadAfterImportDoesNotWaitForDescription(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	f.groups["4"] = &fakeGroup{ID: "4", Name: "Imported", Description: "From the server"}
	r := &groupResource{client: c, readRetries: []time.Duration{0, 0}}
	s := groupSchema(t)
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "4", "organization": "my-org", "name": "Imported",
	})}
	resp := &resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() || f.searches != 1 {
		t.Fatalf("searches = %d, want 1: %v", f.searches, resp.Diagnostics)
	}
	if got := providertest.ReadModel[groupResourceModel](t, resp.State).Description.ValueString(); got != "From the server" {
		t.Errorf("description = %q", got)
	}
}

func TestGroupResourceReadRemovesGroupAfterRetries(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	r := &groupResource{client: c, readRetries: []time.Duration{0, 0}}
	s := groupSchema(t)
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "4", "organization": "my-org", "name": "Gone",
	})}
	resp := &resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Fatalf("a deleted group stayed in the state: %v", resp.Diagnostics)
	}
	if f.searches != 3 {
		t.Errorf("searches = %d, want 3", f.searches)
	}
}

func TestGroupResourceRefusesInvalidOrganizationKey(t *testing.T) {
	t.Parallel()
	if !providertest.ValidateString(t, groupSchema(t), "organization", "Not-A-Key").HasError() {
		t.Error("an organization key with upper-case letters was accepted")
	}
}

func TestGroupResourceKeepsManagedGroupOnRejectedChanges(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	f.groups["10"] = &fakeGroup{ID: "10", Name: "Managed", Managed: true}
	r := &groupResource{client: c}
	s := groupSchema(t)
	prior := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "10", "organization": "my-org", "name": "Managed",
	})}
	update := &resource.UpdateResponse{State: prior}
	r.Update(t.Context(), resource.UpdateRequest{State: prior, Plan: tfsdk.Plan{Schema: s,
		Raw: providertest.SchemaValue(t, s, map[string]string{
			"id": "10", "organization": "my-org", "name": "Renamed",
		})}}, update)
	providertest.AssertDiagnosticsContain(t, update.Diagnostics, "IdP-managed group cannot be changed")
	if providertest.ReadModel[groupResourceModel](t, update.State).Name.ValueString() != "Managed" {
		t.Error("a rejected update changed state")
	}
	deletion := &resource.DeleteResponse{}
	r.Delete(t.Context(), resource.DeleteRequest{State: prior}, deletion)
	providertest.AssertDiagnosticsContain(t, deletion.Diagnostics, "IdP-managed group cannot be changed")
	if f.groups["10"] == nil {
		t.Error("a rejected delete removed the group")
	}
}
