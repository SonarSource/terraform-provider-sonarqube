package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// These tests drive the resource against a small instance held in memory, so
// they cover create, read, update, rename and delete without a Terraform
// binary and without credentials. The acceptance test covers the same ground
// against a live instance.

// fakeInstance answers the calls of both API surfaces: Web API v2 for the
// read, and the older web service for the writes.
type fakeInstance struct {
	organizations map[string]map[string]string
	calls         []string
}

func newFakeInstance() *fakeInstance {
	return &fakeInstance{organizations: map[string]map[string]string{}}
}

func (f *fakeInstance) start(t *testing.T) *client.Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)

	return newTestCloudClient(srv)
}

// notFound answers the way both surfaces answer for an entity they do not
// hold. Without it a write to an unknown key would reach a nil map and stop
// the test with a panic instead of a failure.
func (f *fakeInstance) notFound(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`{"errors":[{"msg":"not found"}]}`))
}

func (f *fakeInstance) serve(w http.ResponseWriter, r *http.Request) {
	f.calls = append(f.calls, r.URL.Path)

	if r.URL.Path == "/organizations/organizations" {
		org, found := f.organizations[r.URL.Query().Get("organizationKey")]
		if !found {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"not found"}`))
			return
		}
		json.NewEncoder(w).Encode([]map[string]string{org})
		return
	}

	_ = r.ParseForm()
	key := r.PostForm.Get("key")

	switch r.URL.Path {
	case "/api/organizations/create":
		f.organizations[key] = map[string]string{
			"key":         key,
			"name":        r.PostForm.Get("name"),
			"description": r.PostForm.Get("description"),
			"url":         r.PostForm.Get("url"),
			"avatarUrl":   r.PostForm.Get("avatar"),
		}
	case "/api/organizations/update":
		org, found := f.organizations[key]
		if !found {
			f.notFound(w)
			return
		}
		for parameter, field := range map[string]string{
			"name": "name", "description": "description", "url": "url", "avatar": "avatarUrl",
		} {
			if values, sent := r.PostForm[parameter]; sent {
				org[field] = values[0]
			}
		}
	case "/api/organizations/update_key":
		org, found := f.organizations[key]
		if !found {
			f.notFound(w)
			return
		}
		newKey := r.PostForm.Get("newKey")
		org["key"] = newKey
		f.organizations[newKey] = org
		delete(f.organizations, key)
	case "/api/organizations/delete":
		delete(f.organizations, r.PostForm.Get("organization"))
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func TestOrganizationResourceCreate(t *testing.T) {
	t.Parallel()

	instance := newFakeInstance()
	r := &organizationResource{client: instance.start(t)}

	s := organizationResourceSchema(t)
	resp := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: organizationValue(t, s, map[string]string{
			"key": "my-org", "name": "My Organization", "description": "Managed by Terraform",
		})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	stored, made := instance.organizations["my-org"]
	if !made {
		t.Fatal("the organization was not created")
	}
	if got, want := stored["name"], "My Organization"; got != want {
		t.Errorf("stored name = %q, want %q", got, want)
	}

	state := readModel(t, resp.State)
	if got, want := state.ID.ValueString(), "my-org"; got != want {
		t.Errorf("id = %q, want the key %q", got, want)
	}
	if !state.URL.IsNull() {
		t.Errorf("url = %v, want null, because the configuration left it out", state.URL)
	}
}

func TestOrganizationResourceRead(t *testing.T) {
	t.Parallel()

	instance := newFakeInstance()
	instance.organizations["my-org"] = map[string]string{"key": "my-org", "name": "My Organization"}
	r := &organizationResource{client: instance.start(t)}

	s := organizationResourceSchema(t)
	resp := &resource.ReadResponse{State: emptyState(t, s)}
	r.Read(context.Background(), resource.ReadRequest{
		State: tfsdk.State{Schema: s, Raw: organizationValue(t, s, map[string]string{"key": "my-org"})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if got, want := readModel(t, resp.State).Name.ValueString(), "My Organization"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
}

// An organization deleted outside Terraform leaves the state, so that the next
// plan makes it again.
func TestOrganizationResourceReadAfterAnOutsideDeletion(t *testing.T) {
	t.Parallel()

	r := &organizationResource{client: newFakeInstance().start(t)}

	s := organizationResourceSchema(t)
	resp := &resource.ReadResponse{State: emptyState(t, s)}
	r.Read(context.Background(), resource.ReadRequest{
		State: tfsdk.State{Schema: s, Raw: organizationValue(t, s, map[string]string{"key": "gone"})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("the organization stayed in the state although it is gone")
	}
}

// A new key renames the organization, and the rename goes before the update,
// because every other call finds the organization by its key.
func TestOrganizationResourceUpdateRenames(t *testing.T) {
	t.Parallel()

	instance := newFakeInstance()
	instance.organizations["old-key"] = map[string]string{"key": "old-key", "name": "Old name"}
	r := &organizationResource{client: instance.start(t)}

	s := organizationResourceSchema(t)
	resp := &resource.UpdateResponse{State: tfsdk.State{
		Schema: s,
		Raw:    organizationValue(t, s, map[string]string{"key": "old-key", "name": "Old name"}),
	}}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: organizationValue(t, s, map[string]string{
			"key": "new-key", "name": "New name",
		})},
		State: tfsdk.State{Schema: s, Raw: organizationValue(t, s, map[string]string{
			"key": "old-key", "name": "Old name",
		})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	if _, stillThere := instance.organizations["old-key"]; stillThere {
		t.Error("the old key still names an organization")
	}
	if got, want := instance.organizations["new-key"]["name"], "New name"; got != want {
		t.Errorf("stored name = %q, want %q", got, want)
	}

	assertCallOrder(t, instance.calls, "/api/organizations/update_key", "/api/organizations/update")

	state := readModel(t, resp.State)
	if got, want := state.ID.ValueString(), "new-key"; got != want {
		t.Errorf("id = %q, want %q", got, want)
	}
}

// An attribute taken out of the configuration is cleared, because the update
// sends it as an empty parameter.
func TestOrganizationResourceUpdateClearsADescription(t *testing.T) {
	t.Parallel()

	instance := newFakeInstance()
	instance.organizations["my-org"] = map[string]string{
		"key": "my-org", "name": "My Organization", "description": "Going away",
	}
	r := &organizationResource{client: instance.start(t)}

	s := organizationResourceSchema(t)
	prior := organizationValue(t, s, map[string]string{
		"key": "my-org", "name": "My Organization", "description": "Going away",
	})

	// The framework starts the response state as the prior state.
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: prior}}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: organizationValue(t, s, map[string]string{
			"key": "my-org", "name": "My Organization",
		})},
		State: tfsdk.State{Schema: s, Raw: prior},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if got := instance.organizations["my-org"]["description"]; got != "" {
		t.Errorf("stored description = %q, want it cleared", got)
	}
	if !readModel(t, resp.State).Description.IsNull() {
		t.Error("the description stayed in the state although it was cleared")
	}
}

func TestOrganizationResourceDelete(t *testing.T) {
	t.Parallel()

	instance := newFakeInstance()
	instance.organizations["my-org"] = map[string]string{"key": "my-org", "name": "My Organization"}
	r := &organizationResource{client: instance.start(t)}

	s := organizationResourceSchema(t)
	resp := &resource.DeleteResponse{State: emptyState(t, s)}
	r.Delete(context.Background(), resource.DeleteRequest{
		State: tfsdk.State{Schema: s, Raw: organizationValue(t, s, map[string]string{"key": "my-org"})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if _, stillThere := instance.organizations["my-org"]; stillThere {
		t.Error("the organization was not deleted")
	}
}

// A failure of the API reaches the user, rather than a half written state.
func TestOrganizationResourceCreateReportsAFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":[{"msg":"Insufficient privileges"}]}`))
	}))
	defer srv.Close()

	r := &organizationResource{client: client.New(client.Config{
		URL: srv.URL, APIURL: srv.URL, Product: client.ProductCloud, HTTPClient: srv.Client(),
	})}

	s := organizationResourceSchema(t)
	resp := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: organizationValue(t, s, map[string]string{
			"key": "my-org", "name": "My Organization",
		})},
	}, resp)

	assertDiagnosticsContain(t, resp.Diagnostics, "Insufficient privileges")
}

