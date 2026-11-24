package cloud

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func projectAutomaticAnalysisSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewProjectAutomaticAnalysisResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func TestProjectAutomaticAnalysisResourceSchema(t *testing.T) {
	t.Parallel()
	s := projectAutomaticAnalysisSchema(t)
	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"organization", "project_key", "enabled"} {
		if !s.Attributes[name].IsRequired() {
			t.Errorf("%s must be required", name)
		}
	}
	resp := &resource.MetadataResponse{}
	NewProjectAutomaticAnalysisResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "sonarqube"}, resp)
	if resp.TypeName != "sonarqube_cloud_project_automatic_analysis" {
		t.Errorf("type name = %q", resp.TypeName)
	}
}

func TestProjectAutomaticAnalysisResourceConfigureNeedsCloud(t *testing.T) {
	t.Parallel()
	r := &projectAutomaticAnalysisResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: client.New(client.Config{Product: client.ProductServer})}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "needs SonarQube Cloud")
}

// fakeAutomaticAnalysis serves one organization with one project.
type fakeAutomaticAnalysis struct {
	enabled        bool
	orgReads       int
	writes         int
	missingProject bool
	missingSetting bool
	refuseWrite    int
}

func (f *fakeAutomaticAnalysis) start(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return providertest.NewCloudClient(srv)
}

func (f *fakeAutomaticAnalysis) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/organizations/organizations":
		f.orgReads++
		w.Write([]byte(`[{"id":"organization-id","uuidV4":"organization-uuid","key":"my-org"}]`))
	case r.Method == http.MethodGet && r.URL.Path == "/projects/projects":
		if f.missingProject {
			w.Write([]byte(`{"projects":[]}`))
			return
		}
		w.Write([]byte(`{"projects":[{"id":"project-id","key":"my-project","organizationId":"organization-uuid"}]}`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/settings/values":
		if f.missingSetting {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `{"settings":[{"key":"sonar.autoscan.enabled","value":"%t"}]}`, f.enabled)
	case r.Method == http.MethodPost && r.URL.Path == "/api/autoscan/activation":
		f.writes++
		if f.refuseWrite != 0 {
			w.WriteHeader(f.refuseWrite)
			w.Write([]byte(`{"errors":[{"msg":"refused"}]}`))
			return
		}
		_ = r.ParseForm()
		f.enabled, _ = strconv.ParseBool(r.PostForm.Get("enable"))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func automaticAnalysisPlan(t *testing.T, s schema.Schema, enabled bool) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
		"organization": "my-org", "project_key": "my-project", "enabled": enabled,
	})}
}

func TestProjectAutomaticAnalysisLifecycleAndDrift(t *testing.T) {
	instance := &fakeAutomaticAnalysis{enabled: true}
	r := &projectAutomaticAnalysisResource{client: instance.start(t)}
	s := projectAutomaticAnalysisSchema(t)
	ctx := context.Background()

	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(ctx, resource.CreateRequest{Plan: automaticAnalysisPlan(t, s, false)}, created)
	if created.Diagnostics.HasError() || instance.enabled || instance.writes != 1 {
		t.Fatalf("create: %v, enabled %t, writes %d", created.Diagnostics, instance.enabled, instance.writes)
	}
	if id := providertest.ReadModel[projectAutomaticAnalysisModel](t, created.State).ID.ValueString(); id != "my-org/my-project" {
		t.Errorf("id = %q", id)
	}
	if instance.orgReads != 1 {
		t.Errorf("create read the organization %d times, want 1", instance.orgReads)
	}

	instance.enabled = true
	drifted := &resource.ReadResponse{State: created.State}
	r.Read(ctx, resource.ReadRequest{State: created.State}, drifted)
	if drifted.Diagnostics.HasError() || !providertest.ReadModel[projectAutomaticAnalysisModel](t, drifted.State).Enabled.ValueBool() {
		t.Fatalf("refresh did not report the outside change: %v", drifted.Diagnostics)
	}

	updated := &resource.UpdateResponse{State: drifted.State}
	r.Update(ctx, resource.UpdateRequest{Plan: automaticAnalysisPlan(t, s, false), State: drifted.State}, updated)
	if updated.Diagnostics.HasError() || instance.enabled || instance.writes != 2 {
		t.Fatalf("update: %v, enabled %t, writes %d", updated.Diagnostics, instance.enabled, instance.writes)
	}

	deleted := &resource.DeleteResponse{State: updated.State}
	r.Delete(ctx, resource.DeleteRequest{State: updated.State}, deleted)
	if deleted.Diagnostics.HasError() || len(deleted.Diagnostics.Warnings()) != 1 || instance.writes != 2 {
		t.Errorf("delete: %v, writes %d", deleted.Diagnostics, instance.writes)
	}
}

