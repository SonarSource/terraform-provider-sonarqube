// Package providertest holds helpers for the unit tests of the resources and
// data sources of every product.
package providertest

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// NewCloudClient builds a client that talks to a fake instance of
// SonarQube Cloud, held in memory by srv.
func NewCloudClient(srv *httptest.Server) *client.Client {
	return client.New(client.Config{
		URL:        srv.URL,
		APIURL:     srv.URL,
		Token:      "test-token",
		Product:    client.ProductCloud,
		HTTPClient: srv.Client(),
	})
}

// SchemaValue builds a value of any resource schema. Every attribute the
// caller leaves out is null, as it is for an attribute absent from the
// configuration.
func SchemaValue(t *testing.T, s schema.Schema, attributes map[string]string) tftypes.Value {
	t.Helper()

	objectType, ok := s.Type().TerraformType(context.Background()).(tftypes.Object)
	if !ok {
		t.Fatalf("the schema of the resource is not an object type")
	}

	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attributeType := range objectType.AttributeTypes {
		if value, given := attributes[name]; given {
			values[name] = tftypes.NewValue(tftypes.String, value)
			continue
		}
		values[name] = tftypes.NewValue(attributeType, nil)
	}
	return tftypes.NewValue(objectType, values)
}

// EmptyState is the state that the framework hands to an operation before the
// operation writes anything into it.
func EmptyState(t *testing.T, s schema.Schema) tfsdk.State {
	t.Helper()

	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
}

// ReadModel reads the resource state back into the model of the resource.
func ReadModel[T any](t *testing.T, state tfsdk.State) T {
	t.Helper()

	var model T
	if diags := state.Get(context.Background(), &model); diags.HasError() {
		t.Fatalf("cannot read the state: %v", diags)
	}
	return model
}

// AssertCallOrder checks that the calls arrived in the given order. Other
// calls between them are allowed.
func AssertCallOrder(t *testing.T, calls []string, first, second string) {
	t.Helper()

	firstAt, secondAt := -1, -1
	for i, call := range calls {
		if call == first && firstAt == -1 {
			firstAt = i
		}
		if call == second && firstAt != -1 && i > firstAt {
			secondAt = i
			break
		}
	}

	if firstAt == -1 {
		t.Errorf("%s was never called, got %v", first, calls)
		return
	}
	if secondAt == -1 {
		t.Errorf("%s did not follow %s, got %v", second, first, calls)
	}
}

// AssertDiagnosticsContain checks that an error in the diagnostics mentions
// want, in its summary or in its detail.
func AssertDiagnosticsContain(t *testing.T, diagnostics diag.Diagnostics, want string) {
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

// FakeOrganizationUUID is the UUID of the organization that the fake
// instances of the unit tests hold.
const FakeOrganizationUUID = "00000000-0000-4000-8000-000000000001"

// ValidateString runs the validators of a string attribute of the schema on
// value, and returns what they report.
func ValidateString(t *testing.T, s schema.Schema, name, value string) diag.Diagnostics {
	t.Helper()

	attribute, ok := s.Attributes[name].(schema.StringAttribute)
	if !ok {
		t.Fatalf("%s is not a string attribute", name)
	}

	resp := &validator.StringResponse{}
	req := validator.StringRequest{Path: path.Root(name), ConfigValue: types.StringValue(value)}
	for _, v := range attribute.Validators {
		v.ValidateString(context.Background(), req, resp)
	}
	return resp.Diagnostics
}
