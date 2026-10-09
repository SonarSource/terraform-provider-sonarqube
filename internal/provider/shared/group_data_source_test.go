package shared

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

func TestGroupDataSourceReadsBuiltInGroup(t *testing.T) {
	t.Parallel()
	f, c := newFakeGroups(t)
	f.groups["7"] = &fakeGroup{ID: "7", Name: "Members", Default: true}
	d := &groupDataSource{client: c}
	schemaResponse := &datasource.SchemaResponse{}
	d.Schema(t.Context(), datasource.SchemaRequest{}, schemaResponse)
	if schemaResponse.Diagnostics.HasError() {
		t.Fatal(schemaResponse.Diagnostics)
	}
	s := schemaResponse.Schema
	if err := s.ValidateImplementation(t.Context()); err != nil {
		t.Fatal(err)
	}
	objectType := s.Type().TerraformType(context.Background()).(tftypes.Object)
	values := map[string]tftypes.Value{}
	for name, attributeType := range objectType.AttributeTypes {
		switch name {
		case "organization":
			values[name] = tftypes.NewValue(tftypes.String, "my-org")
		case "name":
			values[name] = tftypes.NewValue(tftypes.String, "Members")
		default:
			values[name] = tftypes.NewValue(attributeType, nil)
		}
	}
	config := tfsdk.Config{Schema: s, Raw: tftypes.NewValue(objectType, values)}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objectType, nil)}}
	d.Read(t.Context(), datasource.ReadRequest{Config: config}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	state := providertest.ReadModel[groupDataSourceModel](t, resp.State)
	if state.ID.ValueString() != "7" || !state.Default.ValueBool() || state.Name.ValueString() != "Members" {
		t.Errorf("built-in group state = %+v", state)
	}
}