// A change to on starts a check of the repository. Thus, an apply that keeps
// the value on must not write. Off is always written (see the next test).
func TestProjectAutomaticAnalysisCreateKeepsUnchangedOn(t *testing.T) {
	instance := &fakeAutomaticAnalysis{enabled: true}
	r := &projectAutomaticAnalysisResource{client: instance.start(t)}
	s := projectAutomaticAnalysisSchema(t)

	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: automaticAnalysisPlan(t, s, true)}, resp)
	if resp.Diagnostics.HasError() || instance.writes != 0 {
		t.Errorf("create: %v, writes %d", resp.Diagnostics, instance.writes)
	}
}

// A project without a stored value reads as off, but the server can still turn
// Automatic Analysis on. So off is always written.
func TestProjectAutomaticAnalysisCreateAlwaysWritesOff(t *testing.T) {
	instance := &fakeAutomaticAnalysis{enabled: false}
	r := &projectAutomaticAnalysisResource{client: instance.start(t)}
	s := projectAutomaticAnalysisSchema(t)

	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: automaticAnalysisPlan(t, s, false)}, resp)
	if resp.Diagnostics.HasError() || instance.writes != 1 || instance.enabled {
		t.Errorf("create: %v, writes %d, enabled %t", resp.Diagnostics, instance.writes, instance.enabled)
	}
}

func TestProjectAutomaticAnalysisCreateErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fake    fakeAutomaticAnalysis
		enabled bool
		want    string
	}{
		{"missing project", fakeAutomaticAnalysis{missingProject: true}, true, "Project my-project not found"},
		{"missing setting", fakeAutomaticAnalysis{missingSetting: true}, true, "Project my-project not found"},
		{"refused enable", fakeAutomaticAnalysis{refuseWrite: http.StatusBadRequest}, true, "the only project bound to its repository"},
		{"refused disable", fakeAutomaticAnalysis{enabled: true, refuseWrite: http.StatusNotFound}, false, "Administer permission"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &projectAutomaticAnalysisResource{client: tc.fake.start(t)}
			s := projectAutomaticAnalysisSchema(t)
			resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
			r.Create(context.Background(), resource.CreateRequest{Plan: automaticAnalysisPlan(t, s, tc.enabled)}, resp)
			providertest.AssertDiagnosticsContain(t, resp.Diagnostics, tc.want)
			if !resp.State.Raw.IsNull() {
				t.Error("a failed create wrote the state")
			}
		})
	}
}

func TestProjectAutomaticAnalysisDisableErrorHasNoBindingHint(t *testing.T) {
	instance := &fakeAutomaticAnalysis{enabled: true, refuseWrite: http.StatusBadRequest}
	r := &projectAutomaticAnalysisResource{client: instance.start(t)}
	s := projectAutomaticAnalysisSchema(t)
	resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: automaticAnalysisPlan(t, s, false)}, resp)
	if !resp.Diagnostics.HasError() || strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "bound") {
		t.Errorf("diagnostics = %v", resp.Diagnostics)
	}
}

func TestProjectAutomaticAnalysisReadMissingProject(t *testing.T) {
	for _, fake := range []fakeAutomaticAnalysis{{missingProject: true}, {missingSetting: true}} {
		r := &projectAutomaticAnalysisResource{client: fake.start(t)}
		s := projectAutomaticAnalysisSchema(t)
		state := tfsdk.State{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"id": "my-org/my-project", "organization": "my-org", "project_key": "my-project", "enabled": false,
		})}
		resp := &resource.ReadResponse{State: state}
		r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
		if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
			t.Errorf("%+v: a missing project did not remove the state: %v", fake, resp.Diagnostics)
		}
		if len(resp.Diagnostics.Warnings()) != 1 || !strings.Contains(resp.Diagnostics.Warnings()[0].Detail(), "token") {
			t.Errorf("%+v: the warning did not explain access loss: %v", fake, resp.Diagnostics)
		}
	}
}

func TestProjectAutomaticAnalysisImport(t *testing.T) {
	t.Parallel()
	s := projectAutomaticAnalysisSchema(t)

	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	NewProjectAutomaticAnalysisResource().(*projectAutomaticAnalysisResource).ImportState(
		context.Background(), resource.ImportStateRequest{ID: "my-org/my-project"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	state := providertest.ReadModel[projectAutomaticAnalysisModel](t, resp.State)
	if state.Organization.ValueString() != "my-org" || state.ProjectKey.ValueString() != "my-project" {
		t.Errorf("import state = %+v", state)
	}

	for _, id := range []string{"my-project", "/my-project", "my-org/"} {
		resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
		NewProjectAutomaticAnalysisResource().(*projectAutomaticAnalysisResource).ImportState(
			context.Background(), resource.ImportStateRequest{ID: id}, resp)
		providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Cannot import the Automatic Analysis setting")
	}
}
