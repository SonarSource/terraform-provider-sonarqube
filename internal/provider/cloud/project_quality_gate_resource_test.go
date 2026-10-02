package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

const fakeProjectUUID = "project-uuid"

// fakeProjectGateInstance holds the organization my-org, the project my-project
// and the gates gate-a and gate-b.
type fakeProjectGateInstance struct {
	organizationExists bool
	projectExists      bool
	// gate is the gate of the association. It is empty when the project uses
	// the default gate.
	gate string
	// fallback makes the read report an association with defaultFallback set.
	fallback bool
	// createStatus makes the create call fail with this status when it is not 0.
	createStatus int
	calls        []string
}

func newFakeProjectGateInstance() *fakeProjectGateInstance {
	return &fakeProjectGateInstance{organizationExists: true, projectExists: true}
}

func (f *fakeProjectGateInstance) start(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /organizations/organizations":
			if !f.organizationExists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`[{"id":"organization-id","uuidV4":"` + providertest.FakeOrganizationUUID + `","key":"my-org"}]`))
		case "GET /projects/projects":
			if !f.projectExists {
				_, _ = w.Write([]byte(`{"projects":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"projects":[{"id":"project-uuid","legacyId":"` + fakeProjectUUID + `","key":"my-project"}]}`))
		case "GET /quality-gates/project-associations":
			f.read(w)
		case "POST /quality-gates/project-associations":
			f.create(w, r)
		case "DELETE /quality-gates/project-associations/assoc-id":
			f.gate = ""
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return providertest.NewCloudClient(srv)
}

func (f *fakeProjectGateInstance) read(w http.ResponseWriter) {
	switch {
	case f.fallback:
		_, _ = w.Write([]byte(`{"projectAssociations":[{"id":"assoc-id","projectId":"` + fakeProjectUUID +
			`","qualityGateId":"default-gate","defaultFallback":true}]}`))
	case f.gate == "":
		_, _ = w.Write([]byte(`{"projectAssociations":[]}`))
	default:
		_, _ = w.Write([]byte(`{"projectAssociations":[{"id":"assoc-id","projectId":"` + fakeProjectUUID +
			`","qualityGateId":"` + f.gate + `","defaultFallback":false}]}`))
	}
}

func (f *fakeProjectGateInstance) create(w http.ResponseWriter, r *http.Request) {
	if f.createStatus != 0 {
		w.WriteHeader(f.createStatus)
		_, _ = w.Write([]byte(`{"message":"refused"}`))
		return
	}
	var body struct{ ProjectID, QualityGateID string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ProjectID != fakeProjectUUID {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if body.QualityGateID != "gate-a" && body.QualityGateID != "gate-b" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	f.gate = body.QualityGateID
	_, _ = w.Write([]byte(`{"id":"assoc-id","projectId":"` + fakeProjectUUID + `","qualityGateId":"` + f.gate + `"}`))
}

func projectGateResourceSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewProjectQualityGateResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func projectGatePlan(t *testing.T, s schema.Schema, gate string) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"organization": "my-org", "project_key": "my-project", "quality_gate_id": gate,
	})}
}

func projectGateState(t *testing.T, s schema.Schema, gate string) tfsdk.State {
	t.Helper()
	return tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "my-org/my-project", "organization": "my-org", "project_key": "my-project",
		"quality_gate_id": gate, "association_id": "assoc-id",
	})}
}

func TestProjectQualityGateResourceSchema(t *testing.T) {
	t.Parallel()

	s := projectGateResourceSchema(t)
	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"organization", "project_key", "quality_gate_id"} {
		if !s.Attributes[name].IsRequired() {
			t.Errorf("%s must be required", name)
		}
	}
	replace := stringplanmodifier.RequiresReplace().Description(context.Background())
	for _, name := range []string{"organization", "project_key"} {
		attribute, ok := s.Attributes[name].(schema.StringAttribute)
		if !ok || len(attribute.PlanModifiers) != 1 || attribute.PlanModifiers[0].Description(context.Background()) != replace {
			t.Errorf("%s must require a replacement", name)
		}
	}
	// A different gate changes the assignment in place.
	if gate, ok := s.Attributes["quality_gate_id"].(schema.StringAttribute); !ok || len(gate.PlanModifiers) != 0 {
		t.Error("quality_gate_id must not require a replacement")
	}
	resp := &resource.MetadataResponse{}
	NewProjectQualityGateResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "sonarqube"}, resp)
	if resp.TypeName != "sonarqube_cloud_project_quality_gate" {
		t.Errorf("type name = %q", resp.TypeName)
	}
}

func TestProjectQualityGateResourceConfigureNeedsCloud(t *testing.T) {
	t.Parallel()

	r := &projectQualityGateResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: client.New(client.Config{
		URL: "https://dev.example.io", Product: client.ProductServer,
	})}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "sonarqube_cloud_project_quality_gate resource")
	if r.client != nil {
		t.Error("the resource retained a Server client")
	}
}

