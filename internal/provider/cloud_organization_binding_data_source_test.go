package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
)

func cloudOrganizationBindingDataSourceSchema(t *testing.T) schema.Schema {
	t.Helper()

	resp := &datasource.SchemaResponse{}
	NewCloudOrganizationBindingDataSource().Schema(context.Background(), datasource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func TestCloudOrganizationBindingDataSourceMetadata(t *testing.T) {
	t.Parallel()

	resp := &datasource.MetadataResponse{}
	NewCloudOrganizationBindingDataSource().Metadata(
		context.Background(),
		datasource.MetadataRequest{ProviderTypeName: "sonarqube"},
		resp,
	)

	if got, want := resp.TypeName, "sonarqube_cloud_organization_binding"; got != want {
		t.Errorf("TypeName = %q, want %q", got, want)
	}
}

func TestCloudOrganizationBindingDataSourceSchema(t *testing.T) {
	t.Parallel()

	s := cloudOrganizationBindingDataSourceSchema(t)

	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Errorf("invalid schema implementation: %v", err)
	}
	if !s.Attributes["organization_key"].IsRequired() {
		t.Error("organization_key must be required")
	}
}

func TestCloudOrganizationBindingDataSourceRead(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.bind("my-org", theOrganizationID)
	d := &cloudOrganizationBindingDataSource{client: instance.start(t)}

	s := cloudOrganizationBindingDataSourceSchema(t)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(context.Background(), datasource.ReadRequest{
		Config: tfsdk.Config{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"organization_key": "my-org",
		})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	state := readModel[cloudOrganizationBindingModel](t, resp.State)
	if got, want := state.ID.ValueString(), theBindingID; got != want {
		t.Errorf("id = %q, want %q", got, want)
	}
	if got, want := state.InstallationID.ValueString(), "12345678"; got != want {
		t.Errorf("installation_id = %q, want %q", got, want)
	}
	if got, want := state.OrganizationKey.ValueString(), "my-org"; got != want {
		t.Errorf("organization_key = %q, want %q", got, want)
	}
}

// A data source that finds nothing must fail. It cannot report an empty
// result the way a resource does, because the configuration asked for a
// binding that must be there.
func TestCloudOrganizationBindingDataSourceReadOfAnUnboundOrganization(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.organizations["my-org"] = theOrganizationID
	d := &cloudOrganizationBindingDataSource{client: instance.start(t)}

	s := cloudOrganizationBindingDataSourceSchema(t)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(context.Background(), datasource.ReadRequest{
		Config: tfsdk.Config{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"organization_key": "my-org",
		})},
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("an organization with no binding was read")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(got, "no binding") {
		t.Errorf("summary = %q, want it to report that there is no binding", got)
	}
	// The data source has nothing more to say, so no space is left hanging.
	if got := resp.Diagnostics.Errors()[0].Detail(); !strings.HasSuffix(got, "not bound to a DevOps platform.") {
		t.Errorf("detail = %q, want it to end with the sentence and nothing after it", got)
	}
}

func TestCloudOrganizationBindingDataSourceReadOfAnUnknownOrganization(t *testing.T) {
	t.Parallel()

	d := &cloudOrganizationBindingDataSource{client: newFakeBoundInstance().start(t)}

	s := cloudOrganizationBindingDataSourceSchema(t)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(context.Background(), datasource.ReadRequest{
		Config: tfsdk.Config{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"organization_key": "gone",
		})},
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a key that names no organization was accepted")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(got, "not found") {
		t.Errorf("summary = %q, want it to report that the organization was not found", got)
	}
}

// A binding to a platform that is not GitHub carries no installation. The
// attribute is computed here, so it must hold no value rather than an empty
// string.
func TestCloudOrganizationBindingDataSourceReadOfAnotherPlatform(t *testing.T) {
	t.Parallel()

	instance := newFakeBoundInstance()
	instance.bindTo("gitlab-org", theOrganizationID, "gitlab")
	d := &cloudOrganizationBindingDataSource{client: instance.start(t)}

	s := cloudOrganizationBindingDataSourceSchema(t)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(context.Background(), datasource.ReadRequest{
		Config: tfsdk.Config{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"organization_key": "gitlab-org",
		})},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	state := readModel[cloudOrganizationBindingModel](t, resp.State)
	if !state.InstallationID.IsNull() {
		t.Errorf("installation_id = %v, want no value for a binding that carries none", state.InstallationID)
	}
	if got, want := state.DevOpsPlatform.ValueString(), "gitlab"; got != want {
		t.Errorf("dev_ops_platform = %q, want %q", got, want)
	}
}
