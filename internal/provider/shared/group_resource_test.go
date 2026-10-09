package shared

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

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
	Default     bool
	Managed     bool
}

type fakeGroups struct {
	groups map[string]*fakeGroup
	nextID int
	// searches counts the group searches. hiddenSearches is the number of
	// searches that answer with no group, as the eventually consistent search
	// of SonarQube Cloud can do after a write.
	searches       int
	hiddenSearches int
}

func newFakeGroups(t *testing.T) (*fakeGroups, *client.Client) {
	t.Helper()
	f := &fakeGroups{groups: map[string]*fakeGroup{}, nextID: 1}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, providertest.NewCloudClient(srv)
}

func (f *fakeGroups) search(w http.ResponseWriter) {
	f.searches++
	groups := make([]map[string]any, 0, len(f.groups))
	if f.hiddenSearches > 0 {
		f.hiddenSearches--
		_ = json.NewEncoder(w).Encode(map[string]any{"groups": groups, "paging": map[string]int{"total": 0}})
		return
	}
	for _, group := range f.groups {
		id, _ := strconv.Atoi(group.ID)
		groups = append(groups, map[string]any{"id": id, "name": group.Name,
			"description": group.Description, "default": group.Default})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"groups": groups, "paging": map[string]int{"total": len(groups)}})
}

func (f *fakeGroups) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	switch r.URL.Path {
	case "/api/user_groups/search":
		f.search(w)
	case "/api/user_groups/create":
		id := strconv.Itoa(f.nextID)
		f.nextID++
		group := &fakeGroup{ID: id, Name: r.PostForm.Get("name"), Description: r.PostForm.Get("description")}
		f.groups[id] = group
		_ = json.NewEncoder(w).Encode(map[string]any{"group": map[string]any{
			"id": f.nextID - 1, "name": group.Name, "description": group.Description}})
	case "/api/user_groups/update":
		group := f.groups[r.PostForm.Get("id")]
		if group == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if group.Managed {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":[{"msg":"IdP-managed group cannot be updated"}]}`))
			return
		}
		if values, ok := r.PostForm["name"]; ok {
			group.Name = values[0]
		}
		if values, ok := r.PostForm["description"]; ok {
			group.Description = values[0]
		}
		w.WriteHeader(http.StatusNoContent)
	case "/api/user_groups/delete":
		if group := f.groups[r.PostForm.Get("id")]; group != nil && group.Managed {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":[{"msg":"IdP-managed group cannot be deleted"}]}`))
			return
		}
		delete(f.groups, r.PostForm.Get("id"))
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
		map[string]string{"organization": "my-org", "name": "First"})}}, create)
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	created := providertest.ReadModel[groupResourceModel](t, create.State)
	if created.ID.ValueString() != "1" || !created.Description.IsNull() {
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

func TestGroupResourceImportAndBuiltInGroup(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	f.groups["7"] = &fakeGroup{ID: "7", Name: "Imported"}
	f.groups["8"] = &fakeGroup{ID: "8", Name: "Members"}
	r := &groupResource{client: c}
	s := groupSchema(t)
	imported := &resource.ImportStateResponse{State: tfsdk.State{Schema: s,
		Raw: providertest.SchemaValue(t, s, nil)}}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "my-org/Imported"}, imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	read := &resource.ReadResponse{State: imported.State}
	r.Read(t.Context(), resource.ReadRequest{State: imported.State}, read)
	if read.Diagnostics.HasError() || providertest.ReadModel[groupResourceModel](t, read.State).ID.ValueString() != "7" {
		t.Fatalf("import did not resolve the numeric ID: %v", read.Diagnostics)
	}
	invalid := &resource.ImportStateResponse{State: imported.State}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "no-slash"}, invalid)
	providertest.AssertDiagnosticsContain(t, invalid.Diagnostics, "Use <organization>/<name>")
	builtIn := &resource.ReadResponse{State: tfsdk.State{Schema: s,
		Raw: providertest.SchemaValue(t, s, map[string]string{"organization": "my-org", "name": "Members"})}}
	r.Read(t.Context(), resource.ReadRequest{State: builtIn.State}, builtIn)
	providertest.AssertDiagnosticsContain(t, builtIn.Diagnostics, "Members group")
}

func TestGroupDescriptionState(t *testing.T) {
	t.Parallel()
	if !groupDescriptionState("", types.StringNull()).IsNull() {
		t.Error("empty server description must keep null configuration")
	}
	if !groupDescriptionState("", types.StringValue("")).Equal(types.StringValue("")) {
		t.Error("explicit empty description must remain empty")
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
			"id": "3", "organization": "my-org", "name": "Developers",
		})}}, resp)
	if resp.Diagnostics.HasError() || f.groups["3"].Description != "" {
		t.Fatalf("description was not cleared: %v, group=%+v", resp.Diagnostics, f.groups["3"])
	}
	if !providertest.ReadModel[groupResourceModel](t, resp.State).Description.IsNull() {
		t.Error("empty description did not become null")
	}
}

func TestGroupResourceRefusesOwnersImport(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	f.groups["9"] = &fakeGroup{ID: "9", Name: "Owners"}
	r := &groupResource{client: c}
	s := groupSchema(t)
	imported := &resource.ReadResponse{State: tfsdk.State{Schema: s,
		Raw: providertest.SchemaValue(t, s, map[string]string{"organization": "my-org", "name": "Owners"})}}
	r.Read(t.Context(), resource.ReadRequest{State: imported.State}, imported)
	providertest.AssertDiagnosticsContain(t, imported.Diagnostics, "Owners group")
}

func TestGroupResourceRefusesOwnersDeleteWithoutRefresh(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	f.groups["9"] = &fakeGroup{ID: "9", Name: "Owners"}
	r := &groupResource{client: c}
	s := groupSchema(t)
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "9", "organization": "my-org", "name": "Owners",
	})}
	resp := &resource.DeleteResponse{}
	r.Delete(t.Context(), resource.DeleteRequest{State: state}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Owners group")
	if f.groups["9"] == nil {
		t.Fatal("the Owners group was deleted")
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

func TestGroupResourceImportOfMissingGroupExplainsName(t *testing.T) {
	t.Parallel()
	_, c := newFakeGroups(t)
	r := &groupResource{client: c}
	s := groupSchema(t)
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s,
		Raw: providertest.SchemaValue(t, s, map[string]string{"organization": "my-org", "name": "Absent"})}}
	r.Read(t.Context(), resource.ReadRequest{State: resp.State}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "must match exactly")
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

func TestGroupResourceReadSearchesAgainForNewGroup(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	f.groups["4"] = &fakeGroup{ID: "4", Name: "New"}
	f.hiddenSearches = 2
	r := &groupResource{client: c, missingRetries: []time.Duration{0, 0, 0}}
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

func TestGroupResourceReadRemovesGroupAfterRetries(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	r := &groupResource{client: c, missingRetries: []time.Duration{0, 0}}
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
	providertest.AssertDiagnosticsContain(t, update.Diagnostics, "IdP-managed group cannot be updated")
	if providertest.ReadModel[groupResourceModel](t, update.State).Name.ValueString() != "Managed" {
		t.Error("a rejected update changed state")
	}
	deletion := &resource.DeleteResponse{}
	r.Delete(t.Context(), resource.DeleteRequest{State: prior}, deletion)
	providertest.AssertDiagnosticsContain(t, deletion.Diagnostics, "IdP-managed group cannot be deleted")
	if f.groups["10"] == nil {
		t.Error("a rejected delete removed the group")
	}
}
