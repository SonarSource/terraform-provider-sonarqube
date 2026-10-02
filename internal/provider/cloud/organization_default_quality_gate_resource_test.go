package cloud

import (
	"context"
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

// fakeDefaultGateInstance holds the organization my-org and the gates
// gate-a and gate-b, with the legacy identifiers 1 and 2.
type fakeDefaultGateInstance struct {
	organizationExists bool
	defaultGate        string
	calls              []string
	// setStatus makes the set call fail with this status when it is not 0.
	setStatus int
}

var fakeGateLegacyIDs = map[string]string{"gate-a": "1", "gate-b": "2"}

func newFakeDefaultGateInstance() *fakeDefaultGateInstance {
	return &fakeDefaultGateInstance{organizationExists: true, defaultGate: "sonar-way"}
}

func (f *fakeDefaultGateInstance) start(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /organizations/organizations":
			if !f.organizationExists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`[{"id":"organization-id","uuidV4":"` + providertest.FakeOrganizationUUID +
				`","key":"my-org","defaultQualityGateUuid":"` + f.defaultGate + `"}]`))
		case "GET /quality-gates/quality-gates/gate-a", "GET /quality-gates/quality-gates/gate-b":
			id := r.URL.Path[len("/quality-gates/quality-gates/"):]
			_, _ = w.Write([]byte(`{"id":"` + id + `","name":"Gate","legacyId":` + fakeGateLegacyIDs[id] + `}`))
		case "POST /api/qualitygates/set_as_default":
			f.setDefault(t, w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return providertest.NewCloudClient(srv)
}

func (f *fakeDefaultGateInstance) setDefault(t *testing.T, w http.ResponseWriter, r *http.Request) {
	if f.setStatus != 0 {
		w.WriteHeader(f.setStatus)
		_, _ = w.Write([]byte(`{"errors":[{"msg":"refused"}]}`))
		return
	}
	if err := r.ParseForm(); err != nil {
		t.Errorf("cannot read the form: %v", err)
	}
	if got := r.PostForm.Get("organization"); got != "my-org" {
		t.Errorf("organization = %q, want my-org", got)
	}
	for uuid, legacyID := range fakeGateLegacyIDs {
		if legacyID == r.PostForm.Get("id") {
			f.defaultGate = uuid
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func defaultGateResourceSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewOrganizationDefaultQualityGateResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func defaultGatePlan(t *testing.T, s schema.Schema, gate string) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"organization": "my-org", "quality_gate_id": gate,
	})}
}

func defaultGateState(t *testing.T, s schema.Schema, gate string) tfsdk.State {
	t.Helper()
	return tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "my-org", "organization": "my-org", "quality_gate_id": gate,
	})}
}

func TestOrganizationDefaultQualityGateResourceSchema(t *testing.T) {
	t.Parallel()

	s := defaultGateResourceSchema(t)
	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"organization", "quality_gate_id"} {
		if !s.Attributes[name].IsRequired() {
			t.Errorf("%s must be required", name)
		}
	}
	organization, ok := s.Attributes["organization"].(schema.StringAttribute)
	if !ok || len(organization.PlanModifiers) != 1 ||
		organization.PlanModifiers[0].Description(context.Background()) !=
			stringplanmodifier.RequiresReplace().Description(context.Background()) {
		t.Error("organization must require a replacement")
	}
	// A different gate changes the default in place.
	if gate, ok := s.Attributes["quality_gate_id"].(schema.StringAttribute); !ok || len(gate.PlanModifiers) != 0 {
		t.Error("quality_gate_id must not require a replacement")
	}
	resp := &resource.MetadataResponse{}
	NewOrganizationDefaultQualityGateResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "sonarqube"}, resp)
	if resp.TypeName != "sonarqube_cloud_organization_default_quality_gate" {
		t.Errorf("type name = %q", resp.TypeName)
	}
}

func TestOrganizationDefaultQualityGateResourceConfigureNeedsCloud(t *testing.T) {
	t.Parallel()

	r := &organizationDefaultQualityGateResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: client.New(client.Config{
		URL: "https://dev.example.io", Product: client.ProductServer,
	})}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "sonarqube_cloud_organization_default_quality_gate resource")
	if r.client != nil {
		t.Error("the resource retained a Server client")
	}
}

