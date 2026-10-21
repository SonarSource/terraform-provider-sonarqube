package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func tokenSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	NewScopedOrganizationTokenResource().Schema(t.Context(), resource.SchemaRequest{}, &resp)
	return resp.Schema
}

func tokenSet(roles ...string) types.Set {
	values := make([]attr.Value, len(roles))
	for i, role := range roles {
		values[i] = types.StringValue(role)
	}
	return types.SetValueMust(types.StringType, values)
}

func TestTokenConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name         string
		org, project types.Set
		expires      types.String
		invalid      bool
	}{
		{"organization", tokenSet("scan"), types.SetNull(types.StringType), types.StringNull(), false},
		{"project", types.SetNull(types.StringType), tokenSet("scan"), types.StringNull(), false},
		{"both", tokenSet("provisioning"), tokenSet("scan"), types.StringNull(), false},
		{"missing provisioning", tokenSet("admin"), tokenSet("scan"), types.StringNull(), true},
		{"none", tokenSet(), tokenSet(), types.StringNull(), true},
		{"unknown", types.SetUnknown(types.StringType), tokenSet(), types.StringUnknown(), false},
		{"bad timestamp", tokenSet("scan"), tokenSet(), types.StringValue("tomorrow"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var diagnostics diag.Diagnostics
			validateTokenConfiguration(t.Context(), scopedOrganizationTokenModel{OrganizationPermissions: tc.org, ProjectPermissions: tc.project, ExpiresAt: tc.expires}, &diagnostics)
			if diagnostics.HasError() != tc.invalid {
				t.Fatalf("diagnostics=%v", diagnostics)
			}
		})
	}
}

func TestTokenExpiration(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		expiry  types.String
		expired bool
	}{
		{"absent", types.StringNull(), false}, {"unknown", types.StringUnknown(), false},
		{"future", types.StringValue("2026-01-02T00:00:00Z"), false},
		{"boundary", types.StringValue("2026-01-01T00:00:00Z"), true},
		{"past", types.StringValue("2025-12-31T00:00:00Z"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tokenExpired(tc.expiry, now) != tc.expired {
				t.Fatal("wrong expiration result")
			}
		})
	}
}

func TestTokenExpiredPlanReplacesSecret(t *testing.T) {
	s := tokenSchema(t)
	state := providertest.EmptyState(t, s)
	model := scopedOrganizationTokenModel{ID: types.StringValue(theBindingID), Token: types.StringValue("test-secret"), ExpiresAt: types.StringValue("2000-01-01T00:00:00Z"), OrganizationPermissions: tokenSet("scan"), ProjectPermissions: types.SetNull(types.StringType)}
	if diags := state.Set(t.Context(), &model); diags.HasError() {
		t.Fatal(diags)
	}
	plan := tfsdk.Plan{Schema: s, Raw: state.Raw}
	resp := resource.ModifyPlanResponse{Plan: plan}
	NewScopedOrganizationTokenResource().(resource.ResourceWithModifyPlan).ModifyPlan(t.Context(), resource.ModifyPlanRequest{State: state, Plan: plan}, &resp)
	if resp.Diagnostics.HasError() || len(resp.RequiresReplace) != 1 {
		t.Fatalf("response=%+v", resp)
	}
	var got scopedOrganizationTokenModel
	resp.Plan.Get(t.Context(), &got)
	if !got.ID.IsUnknown() || !got.Token.IsUnknown() || !got.CreatedAt.IsUnknown() {
		t.Fatal("replacement kept computed values")
	}
}

func TestTokenReadPreservesSecretAndHandlesAbsence(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		status                     int
		body                       string
		remove, hasError, imported bool
	}{
		{"refresh", 200, tokenListAnswer, false, false, false},
		{"import", 200, tokenListAnswer, false, false, true},
		{"absent", 200, `{"tokenDefinitions":[],"page":{"total":0}}`, true, false, false},
		{"forbidden", 403, `{}`, false, true, false},
		{"scope missing", 404, `{}`, false, true, false},
		{"malformed", 200, `{}`, false, true, false},
		{"missing total", 200, `{"tokenDefinitions":[],"page":{}}`, false, true, false},
		{"missing collection", 200, `{"page":{"total":0}}`, false, true, false},
		{"import unsupported grants", 200, tokenProjectListAnswer, false, true, true},
		{"refresh unsupported grants", 200, tokenProjectListAnswer, false, true, false},
		{"import supported grants", 200, strings.Replace(tokenProjectListAnswer, `"autoGrantProjectPermissions":false`, `"autoGrantProjectPermissions":true`, 1), false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := tokenTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			s := tokenSchema(t)
			state := providertest.EmptyState(t, s)
			secret := types.StringValue("test-secret")
			if tc.imported {
				secret = types.StringNull()
			}
			model := scopedOrganizationTokenModel{ID: types.StringValue(theBindingID), Organization: types.StringValue("my-org"), Token: secret, OrganizationPermissions: types.SetNull(types.StringType), ProjectPermissions: tokenSet()}
			if ds := state.Set(t.Context(), &model); ds.HasError() {
				t.Fatal(ds)
			}
			resp := resource.ReadResponse{State: state}
			r := &scopedOrganizationTokenResource{client: providertest.NewCloudClient(srv)}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() != tc.hasError || resp.State.Raw.IsNull() != tc.remove {
				t.Fatalf("diagnostics=%v, removed=%v", resp.Diagnostics, resp.State.Raw.IsNull())
			}
			if tc.hasError || tc.remove {
				assertTokenStateOnError(t, tc.hasError, state, resp.State)
				return
			}
			assertTokenReadState(t, resp.State, secret)
		})
	}
}

