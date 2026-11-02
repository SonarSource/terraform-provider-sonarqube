package cloud

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

const (
	theEnterpriseID = "00000000-0000-4000-8000-000000000010"
	enterprisePath  = "/enterprises/enterprises"
)

// fakeEnterprises answers the organization lookup and the enterprise API.
// Each answer is keyed by "METHOD path". A missing key gives 404.
type fakeEnterprises struct {
	answers map[string]func(w http.ResponseWriter, body []byte)
	calls   []string
	bodies  map[string][]byte
}

func newFakeEnterprises(t *testing.T) (*fakeEnterprises, *enterpriseResource) {
	t.Helper()
	f := &fakeEnterprises{answers: map[string]func(http.ResponseWriter, []byte){
		"GET /organizations/organizations": func(w http.ResponseWriter, _ []byte) {
			fmt.Fprint(w, `[{"id":"org-legacy-id","uuidV4":"`+providertest.FakeOrganizationUUID+`","key":"my-org"}]`)
		},
	}, bodies: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := r.Method + " " + r.URL.Path
		f.calls = append(f.calls, call)
		body, _ := io.ReadAll(r.Body)
		f.bodies[call] = body
		answer, ok := f.answers[call]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"not found"}`)
			return
		}
		answer(w, body)
	}))
	t.Cleanup(srv.Close)
	return f, &enterpriseResource{client: providertest.NewCloudClient(srv)}
}

func answer(status int, body string) func(http.ResponseWriter, []byte) {
	return func(w http.ResponseWriter, _ []byte) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}
}

func enterpriseJSON(name, avatar string) string {
	return fmt.Sprintf(`{"id":%q,"key":"my-enterprise","name":%q,"avatar":%q}`, theEnterpriseID, name, avatar)
}

func enterpriseSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	NewEnterpriseResource().Schema(t.Context(), resource.SchemaRequest{}, &resp)
	return resp.Schema
}

func enterpriseValues(overrides map[string]string) map[string]string {
	values := map[string]string{"key": "my-enterprise", "name": "My enterprise", "avatar": "", "license": "test-license", "organization": "my-org"}
	for k, v := range overrides {
		values[k] = v
	}
	return values
}

func enterpriseState(t *testing.T, values map[string]string) tfsdk.State {
	t.Helper()
	return tfsdk.State{Schema: enterpriseSchema(t), Raw: providertest.SchemaValue(t, enterpriseSchema(t), values)}
}

func enterprisePlan(t *testing.T, values map[string]string) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan{Schema: enterpriseSchema(t), Raw: providertest.SchemaValue(t, enterpriseSchema(t), values)}
}

func TestEnterpriseCreate(t *testing.T) {
	f, r := newFakeEnterprises(t)
	f.answers["POST "+enterprisePath] = answer(201, enterpriseJSON("My enterprise", ""))
	resp := resource.CreateResponse{State: providertest.EmptyState(t, enterpriseSchema(t))}
	r.Create(t.Context(), resource.CreateRequest{Plan: enterprisePlan(t, enterpriseValues(nil))}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	providertest.AssertCallOrder(t, f.calls, "GET "+enterprisePath, "POST "+enterprisePath)
	var body map[string]any
	if err := json.Unmarshal(f.bodies["POST "+enterprisePath], &body); err != nil {
		t.Fatal(err)
	}
	if body["organizationId"] != "org-legacy-id" || body["license"] != "test-license" || body["avatar"] != "" {
		t.Errorf("body = %v", body)
	}
	got := providertest.ReadModel[enterpriseModel](t, resp.State)
	if got.ID.ValueString() != theEnterpriseID || got.License.ValueString() != "test-license" || got.Avatar.ValueString() != "" {
		t.Errorf("state = %+v", got)
	}
}

func TestEnterpriseCreateFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers map[string]func(http.ResponseWriter, []byte)
		want    string
		posted  bool
	}{
		{"used key", map[string]func(http.ResponseWriter, []byte){"GET " + enterprisePath: answer(200, "["+enterpriseJSON("x", "")+"]")}, "already used", false},
		{"key check fails", map[string]func(http.ResponseWriter, []byte){"GET " + enterprisePath: answer(403, `{}`)}, "Cannot check the enterprise key", false},
		{"refused key", map[string]func(http.ResponseWriter, []byte){"POST " + enterprisePath: answer(400, `{"message":"Enterprise key is not valid"}`)}, "was refused", true},
		{"invalid license", map[string]func(http.ResponseWriter, []byte){"POST " + enterprisePath: answer(400, `{"message":"Invalid license"}`)}, "Invalid license", true},
		{"no id", map[string]func(http.ResponseWriter, []byte){"POST " + enterprisePath: answer(201, `{"key":"my-enterprise"}`)}, "possibly created", true},
		{"server failure", map[string]func(http.ResponseWriter, []byte){"POST " + enterprisePath: answer(502, `{}`)}, "possibly created", true},
		{"missing organization", map[string]func(http.ResponseWriter, []byte){"GET /organizations/organizations": answer(404, `{}`)}, "not found", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newFakeEnterprises(t)
			for call, a := range tc.answers {
				f.answers[call] = a
			}
			resp := resource.CreateResponse{State: providertest.EmptyState(t, enterpriseSchema(t))}
			r.Create(t.Context(), resource.CreateRequest{Plan: enterprisePlan(t, enterpriseValues(nil))}, &resp)
			providertest.AssertDiagnosticsContain(t, resp.Diagnostics, tc.want)
			if tc.want != "possibly created" && strings.Contains(fmt.Sprint(resp.Diagnostics), "possibly created") {
				t.Errorf("a refused create went to the recovery path: %v", resp.Diagnostics)
			}
			if _, posted := f.bodies["POST "+enterprisePath]; posted != tc.posted {
				t.Errorf("posted = %v, want %v", posted, tc.posted)
			}
			if !resp.State.Raw.IsNull() {
				t.Error("a failed create wrote the state")
			}
		})
	}
}

// A lost answer after the server created the enterprise must not lose the
// enterprise, because its license is used up.
func TestEnterpriseCreateRecoversTheID(t *testing.T) {
	f, r := newFakeEnterprises(t)
	created := false
	f.answers["POST "+enterprisePath] = func(w http.ResponseWriter, _ []byte) {
		created = true
		w.WriteHeader(http.StatusBadGateway)
	}
	f.answers["GET "+enterprisePath] = func(w http.ResponseWriter, _ []byte) {
		if !created {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, "["+enterpriseJSON("My enterprise", "")+"]")
	}
	resp := resource.CreateResponse{State: providertest.EmptyState(t, enterpriseSchema(t))}
	r.Create(t.Context(), resource.CreateRequest{Plan: enterprisePlan(t, enterpriseValues(nil))}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := providertest.ReadModel[enterpriseModel](t, resp.State); got.ID.ValueString() != theEnterpriseID {
		t.Errorf("state = %+v", got)
	}
}

func TestEnterpriseRead(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status         int
		body           string
		removed, fails bool
		wantAvatar     string
	}{
		{"refresh", 200, enterpriseJSON("Renamed", "https://example.com/a.png"), false, false, "https://example.com/a.png"},
		{"no avatar field", 200, `{"id":"` + theEnterpriseID + `","key":"my-enterprise","name":"Renamed"}`, false, false, ""},
		{"gone", 404, `{}`, true, false, ""},
		{"forbidden", 403, `{}`, false, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newFakeEnterprises(t)
			f.answers["GET "+enterprisePath+"/"+theEnterpriseID] = answer(tc.status, tc.body)
			state := enterpriseState(t, enterpriseValues(map[string]string{"id": theEnterpriseID, "avatar": "old"}))
			resp := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() != tc.fails || resp.State.Raw.IsNull() != tc.removed {
				t.Fatalf("diagnostics = %v, removed = %v", resp.Diagnostics, resp.State.Raw.IsNull())
			}
			if tc.removed && resp.Diagnostics.WarningsCount() != 1 {
				t.Errorf("a removal must warn, got %v", resp.Diagnostics)
			}
			switch {
			case tc.fails:
				providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Scoped organization tokens are not supported")
			case !tc.removed:
				assertRefreshedEnterprise(t, resp.State, tc.wantAvatar)
			}
		})
	}
}

func assertRefreshedEnterprise(t *testing.T, state tfsdk.State, wantAvatar string) {
	t.Helper()
	got := providertest.ReadModel[enterpriseModel](t, state)
	if got.Name.ValueString() != "Renamed" || got.Avatar.ValueString() != wantAvatar || got.License.ValueString() != "test-license" {
		t.Errorf("state = %+v", got)
	}
}

func TestEnterpriseUpdate(t *testing.T) {
	f, r := newFakeEnterprises(t)
	f.answers["PATCH "+enterprisePath+"/"+theEnterpriseID] = answer(200, enterpriseJSON("New name", "https://example.com/b.png"))
	state := enterpriseState(t, enterpriseValues(map[string]string{"id": theEnterpriseID}))
	plan := enterprisePlan(t, enterpriseValues(map[string]string{"id": theEnterpriseID, "name": "New name", "avatar": "https://example.com/b.png"}))
	resp := resource.UpdateResponse{State: state}
	r.Update(t.Context(), resource.UpdateRequest{State: state, Plan: plan}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := string(f.bodies["PATCH "+enterprisePath+"/"+theEnterpriseID]); got != `{"name":"New name","avatar":"https://example.com/b.png"}` {
		t.Errorf("body = %s", got)
	}
	if got := providertest.ReadModel[enterpriseModel](t, resp.State); got.Name.ValueString() != "New name" {
		t.Errorf("state = %+v", got)
	}
}

func TestEnterpriseUpdateRefusesCreateOnlyChange(t *testing.T) {
	f, r := newFakeEnterprises(t)
	state := enterpriseState(t, enterpriseValues(map[string]string{"id": theEnterpriseID}))
	plan := enterprisePlan(t, enterpriseValues(map[string]string{"id": theEnterpriseID, "organization": "other-org"}))
	resp := resource.UpdateResponse{State: state}
	r.Update(t.Context(), resource.UpdateRequest{State: state, Plan: plan}, &resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "organization of an enterprise cannot change")
	if len(f.calls) != 0 {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestEnterpriseUpdateAfterImportKeepsConfiguration(t *testing.T) {
	f, r := newFakeEnterprises(t)
	f.answers["PATCH "+enterprisePath+"/"+theEnterpriseID] = answer(200, enterpriseJSON("My enterprise", ""))
	state := enterpriseState(t, map[string]string{"id": theEnterpriseID, "key": "my-enterprise", "name": "My enterprise", "avatar": ""})
	plan := enterprisePlan(t, enterpriseValues(map[string]string{"id": theEnterpriseID}))
	resp := resource.UpdateResponse{State: state}
	r.Update(t.Context(), resource.UpdateRequest{State: state, Plan: plan}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := providertest.ReadModel[enterpriseModel](t, resp.State); got.License.ValueString() != "test-license" || got.Organization.ValueString() != "my-org" {
		t.Errorf("state = %+v", got)
	}
}

func TestEnterpriseDelete(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		fails  bool
	}{{"deleted", 204, false}, {"gone", 404, false}, {"forbidden", 403, true}} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newFakeEnterprises(t)
			f.answers["DELETE "+enterprisePath+"/"+theEnterpriseID] = answer(tc.status, "")
			state := enterpriseState(t, enterpriseValues(map[string]string{"id": theEnterpriseID}))
			resp := resource.DeleteResponse{State: state}
			r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() != tc.fails {
				t.Fatal(resp.Diagnostics)
			}
			if tc.fails {
				providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Scoped organization tokens are not supported")
			}
		})
	}
}

func TestEnterpriseImport(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"found", 200, "[" + enterpriseJSON("My enterprise", "") + "]", ""},
		{"missing", 404, `{}`, "not found"},
		{"forbidden", 403, `{}`, "Cannot read the enterprise"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newFakeEnterprises(t)
			f.answers["GET "+enterprisePath] = answer(tc.status, tc.body)
			resp := resource.ImportStateResponse{State: providertest.EmptyState(t, enterpriseSchema(t))}
			resp.State.Raw = providertest.SchemaValue(t, enterpriseSchema(t), nil)
			r.ImportState(t.Context(), resource.ImportStateRequest{ID: "my-enterprise"}, &resp)
			if tc.want != "" {
				providertest.AssertDiagnosticsContain(t, resp.Diagnostics, tc.want)
				return
			}
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			got := providertest.ReadModel[enterpriseModel](t, resp.State)
			if got.ID.ValueString() != theEnterpriseID || got.Key.ValueString() != "my-enterprise" || !got.License.IsNull() {
				t.Errorf("state = %+v", got)
			}
		})
	}
}

func TestEnterpriseCreateOnlyPlanModifier(t *testing.T) {
	s := enterpriseSchema(t)
	existing := providertest.SchemaValue(t, s, enterpriseValues(map[string]string{"id": theEnterpriseID}))
	absent := tftypes.NewValue(s.Type().TerraformType(t.Context()), nil)
	for _, tc := range []struct {
		name         string
		state, plan  tftypes.Value
		prior, value types.String
		fails        bool
	}{
		{"change", existing, existing, types.StringValue("a"), types.StringValue("b"), true},
		{"same", existing, existing, types.StringValue("a"), types.StringValue("a"), false},
		{"after import", existing, existing, types.StringNull(), types.StringValue("b"), false},
		{"unknown", existing, existing, types.StringValue("a"), types.StringUnknown(), false},
		{"create", absent, existing, types.StringNull(), types.StringValue("b"), false},
		{"destroy", existing, absent, types.StringValue("a"), types.StringNull(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := planmodifier.StringRequest{Path: path.Root("license"), StateValue: tc.prior, PlanValue: tc.value,
				State: tfsdk.State{Schema: s, Raw: tc.state}, Plan: tfsdk.Plan{Schema: s, Raw: tc.plan}}
			var resp planmodifier.StringResponse
			enterpriseCreateOnlyPlanModifier{}.PlanModifyString(t.Context(), req, &resp)
			if resp.Diagnostics.HasError() != tc.fails {
				t.Fatalf("diagnostics = %v", resp.Diagnostics)
			}
		})
	}
}

func TestEnterpriseSchema(t *testing.T) {
	s := enterpriseSchema(t)
	if !s.Attributes["license"].IsSensitive() {
		t.Error("license is not sensitive")
	}
	for _, name := range []string{"key", "license", "organization"} {
		if len(s.Attributes[name].(schema.StringAttribute).PlanModifiers) != 1 {
			t.Errorf("%s has no create-only plan modifier", name)
		}
	}
}

func TestEnterpriseDataSourceRead(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"found", 200, "[" + enterpriseJSON("My enterprise", "https://example.com/a.png") + "]", ""},
		{"missing", 404, `{}`, "Scoped organization tokens are not supported"},
		{"forbidden", 403, `{}`, "Cannot read the enterprise"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newFakeEnterprises(t)
			f.answers["GET "+enterprisePath] = answer(tc.status, tc.body)
			d := &enterpriseDataSource{client: r.client}
			var schemaResp datasource.SchemaResponse
			d.Schema(t.Context(), datasource.SchemaRequest{}, &schemaResp)
			objectType := schemaResp.Schema.Type().TerraformType(t.Context())
			config := tftypes.NewValue(objectType, map[string]tftypes.Value{
				"key": tftypes.NewValue(tftypes.String, "my-enterprise"), "id": tftypes.NewValue(tftypes.String, nil),
				"name": tftypes.NewValue(tftypes.String, nil), "avatar": tftypes.NewValue(tftypes.String, nil),
			})
			resp := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(objectType, nil)}}
			d.Read(t.Context(), datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: config}}, &resp)
			if tc.want != "" {
				providertest.AssertDiagnosticsContain(t, resp.Diagnostics, tc.want)
				return
			}
			var got enterpriseDataSourceModel
			if diags := resp.State.Get(t.Context(), &got); diags.HasError() {
				t.Fatal(diags)
			}
			if got.ID.ValueString() != theEnterpriseID || got.Avatar.ValueString() != "https://example.com/a.png" {
				t.Errorf("state = %+v", got)
			}
		})
	}
}
