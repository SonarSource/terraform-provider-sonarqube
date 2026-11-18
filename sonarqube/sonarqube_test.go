package sonarqube_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/SonarSource/terraform-provider-sonarqube/sonarqube"
)

func TestNewBuildsTheProvider(t *testing.T) {
	t.Parallel()

	p := sonarqube.New("1.2.3")()

	meta := &provider.MetadataResponse{}
	p.Metadata(context.Background(), provider.MetadataRequest{}, meta)
	if meta.TypeName != "sonarqube" || meta.Version != "1.2.3" {
		t.Errorf("Metadata() = %q %q, want %q %q", meta.TypeName, meta.Version, "sonarqube", "1.2.3")
	}

	server, err := providerserver.NewProtocol6WithError(p)()
	if err != nil {
		t.Fatalf("NewProtocol6WithError() error = %v", err)
	}
	resp, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("GetProviderSchema() error = %v", err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Errorf("GetProviderSchema() diagnostic: %s: %s", d.Summary, d.Detail)
		}
	}

	attributes := map[string]bool{}
	for _, a := range resp.Provider.Block.Attributes {
		attributes[a.Name] = true
	}
	for _, name := range []string{"url", "api_url", "token"} {
		if !attributes[name] {
			t.Errorf("provider schema has no %q attribute", name)
		}
	}
	if len(resp.ResourceSchemas) == 0 || len(resp.DataSourceSchemas) == 0 {
		t.Errorf("provider has %d resources and %d data sources, want at least one of each",
			len(resp.ResourceSchemas), len(resp.DataSourceSchemas))
	}
}
