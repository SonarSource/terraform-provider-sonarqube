package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// organizationValue builds a value of the resource schema. Every attribute the
// caller leaves out is null, as it is for an attribute absent from the
// configuration.
func organizationValue(t *testing.T, s schema.Schema, attributes map[string]string) tftypes.Value {
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

// emptyState is the state that the framework hands to an operation before the
// operation writes anything into it.
func emptyState(t *testing.T, s schema.Schema) tfsdk.State {
	t.Helper()

	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
}

// readModel reads the resource state back into the model.
func readModel(t *testing.T, state tfsdk.State) organizationResourceModel {
	t.Helper()

	var model organizationResourceModel
	if diags := state.Get(context.Background(), &model); diags.HasError() {
		t.Fatalf("cannot read the state: %v", diags)
	}
	return model
}

// assertCallOrder checks that the calls arrived in the given order. Other
// calls between them are allowed.
func assertCallOrder(t *testing.T, calls []string, first, second string) {
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