const tokenListAnswer = `{"tokenDefinitions":[{"id":"00000000-0000-4000-8000-000000000002","organizationId":"org","name":"test","createdAt":"2026-01-01T00:00:00Z","roleAssignments":[{"resourceType":"organization","roles":["scan"]}]}],"page":{"total":1}}`

func TestTokenImport(t *testing.T) {
	for _, tc := range []struct {
		id      string
		invalid bool
	}{
		{"my-org/" + theBindingID, false}, {theBindingID, true}, {"my-org/not-a-uuid", true}, {"/" + theBindingID, true}, {"my-org/" + theBindingID + "/extra", true},
	} {
		t.Run(tc.id, func(t *testing.T) {
			resp := resource.ImportStateResponse{State: providertest.EmptyState(t, tokenSchema(t))}
			NewScopedOrganizationTokenResource().(resource.ResourceWithImportState).ImportState(t.Context(), resource.ImportStateRequest{ID: tc.id}, &resp)
			if resp.Diagnostics.HasError() != tc.invalid {
				t.Fatal(resp.Diagnostics)
			}
		})
	}
}

func TestTokenSchema(t *testing.T) {
	s := tokenSchema(t)
	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !s.Attributes["token"].IsSensitive() {
		t.Fatal("token is not sensitive")
	}
	for _, name := range []string{"organization", "name", "description", "expires_at"} {
		if len(s.Attributes[name].(schema.StringAttribute).PlanModifiers) != 1 {
			t.Errorf("%s has no replacement modifier", name)
		}
	}
	for _, name := range []string{"organization_permissions", "project_permissions"} {
		if len(s.Attributes[name].(schema.SetAttribute).PlanModifiers) != 1 {
			t.Errorf("%s has no replacement modifier", name)
		}
	}
}

func TestTokenCreateScopes(t *testing.T) {
	for _, tc := range []struct {
		name         string
		org, project types.Set
		assignments  int
		autoGrant    bool
	}{
		{"organization", tokenSet("scan"), types.SetNull(types.StringType), 1, false},
		{"projects", types.SetNull(types.StringType), tokenSet("scan"), 1, false},
		{"provisioned projects", tokenSet("provisioning"), tokenSet("scan"), 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := tokenTestServer(t, tokenCreateHandler(t, tc.assignments, tc.autoGrant))
			s := tokenSchema(t)
			plan := tfsdk.Plan{Schema: s}
			model := scopedOrganizationTokenModel{Organization: types.StringValue("my-org"), Name: types.StringValue("test"), OrganizationPermissions: tc.org, ProjectPermissions: tc.project}
			if diagnostics := plan.Set(t.Context(), &model); diagnostics.HasError() {
				t.Fatal(diagnostics)
			}
			response := resource.CreateResponse{State: providertest.EmptyState(t, s)}
			r := &scopedOrganizationTokenResource{client: providertest.NewCloudClient(srv)}
			r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &response)
			if response.Diagnostics.HasError() {
				t.Fatal(response.Diagnostics)
			}
			var got scopedOrganizationTokenModel
			response.State.Get(t.Context(), &got)
			if got.Token.ValueString() != "sqco_test-secret" || got.ID.ValueString() != theBindingID {
				t.Fatal("missing creation outputs")
			}
		})
	}
}

func tokenTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/organizations/organizations" {
			fmt.Fprint(w, `[{"key":"my-org","uuidV4":"org"}]`)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func assertTokenReadState(t *testing.T, state tfsdk.State, secret types.String) {
	t.Helper()
	var got scopedOrganizationTokenModel
	if diagnostics := state.Get(t.Context(), &got); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	if !got.Token.Equal(secret) {
		t.Fatal("secret changed during read")
	}
	if got.ProjectPermissions.IsNull() {
		t.Fatal("explicit empty set became null")
	}
}

func tokenCreateHandler(t *testing.T, assignments int, autoGrant bool) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		var request client.OrganizationTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.OrganizationID != "org" || len(request.RoleAssignments) != assignments || request.AutoGrantProjectPermissions != autoGrant {
			t.Errorf("unexpected scope: %+v", request)
		}
		if request.Description != nil || request.ExpiresAt != nil {
			t.Error("omitted fields were sent")
		}
		fmt.Fprint(w, `{"id":"00000000-0000-4000-8000-000000000002","token":"sqco_test-secret","createdAt":"2026-01-01T00:00:00Z"}`)
	}
}

const tokenProjectListAnswer = `{"tokenDefinitions":[{"id":"00000000-0000-4000-8000-000000000002","organizationId":"org","name":"test","createdAt":"2026-01-01T00:00:00Z","autoGrantProjectPermissions":false,"roleAssignments":[{"resourceType":"organization","roles":["provisioning"]},{"resourceType":"project","roles":["scan"]}]}],"page":{"total":1}}`

func assertTokenStateOnError(t *testing.T, hasError bool, before, after tfsdk.State) {
	t.Helper()
	if hasError && !before.Raw.Equal(after.Raw) {
		t.Fatal("read error changed the prior state")
	}
}

func TestTokenCreationTime(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"nanoseconds", "2026-01-01T00:00:00.123456789Z", "2026-01-01T00:00:00.123456Z"},
		{"microseconds", "2026-01-01T00:00:00.123456Z", "2026-01-01T00:00:00.123456Z"},
		{"milliseconds", "2026-01-01T00:00:00.123Z", "2026-01-01T00:00:00.123Z"},
		{"seconds", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"},
		{"offset", "2026-01-01T01:00:00.123456789+01:00", "2026-01-01T00:00:00.123456Z"},
		{"missing", "", ""},
		{"invalid", "invalid-time", "invalid-time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tokenCreationTime(tc.input).ValueString(); got != tc.want {
				t.Errorf("creation time = %q, want %q", got, tc.want)
			}
		})
	}
}
