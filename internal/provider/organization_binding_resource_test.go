package provider

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

func TestOrganizationBindingResourceMetadata(t *testing.T) {
	t.Parallel()

	resp := &resource.MetadataResponse{}
	NewOrganizationBindingResource().Metadata(
		context.Background(),
		resource.MetadataRequest{ProviderTypeName: "sonarqube"},
		resp,
	)

	if got, want := resp.TypeName, "sonarqube_organization_binding"; got != want {
		t.Errorf("TypeName = %q, want %q", got, want)
	}
}

func TestOrganizationBindingResourceSchema(t *testing.T) {
	t.Parallel()

	s := organizationBindingResourceSchema(t)

	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Errorf("invalid schema implementation: %v", err)
	}
	if !s.Attributes["organization_key"].IsRequired() || !s.Attributes["installation_id"].IsRequired() {
		t.Error("organization_key and installation_id must be required")
	}
	if !s.Attributes["id"].IsComputed() {
		t.Error("id must be computed")
	}
	// The bind call takes no address, so an address in the configuration
	// would be dropped and the apply would contradict the plan.
	if s.Attributes["dev_ops_platform_url"].IsOptional() {
		t.Error("dev_ops_platform_url must be read only")
	}
}

func TestOrganizationBindingResourceConfigureNeedsCloud(t *testing.T) {
	t.Parallel()

	serverClient := client.New(client.Config{URL: "https://sonarqube.example.com", Product: client.ProductServer})

	r := &organizationBindingResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: serverClient}, resp)

	assertDiagnosticsContain(t, resp.Diagnostics, "need SonarQube Cloud")
	if r.client != nil {
		t.Error("the resource kept a client that is not SonarQube Cloud")
	}
}

func TestOrganizationBindingResourceConfigureWithoutProvider(t *testing.T) {
	t.Parallel()

	r := &organizationBindingResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{}, resp)

	if resp.Diagnostics.HasError() {
		t.Errorf("unexpected diagnostics: %v", resp.Diagnostics)
	}
}

// A changed installation must fail during the plan. A replacement cannot
// help, because the delete removes nothing and the organization stays bound.
func TestImmutableInstallationIDPlanModifier(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := organizationBindingResourceSchema(t)
	plan := tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
		"organization_key": "my-org", "installation_id": "222",
	})}
	state := tfsdk.State{Schema: s, Raw: bindingValue(t, s, map[string]any{
		"organization_key": "my-org", "installation_id": "111",
	})}

	changed := &planmodifier.StringResponse{}
	immutableInstallationIDPlanModifier{}.PlanModifyString(ctx, planmodifier.StringRequest{
		Path:       path.Root("installation_id"),
		Plan:       plan,
		State:      state,
		PlanValue:  types.StringValue("222"),
		StateValue: types.StringValue("111"),
	}, changed)

	if !changed.Diagnostics.HasError() {
		t.Error("a changed installation was accepted")
	}

	unchanged := &planmodifier.StringResponse{}
	immutableInstallationIDPlanModifier{}.PlanModifyString(ctx, planmodifier.StringRequest{
		Path:       path.Root("installation_id"),
		Plan:       plan,
		State:      state,
		PlanValue:  types.StringValue("111"),
		StateValue: types.StringValue("111"),
	}, unchanged)

	if unchanged.Diagnostics.HasError() {
		t.Errorf("an installation that stays the same was refused: %v", unchanged.Diagnostics)
	}

	// A different organization is a different binding. organization_key asks
	// for a replacement, the new organization is bound to nothing, and its
	// own installation is therefore not a change of this installation.
	moved := &planmodifier.StringResponse{}
	immutableInstallationIDPlanModifier{}.PlanModifyString(ctx, planmodifier.StringRequest{
		Path: path.Root("installation_id"),
		Plan: tfsdk.Plan{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"organization_key": "new-org", "installation_id": "222",
		})},
		State: tfsdk.State{Schema: s, Raw: bindingValue(t, s, map[string]any{
			"organization_key": "old-org", "installation_id": "111",
		})},
		PlanValue:  types.StringValue("222"),
		StateValue: types.StringValue("111"),
	}, moved)

	if moved.Diagnostics.HasError() {
		t.Errorf("a move to another organization was refused: %v", moved.Diagnostics.Errors())
	}

	// A create has no prior state, and that is not a change.
	created := &planmodifier.StringResponse{}
	immutableInstallationIDPlanModifier{}.PlanModifyString(ctx, planmodifier.StringRequest{
		Path:       path.Root("installation_id"),
		Plan:       plan,
		State:      state,
		PlanValue:  types.StringValue("111"),
		StateValue: types.StringNull(),
	}, created)

	if created.Diagnostics.HasError() {
		t.Errorf("a create was refused: %v", created.Diagnostics)
	}
}

// The hint must not depend on the words of the message. The caller reads the
// organization before it binds, so every 404 from the bind call points at the
// installation.
func TestInstallationHint(t *testing.T) {
	t.Parallel()

	unknownInstallation := &client.APIError{
		StatusCode: http.StatusNotFound,
		Messages:   []string{"The GitHub installation id 12345 had not been found"},
	}
	if got := installationHint(unknownInstallation); !strings.Contains(got, "dop_applications") {
		t.Errorf("the hint = %q, want it to name the data source that lists the applications", got)
	}

	// The server can change the words of the message. The hint must stay.
	reworded := &client.APIError{
		StatusCode: http.StatusNotFound,
		Messages:   []string{"no such installation"},
	}
	if got := installationHint(reworded); !strings.Contains(got, "dop_applications") {
		t.Errorf("the hint = %q, want the wording of the message not to matter", got)
	}

	// Another status means something else, and a hint about the installation
	// would mislead.
	refused := &client.APIError{StatusCode: http.StatusForbidden, Messages: []string{"insufficient privileges"}}
	if got := installationHint(refused); got != "" {
		t.Errorf("the hint = %q, want none for a status that is not 404", got)
	}

	if got := installationHint(errors.New("the connection failed")); got != "" {
		t.Errorf("the hint = %q, want none for an error that carries no status", got)
	}
}
