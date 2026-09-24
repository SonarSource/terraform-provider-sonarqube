package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

func TestOrganizationResourceMetadata(t *testing.T) {
	t.Parallel()

	resp := &resource.MetadataResponse{}
	NewOrganizationResource().Metadata(
		context.Background(),
		resource.MetadataRequest{ProviderTypeName: "sonarqube"},
		resp,
	)

	if got, want := resp.TypeName, "sonarqube_organization"; got != want {
		t.Errorf("TypeName = %q, want %q", got, want)
	}
}

func TestOrganizationResourceSchema(t *testing.T) {
	t.Parallel()

	s := organizationResourceSchema(t)

	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Errorf("invalid schema implementation: %v", err)
	}
	if !s.Attributes["key"].IsRequired() || !s.Attributes["name"].IsRequired() {
		t.Error("key and name must be required")
	}
	if !s.Attributes["id"].IsComputed() {
		t.Error("id must be computed")
	}
}

func TestOrganizationResourceConfigureNeedsCloud(t *testing.T) {
	t.Parallel()

	serverClient := client.New(client.Config{URL: "https://sonarqube.example.com", Product: client.ProductServer})

	r := &organizationResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: serverClient}, resp)

	assertDiagnosticsContain(t, resp.Diagnostics, "need SonarQube Cloud")
	if r.client != nil {
		t.Error("the resource kept a client that is not SonarQube Cloud")
	}
}

func TestOrganizationResourceConfigureRejectsAnotherType(t *testing.T) {
	t.Parallel()

	r := &organizationResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: 42}, resp)

	assertDiagnosticsContain(t, resp.Diagnostics, "Unexpected provider data")
}

func TestOrganizationResourceConfigureWithoutProvider(t *testing.T) {
	t.Parallel()

	r := &organizationResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{}, resp)

	if resp.Diagnostics.HasError() {
		t.Errorf("unexpected diagnostics: %v", resp.Diagnostics)
	}
}

// The key of an organization follows the rule of the server, so a key that
// breaks it must fail during the plan rather than during the apply.
func TestOrganizationKeyPattern(t *testing.T) {
	t.Parallel()

	valid := []string{"a", "my-org", "org-1", "1-2-3"}
	invalid := []string{"-leading", "trailing-", "Upper", "under_score", "with space", "dot.dot"}

	for _, key := range valid {
		if !organizationKeyPattern.MatchString(key) {
			t.Errorf("key %q was refused, want accepted", key)
		}
	}
	for _, key := range invalid {
		if organizationKeyPattern.MatchString(key) {
			t.Errorf("key %q was accepted, want refused", key)
		}
	}
}

// The id must follow the planned key, not the key in the state. Otherwise a
// rename plans the old key and the apply contradicts the plan.
func TestKeyPlanModifier(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := organizationResourceSchema(t)

	req := planmodifier.StringRequest{
		Path:       path.Root("id"),
		Plan:       tfsdk.Plan{Schema: s, Raw: organizationValue(t, s, map[string]string{"key": "the-new-key"})},
		PlanValue:  types.StringValue("the-old-key"),
		StateValue: types.StringValue("the-old-key"),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}

	keyPlanModifier{}.PlanModifyString(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if got, want := resp.PlanValue.ValueString(), "the-new-key"; got != want {
		t.Errorf("planned id = %q, want %q", got, want)
	}
}

// The API leaves out a field that holds no value, so an attribute that was
// null must stay null. An organization without a description would otherwise
// report a difference on every plan.
func TestOptionalString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		apiValue string
		prior    types.String
		want     types.String
	}{
		{"value from the API", "a value", types.StringNull(), types.StringValue("a value")},
		{"nothing, and nothing before", "", types.StringNull(), types.StringNull()},
		{"nothing, and unknown before", "", types.StringUnknown(), types.StringNull()},
		{"nothing, and empty before", "", types.StringValue(""), types.StringValue("")},
		{"nothing, and a value before", "", types.StringValue("gone"), types.StringNull()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := optionalString(tc.apiValue, tc.prior); !got.Equal(tc.want) {
				t.Errorf("optionalString(%q, %v) = %v, want %v", tc.apiValue, tc.prior, got, tc.want)
			}
		})
	}
}

// A create leaves out an attribute with no value; an update sends it as empty
// to clear the field.
func TestValueHelpers(t *testing.T) {
	t.Parallel()

	if got := valueIfSet(types.StringNull()); got != nil {
		t.Errorf("valueIfSet(null) = %q, want nil", *got)
	}
	if got := valueIfSet(types.StringValue("a value")); got == nil || *got != "a value" {
		t.Errorf("valueIfSet(%q) did not carry the value", "a value")
	}
	if got := valueOrEmpty(types.StringNull()); got == nil || *got != "" {
		t.Error("valueOrEmpty(null) must give an empty value, so the field is cleared")
	}
	if got := valueOrEmpty(types.StringValue("a value")); got == nil || *got != "a value" {
		t.Errorf("valueOrEmpty(%q) did not carry the value", "a value")
	}
}

func organizationResourceSchema(t *testing.T) schema.Schema {
	t.Helper()

	resp := &resource.SchemaResponse{}
	NewOrganizationResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}
