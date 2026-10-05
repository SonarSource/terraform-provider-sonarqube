package cloud

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func readQualityGateDataSource(t *testing.T, c *client.Client, name string) (tfsdk.State, diag.Diagnostics) {
	t.Helper()
	d := &qualityGateDataSource{client: c}
	respSchema := &datasource.SchemaResponse{}
	d.Schema(t.Context(), datasource.SchemaRequest{}, respSchema)
	if err := respSchema.Schema.ValidateImplementation(t.Context()); err != nil {
		t.Fatal(err)
	}
	s := respSchema.Schema
	objectType := s.Type().TerraformType(t.Context()).(tftypes.Object)
	values := map[string]tftypes.Value{}
	for field, attrType := range objectType.AttributeTypes {
		values[field] = tftypes.NewValue(attrType, nil)
	}
	values["organization"] = tftypes.NewValue(tftypes.String, "my-org")
	values["name"] = tftypes.NewValue(tftypes.String, name)
	config := tfsdk.Config{Schema: s, Raw: tftypes.NewValue(objectType, values)}
	read := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objectType, nil)}}
	d.Read(t.Context(), datasource.ReadRequest{Config: config}, read)
	return read.State, read.Diagnostics
}

func TestQualityGateDataSourceRead(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.gateExists, fake.name, fake.ai = true, "My Gate", true
	state, diagnostics := readQualityGateDataSource(t, c, "My Gate")
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	model := providertest.ReadModel[qualityGateDataSourceModel](t, state)
	if model.ID.ValueString() != "gate-id" || !model.AIQualified.ValueBool() {
		t.Errorf("state = %+v", model)
	}
}

func TestQualityGateDataSourceReadsBuiltInGate(t *testing.T) {
	t.Parallel()
	fake, c := newFakeQualityGates(t)
	fake.gateExists, fake.name, fake.builtIn = true, "Sonar way", true
	state, diagnostics := readQualityGateDataSource(t, c, "Sonar way")
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	model := providertest.ReadModel[qualityGateDataSourceModel](t, state)
	if model.ID.ValueString() != "gate-id" || model.Conditions.IsNull() {
		t.Errorf("built-in gate state = %+v", model)
	}
}

func TestQualityGateDataSourceAbsent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/organizations/organizations" {
			w.Write([]byte(`[{"key":"my-org","uuidV4":"00000000-0000-4000-8000-000000000001"}]`))
			return
		}
		w.Write([]byte(`{"qualityGates":[],"page":{"total":0}}`))
	}))
	defer srv.Close()
	_, diagnostics := readQualityGateDataSource(t, providertest.NewCloudClient(srv), "Absent")
	providertest.AssertDiagnosticsContain(t, diagnostics, "Cannot find the quality gate")
}

func TestQualityGateDataSourceMetadataAndConfigure(t *testing.T) {
	t.Parallel()
	_, c := newFakeQualityGates(t)
	d := NewQualityGateDataSource().(*qualityGateDataSource)
	metadata := &datasource.MetadataResponse{}
	d.Metadata(t.Context(), datasource.MetadataRequest{ProviderTypeName: "sonarqube"}, metadata)
	if metadata.TypeName != "sonarqube_cloud_quality_gate" {
		t.Errorf("type name = %q", metadata.TypeName)
	}
	configured := &datasource.ConfigureResponse{}
	d.Configure(t.Context(), datasource.ConfigureRequest{ProviderData: c}, configured)
	if configured.Diagnostics.HasError() || d.client != c {
		t.Errorf("configure = %v, client %p", configured.Diagnostics, d.client)
	}
}
