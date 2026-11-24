package cloud

import (
	"context"
	"fmt"
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

func organizationAutomaticAnalysisSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewOrganizationAutomaticAnalysisResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func TestOrganizationAutomaticAnalysisResourceSchema(t *testing.T) {
	t.Parallel()
	s := organizationAutomaticAnalysisSchema(t)
	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !s.Attributes["organization"].IsRequired() || !s.Attributes["enabled_for_new_projects"].IsRequired() {
		t.Error("organization and enabled_for_new_projects must be required")
	}
	resp := &resource.MetadataResponse{}
	NewOrganizationAutomaticAnalysisResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "sonarqube"}, resp)
	if resp.TypeName != "sonarqube_cloud_organization_automatic_analysis" {
		t.Errorf("type name = %q", resp.TypeName)
	}
}

func TestOrganizationAutomaticAnalysisResourceConfigureNeedsCloud(t *testing.T) {
	t.Parallel()
	r := &organizationAutomaticAnalysisResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: client.New(client.Config{Product: client.ProductServer})}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "needs SonarQube Cloud")
}

// fakeOrganizationAutomaticAnalysis serves one organization. own is the value
// of the organization, and nil means the default of the instance.
type fakeOrganizationAutomaticAnalysis struct {
	own         *bool
	missingOrg  bool
	refuseSet   bool
	refuseReset bool
	resets      int
}

func (f *fakeOrganizationAutomaticAnalysis) start(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return providertest.NewCloudClient(srv)
}