func TestOrganizationDefaultQualityGateResourceLifecycle(t *testing.T) {
	t.Parallel()

	instance := newFakeDefaultGateInstance()
	r := &organizationDefaultQualityGateResource{client: instance.start(t)}
	s := defaultGateResourceSchema(t)

	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: defaultGatePlan(t, s, "gate-a")}, created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	if instance.defaultGate != "gate-a" {
		t.Errorf("default = %q, want gate-a", instance.defaultGate)
	}
	model := providertest.ReadModel[organizationDefaultQualityGateModel](t, created.State)
	if model.ID.ValueString() != "my-org" || model.QualityGateID.ValueString() != "gate-a" {
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
		Plan: defaultGatePlan(t, s, "gate-b"), State: refreshed.State,
	}, updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	if instance.defaultGate != "gate-b" {
		t.Errorf("default = %q, want gate-b", instance.defaultGate)
	}

	calls := len(instance.calls)
	deleted := &resource.DeleteResponse{}
	r.Delete(context.Background(), resource.DeleteRequest{State: updated.State}, deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	if deleted.Diagnostics.WarningsCount() != 1 {
		t.Errorf("delete gave %d warnings, want 1", deleted.Diagnostics.WarningsCount())
	}
	if len(instance.calls) != calls {
		t.Errorf("delete called the API: %v", instance.calls[calls:])
	}
}

// A change of the default in the UI shows in the state, so the plan shows it.
func TestOrganizationDefaultQualityGateResourceReadReportsDrift(t *testing.T) {
	t.Parallel()

	instance := newFakeDefaultGateInstance()
	instance.defaultGate = "gate-b"
	r := &organizationDefaultQualityGateResource{client: instance.start(t)}
	s := defaultGateResourceSchema(t)
	state := defaultGateState(t, s, "gate-a")

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := providertest.ReadModel[organizationDefaultQualityGateModel](t, resp.State).QualityGateID.ValueString(); got != "gate-b" {
		t.Errorf("quality_gate_id = %q, want gate-b", got)
	}
}

func TestOrganizationDefaultQualityGateResourceReadOfAMissingOrganization(t *testing.T) {
	t.Parallel()

	instance := newFakeDefaultGateInstance()
	instance.organizationExists = false
	r := &organizationDefaultQualityGateResource{client: instance.start(t)}
	s := defaultGateResourceSchema(t)
	state := defaultGateState(t, s, "gate-a")

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if resp.Diagnostics.WarningsCount() != 1 {
		t.Errorf("read gave %d warnings, want 1", resp.Diagnostics.WarningsCount())
	}
	if !resp.State.Raw.IsNull() {
		t.Error("the resource stayed in the state")
	}
}

// An empty default would show a change in every plan, so the read refuses it
// and keeps the state.
func TestOrganizationDefaultQualityGateResourceReadRefusesAnEmptyDefault(t *testing.T) {
	t.Parallel()

	instance := newFakeDefaultGateInstance()
	instance.defaultGate = ""
	r := &organizationDefaultQualityGateResource{client: instance.start(t)}
	s := defaultGateResourceSchema(t)
	state := defaultGateState(t, s, "gate-a")

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "The API gave no default quality gate")
	if !state.Raw.Equal(resp.State.Raw) {
		t.Errorf("a failed read changed the state: %v", resp.State.Raw)
	}
}

func TestOrganizationDefaultQualityGateResourceCreateFailures(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		prepare func(*fakeDefaultGateInstance)
		gate    string
		want    string
	}{
		"missing organization": {
			prepare: func(f *fakeDefaultGateInstance) { f.organizationExists = false },
			gate:    "gate-a", want: "Organization my-org not found",
		},
		"missing gate": {
			prepare: func(*fakeDefaultGateInstance) { /* the fake has no gate-c */ },
			gate:    "gate-c", want: "Quality gate gate-c not found",
		},
		"refused set": {
			prepare: func(f *fakeDefaultGateInstance) { f.setStatus = http.StatusBadRequest },
			gate:    "gate-a", want: "Cannot set the default quality gate of the organization my-org",
		},
		"free plan": {
			prepare: func(f *fakeDefaultGateInstance) { f.setStatus = http.StatusForbidden },
			gate:    "gate-a", want: "Team or the Enterprise plan",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instance := newFakeDefaultGateInstance()
			test.prepare(instance)
			r := &organizationDefaultQualityGateResource{client: instance.start(t)}
			s := defaultGateResourceSchema(t)

			resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
			r.Create(context.Background(), resource.CreateRequest{Plan: defaultGatePlan(t, s, test.gate)}, resp)
			providertest.AssertDiagnosticsContain(t, resp.Diagnostics, test.want)
			if !resp.State.Raw.IsNull() {
				t.Error("a failed create wrote the state")
			}
		})
	}
}

func TestOrganizationDefaultQualityGateResourceImportState(t *testing.T) {
	t.Parallel()

	s := defaultGateResourceSchema(t)
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	NewOrganizationDefaultQualityGateResource().(*organizationDefaultQualityGateResource).ImportState(
		context.Background(), resource.ImportStateRequest{ID: "my-org"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	model := providertest.ReadModel[organizationDefaultQualityGateModel](t, resp.State)
	if model.Organization.ValueString() != "my-org" || model.ID.ValueString() != "my-org" {
		t.Errorf("state = %+v, want my-org", model)
	}
}
