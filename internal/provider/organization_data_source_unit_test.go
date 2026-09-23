package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// These tests drive the data source directly, so they need neither a Terraform
// binary nor credentials. The acceptance test beside them covers the same
// ground against a live instance.

func TestOrganizationDataSourceMetadata(t *testing.T) {
	t.Parallel()

	resp := &datasource.MetadataResponse{}
	NewOrganizationDataSource().Metadata(
		context.Background(),
		datasource.MetadataRequest{ProviderTypeName: "sonarqube"},
		resp,
	)

	if got, want := resp.TypeName, "sonarqube_organization"; got != want {
		t.Errorf("TypeName = %q, want %q", got, want)
	}
}

func TestOrganizationDataSourceSchema(t *testing.T) {
	t.Parallel()

	schema := organizationSchema(t)

	if err := schema.ValidateImplementation(context.Background()); err != nil {
		t.Errorf("invalid schema implementation: %v", err)
	}
	if !schema.Attributes["key"].IsRequired() {
		t.Error("key must be required, because it names the organization to read")
	}
}

// The framework calls Configure without data while it validates a
// configuration, before the provider itself is configured.
func TestOrganizationDataSourceConfigureWithoutProvider(t *testing.T) {
	t.Parallel()

	d := &organizationDataSource{}
	resp := &datasource.ConfigureResponse{}
	d.Configure(context.Background(), datasource.ConfigureRequest{}, resp)

	if resp.Diagnostics.HasError() {
		t.Errorf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if d.client != nil {
		t.Error("the data source kept a client although none was given")
	}
}

func TestOrganizationDataSourceConfigureRejectsAnotherType(t *testing.T) {
	t.Parallel()

	d := &organizationDataSource{}
	resp := &datasource.ConfigureResponse{}
	d.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: "not a client"}, resp)

	assertDiagnosticsContain(t, resp.Diagnostics, "Unexpected provider data")
}

// The guard that every data source and resource of SonarQube Cloud repeats.
func TestOrganizationDataSourceConfigureNeedsCloud(t *testing.T) {
	t.Parallel()

	serverClient := client.New(client.Config{URL: "https://sonarqube.example.com", Product: client.ProductServer})

	d := &organizationDataSource{}
	resp := &datasource.ConfigureResponse{}
	d.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: serverClient}, resp)

	assertDiagnosticsContain(t, resp.Diagnostics, "need SonarQube Cloud")
	if d.client != nil {
		t.Error("the data source kept a client that is not SonarQube Cloud")
	}
}

func TestOrganizationDataSourceRead(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"organizations":[{"key":"my-org","name":"My Organization",
			"description":"Managed by Terraform","url":"https://example.com",
			"avatar":"https://example.com/avatar.png"}]}`))
	}))
	defer srv.Close()

	state, diagnostics := readOrganization(t, srv, "my-org")

	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}

	var got organizationDataSourceModel
	if diags := state.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("cannot read the state: %v", diags)
	}

	for _, field := range []struct {
		name string
		got  string
		want string
	}{
		{"key", got.Key.ValueString(), "my-org"},
		{"name", got.Name.ValueString(), "My Organization"},
		{"description", got.Description.ValueString(), "Managed by Terraform"},
		{"url", got.URL.ValueString(), "https://example.com"},
		{"avatar_url", got.AvatarURL.ValueString(), "https://example.com/avatar.png"},
	} {
		if field.got != field.want {
			t.Errorf("%s = %q, want %q", field.name, field.got, field.want)
		}
	}
}

// The message matters as much as the failure: a missing organization and a
// token without permission look identical, so the text must name the instance
// and mention the permission.
func TestOrganizationDataSourceReadNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"organizations":[]}`))
	}))
	defer srv.Close()

	_, diagnostics := readOrganization(t, srv, "absent-org")

	assertDiagnosticsContain(t, diagnostics, "absent-org not found")
	assertDiagnosticsContain(t, diagnostics, srv.URL)
	assertDiagnosticsContain(t, diagnostics, "token can read")
}

func TestOrganizationDataSourceReadReportsOtherFailures(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"errors":[{"msg":"the instance is unwell"}]}`))
	}))
	defer srv.Close()

	_, diagnostics := readOrganization(t, srv, "my-org")

	assertDiagnosticsContain(t, diagnostics, "Cannot read organization my-org")
	assertDiagnosticsContain(t, diagnostics, "the instance is unwell")
}

func organizationSchema(t *testing.T) schema.Schema {
	t.Helper()

	resp := &datasource.SchemaResponse{}
	NewOrganizationDataSource().Schema(context.Background(), datasource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func readOrganization(t *testing.T, srv *httptest.Server, key string) (tfsdk.State, diag.Diagnostics) {
	t.Helper()

	ctx := context.Background()
	schema := organizationSchema(t)

	d := &organizationDataSource{}
	configureResp := &datasource.ConfigureResponse{}
	d.Configure(ctx, datasource.ConfigureRequest{
		ProviderData: client.New(client.Config{
			URL:        srv.URL,
			Token:      "test-token",
			Product:    client.ProductCloud,
			HTTPClient: srv.Client(),
		}),
	}, configureResp)
	if configureResp.Diagnostics.HasError() {
		t.Fatalf("Configure reported %v", configureResp.Diagnostics)
	}

	objectType, ok := schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("the schema of the data source is not an object type")
	}

	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attributeType := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(attributeType, nil)
	}
	values["key"] = tftypes.NewValue(tftypes.String, key)

	req := datasource.ReadRequest{
		Config: tfsdk.Config{Schema: schema, Raw: tftypes.NewValue(objectType, values)},
	}
	resp := &datasource.ReadResponse{
		State: tfsdk.State{Schema: schema, Raw: tftypes.NewValue(objectType, nil)},
	}
	d.Read(ctx, req, resp)

	return resp.State, resp.Diagnostics
}

func assertDiagnosticsContain(t *testing.T, diagnostics diag.Diagnostics, want string) {
	t.Helper()

	if !diagnostics.HasError() {
		t.Fatalf("no error reported, want one about %q", want)
	}
	for _, diagnostic := range diagnostics.Errors() {
		if strings.Contains(diagnostic.Summary(), want) || strings.Contains(diagnostic.Detail(), want) {
			return
		}
	}
	t.Errorf("no error mentions %q, got %v", want, diagnostics.Errors())
}