func (f *fakeOrganizationAutomaticAnalysis) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/organizations/organizations":
		if f.missingOrg {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(`[{"id":"organization-id","uuidV4":"organization-uuid","key":"my-org"}]`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/settings/values":
		if f.own == nil {
			w.Write([]byte(`{"settings":[{"key":"sonar.autoscan.enabled","value":"true","inherited":true}]}`))
			return
		}
		fmt.Fprintf(w, `{"settings":[{"key":"sonar.autoscan.enabled","value":"%t"}]}`, *f.own)
	case r.Method == http.MethodPost && r.URL.Path == "/api/settings/set":
		if f.refuseSet {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"errors":[{"msg":"Insufficient privileges"}]}`))
			return
		}
		value := r.PostForm.Get("value") == "true"
		f.own = &value
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/api/settings/reset":
		f.resets++
		if f.refuseReset {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		f.own = nil
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func organizationAutomaticAnalysisPlan(t *testing.T, s schema.Schema, enabled bool) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
		"organization": "my-org", "enabled_for_new_projects": enabled,
	})}
}

func TestOrganizationAutomaticAnalysisLifecycleAndDrift(t *testing.T) {
	instance := &fakeOrganizationAutomaticAnalysis{}
	r := &organizationAutomaticAnalysisResource{client: instance.start(t)}
	s := organizationAutomaticAnalysisSchema(t)
	ctx := context.Background()

	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(ctx, resource.CreateRequest{Plan: organizationAutomaticAnalysisPlan(t, s, false)}, created)
	if created.Diagnostics.HasError() || instance.own == nil || *instance.own {
		t.Fatalf("create: %v, own %v", created.Diagnostics, instance.own)
	}
	if id := providertest.ReadModel[organizationAutomaticAnalysisModel](t, created.State).ID.ValueString(); id != "my-org" {
		t.Errorf("id = %q", id)
	}

	instance.own = new(true)
	drifted := &resource.ReadResponse{State: created.State}
	r.Read(ctx, resource.ReadRequest{State: created.State}, drifted)
	if drifted.Diagnostics.HasError() ||
		!providertest.ReadModel[organizationAutomaticAnalysisModel](t, drifted.State).EnabledForNewProjects.ValueBool() {
		t.Fatalf("refresh did not report the outside change: %v", drifted.Diagnostics)
	}

	updated := &resource.UpdateResponse{State: drifted.State}
	r.Update(ctx, resource.UpdateRequest{Plan: organizationAutomaticAnalysisPlan(t, s, false), State: drifted.State}, updated)
	if updated.Diagnostics.HasError() || instance.own == nil || *instance.own {
		t.Fatalf("update: %v, own %v", updated.Diagnostics, instance.own)
	}

	deleted := &resource.DeleteResponse{State: updated.State}
	r.Delete(ctx, resource.DeleteRequest{State: updated.State}, deleted)
	if deleted.Diagnostics.HasError() || instance.resets != 1 || instance.own != nil {
		t.Errorf("delete: %v, resets %d, own %v", deleted.Diagnostics, instance.resets, instance.own)
	}
}

func TestOrganizationAutomaticAnalysisCreateErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		fake fakeOrganizationAutomaticAnalysis
		want string
	}{
		{"missing organization", fakeOrganizationAutomaticAnalysis{missingOrg: true}, "Organization my-org not found"},
		{"not Enterprise", fakeOrganizationAutomaticAnalysis{refuseSet: true}, "must be on the Enterprise plan"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &organizationAutomaticAnalysisResource{client: tc.fake.start(t)}
			s := organizationAutomaticAnalysisSchema(t)
			resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
			r.Create(context.Background(), resource.CreateRequest{Plan: organizationAutomaticAnalysisPlan(t, s, false)}, resp)
			providertest.AssertDiagnosticsContain(t, resp.Diagnostics, tc.want)
			if !resp.State.Raw.IsNull() {
				t.Error("a failed create wrote the state")
			}
		})
	}
}

func TestOrganizationAutomaticAnalysisReadMissingOrganization(t *testing.T) {
	instance := &fakeOrganizationAutomaticAnalysis{missingOrg: true}
	r := &organizationAutomaticAnalysisResource{client: instance.start(t)}
	s := organizationAutomaticAnalysisSchema(t)
	state := tfsdk.State{Schema: s, Raw: bindingValue(t, s, map[string]any{
		"id": "my-org", "organization": "my-org", "enabled_for_new_projects": false,
	})}
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() || len(resp.Diagnostics.Warnings()) != 1 {
		t.Errorf("a missing organization did not remove the state with a warning: %v", resp.Diagnostics)
	}
}

func TestOrganizationAutomaticAnalysisImport(t *testing.T) {
	t.Parallel()
	s := organizationAutomaticAnalysisSchema(t)
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	NewOrganizationAutomaticAnalysisResource().(*organizationAutomaticAnalysisResource).ImportState(
		context.Background(), resource.ImportStateRequest{ID: "my-org"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := providertest.ReadModel[organizationAutomaticAnalysisModel](t, resp.State).Organization.ValueString(); got != "my-org" {
		t.Errorf("organization = %q", got)
	}
}

// Without its own value, the organization follows the default. A refresh and
// an import store null, so that the next plan sets the value.
func TestOrganizationAutomaticAnalysisReadInheritedValue(t *testing.T) {
	instance := &fakeOrganizationAutomaticAnalysis{}
	r := &organizationAutomaticAnalysisResource{client: instance.start(t)}
	s := organizationAutomaticAnalysisSchema(t)

	for name, attributes := range map[string]map[string]any{
		"refresh": {"id": "my-org", "organization": "my-org", "enabled_for_new_projects": true},
		"import":  {"organization": "my-org"},
	} {
		stored := tfsdk.State{Schema: s, Raw: bindingValue(t, s, attributes)}
		resp := &resource.ReadResponse{State: stored}
		r.Read(context.Background(), resource.ReadRequest{State: stored}, resp)
		if resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
			t.Fatalf("%s: %v", name, resp.Diagnostics)
		}
		state := providertest.ReadModel[organizationAutomaticAnalysisModel](t, resp.State)
		if !state.EnabledForNewProjects.IsNull() || state.ID.ValueString() != "my-org" {
			t.Errorf("%s: state = %+v, want a null value", name, state)
		}
	}
}

func TestOrganizationAutomaticAnalysisDeleteRefused(t *testing.T) {
	instance := &fakeOrganizationAutomaticAnalysis{own: new(false), refuseReset: true}
	r := &organizationAutomaticAnalysisResource{client: instance.start(t)}
	s := organizationAutomaticAnalysisSchema(t)
	state := tfsdk.State{Schema: s, Raw: bindingValue(t, s, map[string]any{
		"id": "my-org", "organization": "my-org", "enabled_for_new_projects": false,
	})}
	resp := &resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "Administer permission on the organization") ||
		strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "Enterprise plan") {
		t.Errorf("diagnostics = %v", resp.Diagnostics)
	}
}
