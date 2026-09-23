package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

func TestProviderMetadata(t *testing.T) {
	t.Parallel()

	p := New("test")()

	resp := &provider.MetadataResponse{}
	p.Metadata(context.Background(), provider.MetadataRequest{}, resp)

	if resp.TypeName != "sonarqube" {
		t.Errorf("unexpected provider type name: got %q, want %q", resp.TypeName, "sonarqube")
	}

	if resp.Version != "test" {
		t.Errorf("unexpected provider version: got %q, want %q", resp.Version, "test")
	}
}

func TestProviderSchema(t *testing.T) {
	t.Parallel()

	resp := providerSchema(t)

	if resp.Diagnostics.HasError() {
		t.Errorf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	if err := resp.Schema.ValidateImplementation(context.Background()); err != nil {
		t.Errorf("invalid schema implementation: %v", err)
	}
}

func TestConfigureReadsTheEnvironment(t *testing.T) {
	resp := configure(t, map[string]string{envToken: "token-from-the-environment"}, nil)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	c := configuredClient(t, resp)
	if got, want := c.URL(), client.CloudURL; got != want {
		t.Errorf("URL() = %q, want the default %q", got, want)
	}
	if got, want := c.APIURL(), "https://api.sonarcloud.io"; got != want {
		t.Errorf("APIURL() = %q, want %q derived from the default", got, want)
	}
	if got, want := c.Product(), client.ProductCloud; got != want {
		t.Errorf("Product() = %q, want %q", got, want)
	}
}

// A value in the configuration wins over the environment.
func TestConfigurePrefersTheConfiguration(t *testing.T) {
	resp := configure(t,
		map[string]string{
			envToken: "token-from-the-environment",
			envURL:   "https://from-the-environment.example.com",
		},
		map[string]tftypes.Value{
			"url": tftypes.NewValue(tftypes.String, "https://from-the-configuration.example.com"),
		})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	c := configuredClient(t, resp)
	if got, want := c.URL(), "https://from-the-configuration.example.com"; got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
}

// api_url is the escape hatch for a deployment whose api host does not follow
// the usual naming.
func TestConfigureKeepsAnExplicitAPIURL(t *testing.T) {
	resp := configure(t,
		map[string]string{envToken: "a-token"},
		map[string]tftypes.Value{
			"url":     tftypes.NewValue(tftypes.String, "https://dev11.sc-dev11.io"),
			"api_url": tftypes.NewValue(tftypes.String, "https://api.example.com"),
		})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	c := configuredClient(t, resp)
	if got, want := c.APIURL(), "https://api.example.com"; got != want {
		t.Errorf("APIURL() = %q, want %q", got, want)
	}
}

func TestConfigureNeedsAToken(t *testing.T) {
	resp := configure(t, nil, nil)

	assertErrorContains(t, resp, "Missing token")
}

// The schema accepts "server" so that the contract is visible, but this
// release manages SonarQube Cloud only.
func TestConfigureRefusesServer(t *testing.T) {
	resp := configure(t,
		map[string]string{envToken: "a-token"},
		map[string]tftypes.Value{
			"product": tftypes.NewValue(tftypes.String, string(client.ProductServer)),
		})

	assertErrorContains(t, resp, "SonarQube Server is not supported")
}

// A token that is not known yet must stop the provider, not fall through to
// the environment variable, which would authenticate as somebody else.
func TestConfigureRefusesAnUnknownToken(t *testing.T) {
	resp := configure(t,
		map[string]string{envToken: "token-from-the-environment"},
		map[string]tftypes.Value{
			"token": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		})

	assertErrorContains(t, resp, "Unknown value for token")
}

func providerSchema(t *testing.T) *provider.SchemaResponse {
	t.Helper()

	resp := &provider.SchemaResponse{}
	New("test")().Schema(context.Background(), provider.SchemaRequest{}, resp)
	return resp
}

// configure runs Configure with the given environment and attributes. Every
// variable and every attribute that the caller leaves out is empty, so that no
// test depends on the environment of the machine it runs on.
func configure(t *testing.T, env map[string]string, attributes map[string]tftypes.Value) *provider.ConfigureResponse {
	t.Helper()

	for _, name := range []string{envURL, envAPIURL, envToken} {
		t.Setenv(name, env[name])
	}

	ctx := context.Background()
	schema := providerSchema(t).Schema

	objectType, ok := schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("the provider schema is not an object type")
	}

	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attributeType := range objectType.AttributeTypes {
		if value, given := attributes[name]; given {
			values[name] = value
			continue
		}
		values[name] = tftypes.NewValue(attributeType, nil)
	}

	req := provider.ConfigureRequest{
		Config: tfsdk.Config{Schema: schema, Raw: tftypes.NewValue(objectType, values)},
	}
	resp := &provider.ConfigureResponse{}
	New("test")().Configure(ctx, req, resp)
	return resp
}

func configuredClient(t *testing.T, resp *provider.ConfigureResponse) *client.Client {
	t.Helper()

	c, ok := resp.DataSourceData.(*client.Client)
	if !ok {
		t.Fatalf("DataSourceData is %T, want *client.Client", resp.DataSourceData)
	}
	if resp.ResourceData != resp.DataSourceData {
		t.Error("the resources and the data sources got different clients")
	}
	return c
}

func assertErrorContains(t *testing.T, resp *provider.ConfigureResponse, want string) {
	t.Helper()

	if !resp.Diagnostics.HasError() {
		t.Fatalf("Configure reported no error, want one about %q", want)
	}
	for _, diagnostic := range resp.Diagnostics.Errors() {
		if strings.Contains(diagnostic.Summary(), want) || strings.Contains(diagnostic.Detail(), want) {
			return
		}
	}
	t.Errorf("no error mentions %q, got %v", want, resp.Diagnostics.Errors())
}

// A data source that is written but never registered is invisible to
// Terraform.
func TestProviderRegistersItsDataSources(t *testing.T) {
	t.Parallel()

	p := New("test")()

	dataSources := p.DataSources(context.Background())
	if got, want := len(dataSources), 1; got != want {
		t.Fatalf("the provider registers %d data sources, want %d", got, want)
	}

	resp := &datasource.MetadataResponse{}
	dataSources[0]().Metadata(
		context.Background(),
		datasource.MetadataRequest{ProviderTypeName: "sonarqube"},
		resp,
	)
	if got, want := resp.TypeName, "sonarqube_organization"; got != want {
		t.Errorf("the registered data source is %q, want %q", got, want)
	}

	if got := len(p.Resources(context.Background())); got != 0 {
		t.Errorf("the provider registers %d resources, want none in this release", got)
	}
}

// An address with no scheme must be refused while the provider is configured.
// url.Parse accepts it, so without the check every request would fail later
// with a message that names neither the attribute nor the value.
func TestConfigureRefusesAnAddressWithNoScheme(t *testing.T) {
	resp := configure(t,
		map[string]string{envToken: "a-token"},
		map[string]tftypes.Value{
			"url": tftypes.NewValue(tftypes.String, "sonarcloud.io"),
		})

	assertErrorContains(t, resp, "Invalid address in url")
}

func TestConfigureRefusesAnInvalidAPIAddress(t *testing.T) {
	resp := configure(t,
		map[string]string{envToken: "a-token"},
		map[string]tftypes.Value{
			"api_url": tftypes.NewValue(tftypes.String, "api.sonarcloud.io"),
		})

	assertErrorContains(t, resp, "Invalid address in api_url")
}