func TestProjectQualityGateResourceLifecycle(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectGateInstance()
	r := &projectQualityGateResource{client: instance.start(t)}
	s := projectGateResourceSchema(t)

	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: projectGatePlan(t, s, "gate-a")}, created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	model := providertest.ReadModel[projectQualityGateModel](t, created.State)
	if model.ID.ValueString() != "my-org/my-project" || model.AssociationID.ValueString() != "assoc-id" ||
		model.QualityGateID.ValueString() != "gate-a" {
		t.Errorf("state = %+v", model)
	}

	refreshed := &resource.ReadResponse{State: created.State}
	r.Read(context.Background(), resource.ReadRequest{State: created.State}, refreshed)
	if refreshed.Diagnostics.HasError() {
		t.Fatal(refreshed.Diagnostics)
	}
	if !created.State.Raw.Equal(refreshed.State.Raw) {
		t.Errorf("unchanged refresh changed state: before %v, after %v", created.State.Raw, refreshed.State.Raw)
	}

	updated := &resource.UpdateResponse{State: refreshed.State}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan: projectGatePlan(t, s, "gate-b"), State: refreshed.State,
	}, updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	if instance.gate != "gate-b" {
		t.Errorf("gate = %q, want gate-b", instance.gate)
	}

	deleted := &resource.DeleteResponse{}
	r.Delete(context.Background(), resource.DeleteRequest{State: updated.State}, deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	if instance.gate != "" {
		t.Errorf("gate = %q after delete, want none", instance.gate)
	}
}

// A change of the gate in the UI shows in the state, so the plan shows it.
func TestProjectQualityGateResourceReadReportsDrift(t *testing.T) {
	t.Parallel()

	instance := newFakeProjectGateInstance()
	instance.gate = "gate-b"
	r := &projectQualityGateResource{client: instance.start(t)}
	s := projectGateResourceSchema(t)
	state := projectGateState(t, s, "gate-a")

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := providertest.ReadModel[projectQualityGateModel](t, resp.State).QualityGateID.ValueString(); got != "gate-b" {
		t.Errorf("quality_gate_id = %q, want gate-b", got)
	}
}

func TestProjectQualityGateResourceReadRemovesTheResource(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		prepare      func(*fakeProjectGateInstance)
		wantWarnings int
	}{
		"default fallback": {prepare: func(f *fakeProjectGateInstance) { f.fallback = true }},
		"no association":   {prepare: func(*fakeProjectGateInstance) { /* the fake has no gate */ }},
		"missing project": {
			prepare:      func(f *fakeProjectGateInstance) { f.projectExists = false },
			wantWarnings: 1,
		},
		"missing organization": {
			prepare:      func(f *fakeProjectGateInstance) { f.organizationExists = false },
			wantWarnings: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instance := newFakeProjectGateInstance()
			test.prepare(instance)
			r := &projectQualityGateResource{client: instance.start(t)}
			s := projectGateResourceSchema(t)
			state := projectGateState(t, s, "gate-a")

			resp := &resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			if resp.Diagnostics.WarningsCount() != test.wantWarnings {
				t.Errorf("read gave %d warnings, want %d", resp.Diagnostics.WarningsCount(), test.wantWarnings)
			}
			if !resp.State.Raw.IsNull() {
				t.Error("the resource stayed in the state")
			}
		})
	}
}

func TestProjectQualityGateResourceCreateFailures(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		prepare func(*fakeProjectGateInstance)
		gate    string
		want    string
	}{
		"missing organization": {
			prepare: func(f *fakeProjectGateInstance) { f.organizationExists = false },
			gate:    "gate-a", want: "Organization my-org not found",
		},
		"missing project": {
			prepare: func(f *fakeProjectGateInstance) { f.projectExists = false },
			gate:    "gate-a", want: "my-project",
		},
		"missing gate": {
			prepare: func(*fakeProjectGateInstance) { /* the fake has no gate-c */ },
			gate:    "gate-c", want: "Quality gate gate-c not found",
		},
		"refused create": {
			prepare: func(f *fakeProjectGateInstance) { f.createStatus = http.StatusBadRequest },
			gate:    "gate-a", want: "Cannot assign the quality gate to the project my-project",
		},
		"free plan": {
			prepare: func(f *fakeProjectGateInstance) { f.createStatus = http.StatusForbidden },
			gate:    "gate-a", want: "Team or the Enterprise plan",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instance := newFakeProjectGateInstance()
			test.prepare(instance)
			r := &projectQualityGateResource{client: instance.start(t)}
			s := projectGateResourceSchema(t)

			resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
			r.Create(context.Background(), resource.CreateRequest{Plan: projectGatePlan(t, s, test.gate)}, resp)
			providertest.AssertDiagnosticsContain(t, resp.Diagnostics, test.want)
			if !resp.State.Raw.IsNull() {
				t.Error("a failed create wrote the state")
			}
		})
	}
}

func TestProjectQualityGateResourceImportState(t *testing.T) {
	t.Parallel()

	s := projectGateResourceSchema(t)
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	NewProjectQualityGateResource().(*projectQualityGateResource).ImportState(
		context.Background(), resource.ImportStateRequest{ID: "my-org/my-project"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	model := providertest.ReadModel[projectQualityGateModel](t, resp.State)
	if model.Organization.ValueString() != "my-org" || model.ProjectKey.ValueString() != "my-project" {
		t.Errorf("state = %+v", model)
	}
}

func TestProjectQualityGateResourceImportStateRejectsABadID(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"my-org", "/my-project", "my-org/"} {
		s := projectGateResourceSchema(t)
		resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
		NewProjectQualityGateResource().(*projectQualityGateResource).ImportState(
			context.Background(), resource.ImportStateRequest{ID: id}, resp)
		providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Cannot import the project quality gate")
	}
}