// An organization that vanishes between the write and the read back is
// reported, not recorded as an organization with empty fields.
func TestOrganizationResourceCreateReportsAFailedReadBack(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"not found"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	r := &organizationResource{client: client.New(client.Config{
		URL: srv.URL, APIURL: srv.URL, Product: client.ProductCloud, HTTPClient: srv.Client(),
	})}

	s := organizationResourceSchema(t)
	resp := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: organizationValue(t, s, map[string]string{
			"key": "my-org", "name": "My Organization",
		})},
	}, resp)

	assertDiagnosticsContain(t, resp.Diagnostics, "read the organization my-org back")

	// The organization exists, so it must be in the state. Without it,
	// Terraform knows nothing of it and the next apply fails on a key that is
	// already taken.
	if resp.State.Raw.IsNull() {
		t.Fatal("the created organization is not in the state")
	}
	if got, want := readModel(t, resp.State).Key.ValueString(), "my-org"; got != want {
		t.Errorf("recorded key = %q, want %q", got, want)
	}
}

// A rename that fails must leave the prior key in the state. A state that
// carried the new key would make the next read follow a key that this
// configuration does not own: Terraform would take over another organization,
// or plan a second one beside the first.
func TestOrganizationResourceUpdateKeepsThePriorKeyWhenTheRenameFails(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errors":[{"msg":"An organization with key 'taken-key' already exists"}]}`))
	}))
	defer srv.Close()

	r := &organizationResource{client: client.New(client.Config{
		URL: srv.URL, APIURL: srv.URL, Product: client.ProductCloud, HTTPClient: srv.Client(),
	})}

	s := organizationResourceSchema(t)
	// A prior state always carries the computed id beside the key.
	prior := organizationValue(t, s, map[string]string{
		"id": "my-org", "key": "my-org", "name": "My Organization",
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: prior}}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: organizationValue(t, s, map[string]string{
			"key": "taken-key", "name": "My Organization",
		})},
		State: tfsdk.State{Schema: s, Raw: prior},
	}, resp)

	assertDiagnosticsContain(t, resp.Diagnostics, "already exists")

	if got, want := readModel(t, resp.State).Key.ValueString(), "my-org"; got != want {
		t.Errorf("key in the state = %q, want the prior key %q", got, want)
	}
	if got, want := readModel(t, resp.State).ID.ValueString(), "my-org"; got != want {
		t.Errorf("id in the state = %q, want the prior key %q", got, want)
	}
}

