package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// These tests drive the resource against a small instance held in memory, so
// they cover the whole lifecycle without a Terraform binary and without
// credentials.

const theOrganizationID = "organization-id"

func TestCloudOrganizationBindingResourceCreate(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.organizations["my-org"] = theOrganizationID
	r := &cloudOrganizationBindingResource{client: instance.start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	resp := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"organization_key": "my-org",
			"dev_ops_platform": client.PlatformGitHub,
			"installation_id":  "12345678",
		})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	binding, made := instance.bindings[theBindingID]
	if !made {
		t.Fatal("the binding was not created")
	}
	// The API names an organization by its internal identifier, never by the
	// key that the configuration carries.
	if got, want := binding.OrganizationID, theOrganizationID; got != want {
		t.Errorf("the bind call carried organizationId %q, want %q", got, want)
	}

	state := readModel[cloudOrganizationBindingModel](t, resp.State)
	if got, want := state.ID.ValueString(), theBindingID; got != want {
		t.Errorf("id = %q, want %q", got, want)
	}
	if got, want := state.OrganizationKey.ValueString(), "my-org"; got != want {
		t.Errorf("organization_key = %q, want %q", got, want)
	}
	if got, want := state.OrganizationID.ValueString(), theOrganizationID; got != want {
		t.Errorf("organization_id = %q, want %q", got, want)
	}
	if got, want := state.BindingType.ValueString(), "integration-dop"; got != want {
		t.Errorf("binding_type = %q, want %q", got, want)
	}
}

func TestCloudOrganizationBindingResourceCreateWithAnUnknownOrganization(t *testing.T) {
	t.Parallel()

	r := &cloudOrganizationBindingResource{client: newFakeBoundInstance().start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	resp := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"organization_key": "gone",
			"dev_ops_platform": client.PlatformGitHub,
			"installation_id":  "12345678",
		})},
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a key that names no organization was accepted")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(got, "not found") {
		t.Errorf("summary = %q, want it to report that the organization was not found", got)
	}
}

// The server writes false for an automatic import that it does not allow.
//
// A create must not end with an error here. An error on a create marks the
// object tainted, the next plan replaces a tainted object, and a replacement
// cannot work: the delete removes nothing, the organization stays bound, and
// the new bind is refused. The binding would leave the state for good.
func TestCloudOrganizationBindingResourceCreateWithAnAutoImportTheServerRefuses(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.organizations["my-org"] = theOrganizationID
	instance.autoImportAllowed = false
	r := &cloudOrganizationBindingResource{client: instance.start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	resp := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"organization_key":         "my-org",
			"dev_ops_platform":         client.PlatformGitHub,
			"installation_id":          "12345678",
			"repo_auto_import_enabled": true,
		})},
	}, resp)

	// An error here would taint the binding.
	if resp.Diagnostics.HasError() {
		t.Fatalf("the create failed, which taints a binding that cannot be replaced: %v",
			resp.Diagnostics.Errors())
	}
	if resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("got %d warnings, want the one that names the feature",
			resp.Diagnostics.WarningsCount())
	}
	if got := resp.Diagnostics.Warnings()[0].Summary(); !strings.Contains(got, "automatic import") {
		t.Errorf("summary = %q, want it to name the automatic import", got)
	}

	// The binding exists, so the state must hold it.
	if resp.State.Raw.IsNull() {
		t.Fatal("the state holds no binding although the binding was written")
	}

	// Terraform compares the state with the plan and reports a difference as
	// an inconsistent result, which is an error, which taints. Every other
	// attribute is computed and therefore unknown in a real plan, which
	// answers any value, so the configured attribute is the one that must
	// match.
	if !readModel[cloudOrganizationBindingModel](t, resp.State).RepoAutoImportEnabled.ValueBool() {
		t.Error("repo_auto_import_enabled = false in the state while the plan asks for true, " +
			"which Terraform reports as an inconsistent result and which taints the binding")
	}
}

