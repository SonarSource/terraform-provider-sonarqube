package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func organizationQualityGateSettingsSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewOrganizationQualityGateSettingsResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func TestOrganizationQualityGateSettingsResourceSchema(t *testing.T) {
	t.Parallel()
	s := organizationQualityGateSettingsSchema(t)
	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !s.Attributes["organization"].IsRequired() || !s.Attributes["ignore_small_changes"].IsRequired() {
		t.Error("organization and ignore_small_changes must be required")
	}
}

func TestOrganizationQualityGateSettingsResourceConfigureNeedsCloud(t *testing.T) {
	t.Parallel()
	r := &organizationQualityGateSettingsResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: client.New(client.Config{Product: client.ProductServer})}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "needs SonarQube Cloud")
}

type fakeQualityGateSettings struct {
	value        bool
	patches      int
	failSettings bool
	failPatch    bool
	missingOrg   bool
}

func (f *fakeQualityGateSettings) start(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return providertest.NewCloudClient(srv)
}

func (f *fakeQualityGateSettings) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/organizations/organizations":
		if f.missingOrg {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(`[{"id":"organization-id","uuidV4":"organization-uuid","key":"my-org"}]`))
	case r.Method == http.MethodGet && r.URL.Path == "/quality-gates/settings":
		if f.failSettings {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.URL.Query().Get("resourceId") != "organization-uuid" || r.URL.Query().Get("resourceType") != "ORGANIZATION" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "settings-id", "ignoreSmallChanges": f.value})
	case r.Method == http.MethodPatch && r.URL.Path == "/quality-gates/settings/settings-id":
		f.patches++
		if f.failPatch {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var body struct {
			IgnoreSmallChanges bool `json:"ignoreSmallChanges"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.value = body.IgnoreSmallChanges
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestOrganizationQualityGateSettingsLifecycleAndDrift(t *testing.T) {
	instance := &fakeQualityGateSettings{}
	r := &organizationQualityGateSettingsResource{client: instance.start(t)}
	s := organizationQualityGateSettingsSchema(t)
	ctx := context.Background()

	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
		"organization": "my-org", "ignore_small_changes": true,
	})}}, created)
	if created.Diagnostics.HasError() {
		t.Fatalf("create: %v", created.Diagnostics)
	}
	if !instance.value || instance.patches != 1 {
		t.Fatalf("create wrote value %t in %d patches", instance.value, instance.patches)
	}
	state := providertest.ReadModel[organizationQualityGateSettingsModel](t, created.State)
	if state.ID.ValueString() != "my-org" || !state.IgnoreSmallChanges.ValueBool() {
		t.Errorf("created state = %+v", state)
	}

	refreshed := &resource.ReadResponse{State: created.State}
	r.Read(ctx, resource.ReadRequest{State: created.State}, refreshed)
	if refreshed.Diagnostics.HasError() || instance.patches != 1 {
		t.Fatalf("stable refresh: %v, patches %d", refreshed.Diagnostics, instance.patches)
	}
	if !providertest.ReadModel[organizationQualityGateSettingsModel](t, refreshed.State).IgnoreSmallChanges.ValueBool() {
		t.Error("stable refresh changed the value")
	}

	instance.value = false
	drifted := &resource.ReadResponse{State: refreshed.State}
	r.Read(ctx, resource.ReadRequest{State: refreshed.State}, drifted)
	if drifted.Diagnostics.HasError() {
		t.Fatalf("drift refresh: %v", drifted.Diagnostics)
	}
	if providertest.ReadModel[organizationQualityGateSettingsModel](t, drifted.State).IgnoreSmallChanges.ValueBool() {
		t.Error("refresh did not report the outside change")
	}

	updated := &resource.UpdateResponse{State: drifted.State}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
		"id": "my-org", "organization": "my-org", "ignore_small_changes": true,
	})}, State: drifted.State}, updated)
	if updated.Diagnostics.HasError() || !instance.value || instance.patches != 2 {
		t.Fatalf("update: %v, value %t, patches %d", updated.Diagnostics, instance.value, instance.patches)
	}

	deleted := &resource.DeleteResponse{State: updated.State}
	r.Delete(ctx, resource.DeleteRequest{State: updated.State}, deleted)
	if deleted.Diagnostics.HasError() || !instance.value || instance.patches != 2 {
		t.Fatalf("delete changed the server: %v", deleted.Diagnostics)
	}
}

// A patch of the organization resets the project overrides, so an apply that
// does not change the value must not send one.
func TestOrganizationQualityGateSettingsCreateKeepsUnchangedValue(t *testing.T) {
	instance := &fakeQualityGateSettings{value: true}
	r := &organizationQualityGateSettingsResource{client: instance.start(t)}
	s := organizationQualityGateSettingsSchema(t)

	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
		"organization": "my-org", "ignore_small_changes": true,
	})}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	if instance.patches != 0 {
		t.Errorf("create sent %d patches for an unchanged value", instance.patches)
	}
	if !providertest.ReadModel[organizationQualityGateSettingsModel](t, resp.State).IgnoreSmallChanges.ValueBool() {
		t.Error("create did not store the value")
	}
}

func TestOrganizationQualityGateSettingsResourceErrors(t *testing.T) {
	for _, tc := range []struct {
		name        string
		fake        fakeQualityGateSettings
		want        string
		wantPatches int
	}{
		{"missing organization", fakeQualityGateSettings{missingOrg: true}, "not found", 0},
		{"failed settings read", fakeQualityGateSettings{failSettings: true}, "Cannot read quality gate settings", 0},
		{"refused patch", fakeQualityGateSettings{failPatch: true}, "Cannot update quality gate settings", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &organizationQualityGateSettingsResource{client: tc.fake.start(t)}
			s := organizationQualityGateSettingsSchema(t)
			resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
				"organization": "my-org", "ignore_small_changes": true,
			})}}, resp)
			providertest.AssertDiagnosticsContain(t, resp.Diagnostics, tc.want)
			if tc.fake.patches != tc.wantPatches {
				t.Errorf("create sent %d patches, want %d", tc.fake.patches, tc.wantPatches)
			}
			if tc.fake.value {
				t.Error("a failed create changed the setting")
			}
		})
	}
}

func TestOrganizationQualityGateSettingsImport(t *testing.T) {
	t.Parallel()
	r := &organizationQualityGateSettingsResource{}
	s := organizationQualityGateSettingsSchema(t)
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "my-org"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	state := providertest.ReadModel[organizationQualityGateSettingsModel](t, resp.State)
	if state.Organization.ValueString() != "my-org" || !state.ID.IsNull() {
		t.Errorf("import state = %+v", state)
	}
}

func TestOrganizationQualityGateSettingsReadMissingOrganization(t *testing.T) {
	instance := &fakeQualityGateSettings{missingOrg: true}
	r := &organizationQualityGateSettingsResource{client: instance.start(t)}
	s := organizationQualityGateSettingsSchema(t)
	state := tfsdk.State{Schema: s, Raw: bindingValue(t, s, map[string]any{
		"id": "my-org", "organization": "my-org", "ignore_small_changes": true,
	})}
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Errorf("missing organization did not remove state: %v", resp.Diagnostics)
	}
	if len(resp.Diagnostics.Warnings()) == 0 || !strings.Contains(resp.Diagnostics.Warnings()[0].Detail(), "token cannot read") {
		t.Error("the warning did not explain access loss")
	}
}
