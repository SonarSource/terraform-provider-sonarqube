package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
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

	p := New("test")()

	resp := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, resp)

	if resp.Diagnostics.HasError() {
		t.Errorf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	if err := resp.Schema.ValidateImplementation(context.Background()); err != nil {
		t.Errorf("invalid schema implementation: %v", err)
	}
}
