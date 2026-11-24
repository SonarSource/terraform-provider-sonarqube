package provider

import (
	"context"
	"maps"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// typeKey tells a resource from a data source of the same type name.
type typeKey struct {
	name       string
	dataSource bool
}

// supportScopedToken lists the types that the provider docs mark as supported
// with a scoped organization token.
var supportScopedToken = map[typeKey]bool{
	{"sonarqube_cloud_organization", true}:                        true,
	{"sonarqube_cloud_quality_gate", true}:                        true,
	{"sonarqube_quality_profile", true}:                           true,
	{"sonarqube_cloud_scoped_organization_token", false}:          true,
	{"sonarqube_cloud_organization_quality_gate_settings", false}: true,
	{"sonarqube_cloud_organization_automatic_analysis", false}:    true,
}

// configureEach configures each resource and data source of the provider with
// a client that uses token, and returns the diagnostics by type name.
func configureEach(t *testing.T, token string) map[typeKey]diag.Diagnostics {
	t.Helper()

	c := client.New(client.Config{URL: "https://sonarcloud.example", Token: token, Product: client.ProductCloud})
	ctx := context.Background()
	p := New("test")()
	got := map[typeKey]diag.Diagnostics{}
	for _, factory := range p.Resources(ctx) {
		r := factory()
		metadata := &resource.MetadataResponse{}
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "sonarqube"}, metadata)
		key := typeKey{metadata.TypeName, false}
		configurable, ok := r.(resource.ResourceWithConfigure)
		if !ok {
			t.Errorf("%v has no Configure method", key)
			continue
		}
		resp := &resource.ConfigureResponse{}
		configurable.Configure(ctx, resource.ConfigureRequest{ProviderData: c}, resp)
		got[key] = resp.Diagnostics
	}
	for _, factory := range p.DataSources(ctx) {
		d := factory()
		metadata := &datasource.MetadataResponse{}
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "sonarqube"}, metadata)
		key := typeKey{metadata.TypeName, true}
		configurable, ok := d.(datasource.DataSourceWithConfigure)
		if !ok {
			t.Errorf("%v has no Configure method", key)
			continue
		}
		resp := &datasource.ConfigureResponse{}
		configurable.Configure(ctx, datasource.ConfigureRequest{ProviderData: c}, resp)
		got[key] = resp.Diagnostics
	}
	return got
}

func TestScopedTokenIsRefusedByEachUnsupportedType(t *testing.T) {
	t.Parallel()

	got := configureEach(t, "sqco_test-token")
	for name := range supportScopedToken {
		if _, registered := got[name]; !registered {
			t.Errorf("%v supports a scoped organization token, but the provider does not register it", name)
		}
	}
	for name, diagnostics := range got {
		if supportScopedToken[name] {
			if diagnostics.HasError() {
				t.Errorf("%v supports a scoped organization token, but Configure fails: %v", name, diagnostics)
			}
			continue
		}
		if !diagnostics.HasError() || diagnostics.Errors()[0].Summary() != "This needs a user token" {
			t.Errorf("%v does not support a scoped organization token, but Configure gives %v", name, diagnostics)
		}
	}
}

func TestUserTokenIsAcceptedByEachType(t *testing.T) {
	t.Parallel()

	for name, diagnostics := range configureEach(t, "test-token") {
		if diagnostics.HasError() {
			t.Errorf("%v: %v", name, diagnostics)
		}
	}
}

// supportedRow matches a row of the docs table that marks a type as supported,
// such as "| `sonarqube_cloud_quality_gate` data source | Supported |".
var supportedRow = regexp.MustCompile("(?m)^\\| `([a-z_]+)`( data source)? \\| Supported")

func TestScopedTokenDocsTableMatchesTheSupportedTypes(t *testing.T) {
	t.Parallel()

	template, err := os.ReadFile("../../templates/index.md.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	documented := map[typeKey]bool{}
	for _, row := range supportedRow.FindAllStringSubmatch(string(template), -1) {
		documented[typeKey{row[1], row[2] != ""}] = true
	}
	if !maps.Equal(documented, supportScopedToken) {
		t.Errorf("the docs table marks %v as supported, but supportScopedToken holds %v", documented, supportScopedToken)
	}
}
