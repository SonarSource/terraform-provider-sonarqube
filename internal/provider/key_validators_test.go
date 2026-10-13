package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// keyKind names the rule that a key attribute follows.
type keyKind string

const (
	organizationKey keyKind = "organization key"
	projectKey      keyKind = "project key"
)

// keyAttributes gives the rule of each attribute that holds a key. An attribute
// named `key` holds the key of the object that the type manages, so its rule
// depends on the type.
func keyAttributes(typeName string) map[string]keyKind {
	attributes := map[string]keyKind{
		"organization":     organizationKey,
		"organization_key": organizationKey,
		"project_key":      projectKey,
	}
	switch typeName {
	case "sonarqube_cloud_organization":
		attributes["key"] = organizationKey
	case "sonarqube_project":
		attributes["key"] = projectKey
	}
	return attributes
}

// keyValues holds, for each rule, a value that it accepts and a value that it
// refuses.
var keyValues = map[keyKind]struct{ valid, invalid string }{
	organizationKey: {valid: "my-org", invalid: "My_Org"},
	projectKey:      {valid: "my_project", invalid: "bad key"},
}

// stringAttribute is the part of a resource or data source string attribute
// that this test reads.
type stringAttribute interface {
	IsRequired() bool
	IsOptional() bool
	StringValidators() []validator.String
}

// A key that breaks the rule of the server must fail at plan time, in every
// resource and data source. A new type that forgets the validator fails here.
func TestKeyAttributesValidateTheKey(t *testing.T) {
	t.Parallel()

	p := New("test")()
	ctx := context.Background()
	checked := 0

	for _, newResource := range p.Resources(ctx) {
		r := newResource()
		metadata := &resource.MetadataResponse{}
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "sonarqube"}, metadata)
		schema := &resource.SchemaResponse{}
		r.Schema(ctx, resource.SchemaRequest{}, schema)
		for name, attribute := range schema.Schema.Attributes {
			if a, ok := attribute.(stringAttribute); ok {
				checked += checkKeyAttribute(t, metadata.TypeName, name, a)
			}
		}
	}

	for _, newDataSource := range p.DataSources(ctx) {
		d := newDataSource()
		metadata := &datasource.MetadataResponse{}
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "sonarqube"}, metadata)
		schema := &datasource.SchemaResponse{}
		d.Schema(ctx, datasource.SchemaRequest{}, schema)
		for name, attribute := range schema.Schema.Attributes {
			if a, ok := attribute.(stringAttribute); ok {
				checked += checkKeyAttribute(t, "data."+metadata.TypeName, name, a)
			}
		}
	}

	if checked == 0 {
		t.Fatal("no key attribute was checked")
	}
}

// checkKeyAttribute checks one attribute, and reports 1 when it holds a key
// that the configuration sets. A computed key comes from the server, so it has
// no validator.
func checkKeyAttribute(t *testing.T, typeName, name string, attribute stringAttribute) int {
	t.Helper()

	kind, isKey := keyAttributes(strings.TrimPrefix(typeName, "data."))[name]
	if !isKey || (!attribute.IsRequired() && !attribute.IsOptional()) {
		return 0
	}
	values := keyValues[kind]
	if refused(attribute.StringValidators(), values.valid) {
		t.Errorf("%s.%s refuses the valid %s %q", typeName, name, kind, values.valid)
	}
	if !refused(attribute.StringValidators(), values.invalid) {
		t.Errorf("%s.%s accepts the invalid %s %q", typeName, name, kind, values.invalid)
	}
	return 1
}

func refused(validators []validator.String, value string) bool {
	for _, v := range validators {
		resp := &validator.StringResponse{}
		v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(value)}, resp)
		if resp.Diagnostics.HasError() {
			return true
		}
	}
	return false
}