// An import takes the key, which is what a user has.
func TestOrganizationResourceImportState(t *testing.T) {
	t.Parallel()

	s := organizationResourceSchema(t)
	resp := &resource.ImportStateResponse{State: emptyState(t, s)}
	NewOrganizationResource().(*organizationResource).ImportState(
		context.Background(),
		resource.ImportStateRequest{ID: "my-org"},
		resp,
	)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if got, want := readModel(t, resp.State).Key.ValueString(), "my-org"; got != want {
		t.Errorf("imported key = %q, want %q", got, want)
	}
}

func TestKeyPlanModifierDescriptions(t *testing.T) {
	t.Parallel()

	m := keyPlanModifier{}
	if m.Description(context.Background()) == "" {
		t.Error("the plan modifier has no description")
	}
	if m.MarkdownDescription(context.Background()) != m.Description(context.Background()) {
		t.Error("the two descriptions differ")
	}
}

// An update that succeeds and a read back that fails must leave the state
// holding what the server now holds, not what it held before.
func TestOrganizationResourceUpdateRecordsBeforeTheReadBack(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"errors":[{"msg":"the instance is unwell"}]}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	r := &organizationResource{client: client.New(client.Config{
		URL: srv.URL, APIURL: srv.URL, Product: client.ProductCloud, HTTPClient: srv.Client(),
	})}

	s := organizationResourceSchema(t)
	prior := organizationValue(t, s, map[string]string{
		"id": "my-org", "key": "my-org", "name": "Old name",
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: prior}}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: organizationValue(t, s, map[string]string{
			"key": "my-org", "name": "New name",
		})},
		State: tfsdk.State{Schema: s, Raw: prior},
	}, resp)

	assertDiagnosticsContain(t, resp.Diagnostics, "the instance is unwell")

	if got, want := readModel(t, resp.State).Name.ValueString(), "New name"; got != want {
		t.Errorf("name in the state = %q, want %q, which the server now holds", got, want)
	}
}