// An update reports the same refusal as an error. The binding is already in
// the state, and an error from an update taints nothing.
func TestCloudOrganizationBindingResourceUpdateWithAnAutoImportTheServerRefuses(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.bind("my-org", theOrganizationID)
	instance.autoImportAllowed = false
	r := &cloudOrganizationBindingResource{client: instance.start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	resp := &resource.UpdateResponse{State: emptyState(t, s)}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"organization_key":         "my-org",
			"dev_ops_platform":         client.PlatformGitHub,
			"installation_id":          "12345678",
			"repo_auto_import_enabled": true,
		})},
		State: tfsdk.State{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"id":               theBindingID,
			"organization_key": "my-org",
			"dev_ops_platform": client.PlatformGitHub,
			"installation_id":  "12345678",
		})},
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("an automatic import that the server refused went unreported")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(got, "automatic import") {
		t.Errorf("summary = %q, want it to name the automatic import", got)
	}
	// The state holds what the server holds, so the next plan is honest.
	if readModel[cloudOrganizationBindingModel](t, resp.State).RepoAutoImportEnabled.ValueBool() {
		t.Error("repo_auto_import_enabled = true in the state, want the false that the server holds")
	}
}

func TestCloudOrganizationBindingResourceRead(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.bind("my-org", theOrganizationID)
	r := &cloudOrganizationBindingResource{client: instance.start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	resp := &resource.ReadResponse{State: emptyState(t, s)}
	r.Read(context.Background(), resource.ReadRequest{
		State: tfsdk.State{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"id": theBindingID, "organization_key": "my-org",
		})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	state := readModel[cloudOrganizationBindingModel](t, resp.State)
	if got, want := state.InstallationID.ValueString(), "12345678"; got != want {
		t.Errorf("installation_id = %q, want %q", got, want)
	}
	// The API never reports the key, so the read must keep the one that the
	// prior state carried.
	if got, want := state.OrganizationKey.ValueString(), "my-org"; got != want {
		t.Errorf("organization_key = %q, want %q", got, want)
	}
}

// A binding that is gone leaves the state, so that the next plan makes it
// again.
func TestCloudOrganizationBindingResourceReadAfterAnOutsideDeletion(t *testing.T) {
	t.Parallel()

	r := &cloudOrganizationBindingResource{client: newFakeBoundInstance().start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	resp := &resource.ReadResponse{State: emptyState(t, s)}
	r.Read(context.Background(), resource.ReadRequest{
		State: tfsdk.State{Schema: s, Raw: bindingValue(t, s, map[string]any{"id": "gone"})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("the binding stayed in the state although it is gone")
	}
}

func TestCloudOrganizationBindingResourceUpdate(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.bind("my-org", theOrganizationID)
	r := &cloudOrganizationBindingResource{client: instance.start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	resp := &resource.UpdateResponse{State: emptyState(t, s)}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"organization_key":         "my-org",
			"dev_ops_platform":         client.PlatformGitHub,
			"installation_id":          "12345678",
			"repo_auto_import_enabled": true,
		})},
		State: tfsdk.State{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"id":                       theBindingID,
			"organization_key":         "my-org",
			"dev_ops_platform":         client.PlatformGitHub,
			"installation_id":          "12345678",
			"repo_auto_import_enabled": false,
		})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	if got := instance.bindings[theBindingID].RepoAutoImportEnabled; got == nil || !*got {
		t.Error("the automatic import was not turned on")
	}
	if !readModel[cloudOrganizationBindingModel](t, resp.State).RepoAutoImportEnabled.ValueBool() {
		t.Error("repo_auto_import_enabled = false in the state, want true")
	}
}

// A destroy writes nothing, because the API has no operation to remove a
// binding. It must say so instead of reporting a clean removal.
func TestCloudOrganizationBindingResourceDeleteOnlyWarns(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.bind("my-org", theOrganizationID)
	r := &cloudOrganizationBindingResource{client: instance.start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	resp := &resource.DeleteResponse{State: emptyState(t, s)}
	r.Delete(context.Background(), resource.DeleteRequest{
		State: tfsdk.State{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"id": theBindingID, "organization_key": "my-org",
		})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("got %d warnings, want the one that says the binding stays",
			resp.Diagnostics.WarningsCount())
	}
	if len(instance.calls) != 0 {
		t.Errorf("the destroy called the API: %v", instance.calls)
	}
	if _, stillThere := instance.bindings[theBindingID]; !stillThere {
		t.Error("the binding went away, although the API cannot remove one")
	}
}

// An import takes the key of the organization, because nobody knows the
// identifier of the binding beforehand.
func TestCloudOrganizationBindingResourceImportTakesTheOrganizationKey(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.bind("my-org", theOrganizationID)
	r := &cloudOrganizationBindingResource{client: instance.start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	resp := &resource.ImportStateResponse{State: emptyState(t, s)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "my-org"}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	state := readModel[cloudOrganizationBindingModel](t, resp.State)
	if got, want := state.ID.ValueString(), theBindingID; got != want {
		t.Errorf("id = %q, want %q", got, want)
	}
	if got, want := state.OrganizationKey.ValueString(), "my-org"; got != want {
		t.Errorf("organization_key = %q, want %q", got, want)
	}
}

func TestCloudOrganizationBindingResourceImportOfAnUnboundOrganization(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.organizations["my-org"] = theOrganizationID
	r := &cloudOrganizationBindingResource{client: instance.start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	resp := &resource.ImportStateResponse{State: emptyState(t, s)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "my-org"}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("an organization with no binding was imported")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(got, "no binding") {
		t.Errorf("summary = %q, want it to report that there is no binding", got)
	}
	// The import has one more sentence to say, and a space must join it.
	if got := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(got, "platform. There is nothing to import.") {
		t.Errorf("detail = %q, want the extra sentence after one space", got)
	}
}

// The resource binds to github.com only, so it must refuse the import of a
// binding to another platform rather than write a state that no plan can
// answer: such a binding carries no installation, and the attribute is
// required here. A real instance holds a binding to GitLab of exactly this shape.
func TestCloudOrganizationBindingResourceImportOfAnotherPlatform(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.bindTo("gitlab-org", theOrganizationID, "gitlab")
	r := &cloudOrganizationBindingResource{client: instance.start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	resp := &resource.ImportStateResponse{State: emptyState(t, s)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "gitlab-org"}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a binding to GitLab was imported into a resource that binds to GitHub")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(got, "gitlab") {
		t.Errorf("summary = %q, want it to name the platform", got)
	}
}

// A create reads the binding back, so that a value the server keeps without
// saying so reaches the state. repo_auto_import_enabled is the only field
// where that is known to happen, and the read covers every field.
func TestCloudOrganizationBindingResourceCreateReadsBack(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.organizations["my-org"] = theOrganizationID
	instance.storedPlatformURL = "https://github.com/the-address-the-server-keeps"
	r := &cloudOrganizationBindingResource{client: instance.start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	resp := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"organization_key": "my-org",
			"dev_ops_platform": client.PlatformGitHub,
			"installation_id":  "12345678",
		})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	state := readModel[cloudOrganizationBindingModel](t, resp.State)
	if got, want := state.DevOpsPlatformURL.ValueString(), instance.storedPlatformURL; got != want {
		t.Errorf("dev_ops_platform_url = %q, want %q, which the read back reports", got, want)
	}
}

// A read back that fails after a create gives a warning, never an error. An
// error marks the object tainted, the next plan replaces it, the delete
// removes nothing, and the new bind is refused, so the binding would leave
// the state for good.
func TestCloudOrganizationBindingResourceCreateWarnsWhenTheReadBackFails(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.organizations["my-org"] = theOrganizationID
	instance.failBindingReads = true
	r := &cloudOrganizationBindingResource{client: instance.start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	resp := &resource.CreateResponse{State: emptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"organization_key": "my-org",
			"dev_ops_platform": client.PlatformGitHub,
			"installation_id":  "12345678",
		})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("the create reported an error, which taints the binding: %v", resp.Diagnostics.Errors())
	}
	if resp.Diagnostics.WarningsCount() == 0 {
		t.Error("the create reported no warning about the read back")
	}

	if resp.State.Raw.IsNull() {
		t.Fatal("the binding is not in the state, so nothing can manage it")
	}
	if got, want := readModel[cloudOrganizationBindingModel](t, resp.State).ID.ValueString(), theBindingID; got != want {
		t.Errorf("id in the state = %q, want %q from the answer of the write", got, want)
	}
}

// An update reports a read back that fails as an error. An error from an
// update taints nothing, and the state already names the binding.
func TestCloudOrganizationBindingResourceUpdateFailsWhenTheReadBackFails(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.bind("my-org", theOrganizationID)
	instance.failBindingReads = true
	r := &cloudOrganizationBindingResource{client: instance.start(t)}

	s := cloudOrganizationBindingResourceSchema(t)
	prior := bindingValue(t, s, map[string]any{
		"id": theBindingID, "organization_key": "my-org",
		"dev_ops_platform": client.PlatformGitHub, "installation_id": "12345678",
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: prior}}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"id": theBindingID, "organization_key": "my-org",
			"dev_ops_platform": client.PlatformGitHub, "installation_id": "12345678",
			"repo_auto_import_enabled": true,
		})},
		State: tfsdk.State{Schema: s, Raw: prior},
	}, resp)

	assertDiagnosticsContain(t, resp.Diagnostics, "read the organization binding")
}
