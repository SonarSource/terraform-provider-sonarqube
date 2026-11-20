package provider

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var subcategoryLine = regexp.MustCompile(`(?m)^subcategory: "(.*)"$`)

// Each registered resource and data source needs a Registry page from
// `make docs`, with a group, an example and, for a resource, both forms of
// import.
func TestDocsPages(t *testing.T) {
	ctx := context.Background()
	p := New("test")()

	for _, factory := range p.Resources(ctx) {
		metadata := &resource.MetadataResponse{}
		factory().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "sonarqube"}, metadata)
		checkDocsPage(t, "resources", metadata.TypeName)
	}
	for _, factory := range p.DataSources(ctx) {
		metadata := &datasource.MetadataResponse{}
		factory().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "sonarqube"}, metadata)
		checkDocsPage(t, "data-sources", metadata.TypeName)
	}
}

func checkDocsPage(t *testing.T, dir, typeName string) {
	t.Helper()

	name := dir + "/" + strings.TrimPrefix(typeName, "sonarqube_") + ".md"
	content, err := os.ReadFile(filepath.Join("..", "..", "docs", name))
	if err != nil {
		t.Errorf("%s has no page %s. Run make docs.", typeName, name)
		return
	}
	text := string(content)

	match := subcategoryLine.FindStringSubmatch(text)
	if match == nil || match[1] == "" {
		t.Errorf("%s has no subcategory. Add %s to a group in templates/%s.md.tmpl.", name, typeName, dir)
	}
	if !strings.Contains(text, "## Example Usage") {
		t.Errorf("%s has no example. Add one to examples/%s/%s/.", name, dir, typeName)
	}
	if dir == "resources" {
		for _, want := range []string{"import {", "terraform import "} {
			if !strings.Contains(text, want) {
				t.Errorf("%s has no %q example. Add import-by-string-id.tf and import.sh.", name, want)
			}
		}
	}
}
