package cloud

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

const (
	relationsPath     = "/enterprises/enterprise-organizations"
	otherEnterpriseID = "00000000-0000-4000-8000-000000000020"
	ofOrganization    = "GET " + relationsPath + "?organizationId=org-legacy-id"
	ofEnterprise      = "GET " + relationsPath + "?enterpriseId=" + theEnterpriseID
)

// fakeAttachments answers by "METHOD path?query", or by "METHOD path" when no
// answer has the query. A missing key gives 404.
type fakeAttachments struct {
	answers map[string]func(http.ResponseWriter, []byte)
	calls   []string
	mu      sync.Mutex
}

func newFakeAttachments(t *testing.T) (*fakeAttachments, *enterpriseOrganizationResource) {
	t.Helper()
	f := &fakeAttachments{answers: map[string]func(http.ResponseWriter, []byte){
		"GET /organizations/organizations":              answer(200, `[{"id":"org-legacy-id","uuidV4":"`+providertest.FakeOrganizationUUID+`","key":"my-org"}]`),
		"GET " + enterprisePath + "/" + theEnterpriseID: answer(200, enterpriseJSON("My enterprise", "")),
		"GET " + enterprisePath:                         answer(200, "["+enterpriseJSON("My enterprise", "")+"]"),
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		call := r.Method + " " + r.URL.Path
		if r.URL.RawQuery != "" {
			if _, ok := f.answers[call+"?"+r.URL.RawQuery]; ok {
				call += "?" + r.URL.RawQuery
			}
		}
		f.calls = append(f.calls, call)
		a, ok := f.answers[call]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"not found"}`)
			return
		}
		a(w, nil)
	}))
	t.Cleanup(srv.Close)
	return f, &enterpriseOrganizationResource{client: providertest.NewCloudClient(srv)}
}

func relationJSON(id, enterpriseID string) string {
	return fmt.Sprintf(`{"enterpriseOrganizationId":%q,"enterpriseId":%q,"organizationId":"org-legacy-id","organizationUuidV4":%q}`,
		id, enterpriseID, providertest.FakeOrganizationUUID)
}

func attachmentSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	NewEnterpriseOrganizationResource().Schema(t.Context(), resource.SchemaRequest{}, &resp)
	return resp.Schema
}

func attachmentValues(id string) map[string]string {
	values := map[string]string{"enterprise_id": theEnterpriseID, "organization": "my-org"}
	if id != "" {
		values["id"] = id
	}
	return values
}

func attachmentState(t *testing.T) tfsdk.State {
	t.Helper()
	return tfsdk.State{Schema: attachmentSchema(t), Raw: providertest.SchemaValue(t, attachmentSchema(t), attachmentValues("relation-id"))}
}

func createAttachment(t *testing.T, r *enterpriseOrganizationResource) resource.CreateResponse {
	t.Helper()
	s := attachmentSchema(t)
	resp := resource.CreateResponse{State: providertest.EmptyState(t, s)}
	plan := tfsdk.Plan{Schema: s, Raw: providertest.SchemaValue(t, s, attachmentValues(""))}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &resp)
	return resp
}

func TestEnterpriseOrganizationCreate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers map[string]func(http.ResponseWriter, []byte)
		posted  bool
	}{
		{"attach", map[string]func(http.ResponseWriter, []byte){
			ofOrganization: answer(200, `[]`), "POST " + relationsPath: answer(201, relationJSON("relation-id", theEnterpriseID)),
		}, true},
		{"adopt", map[string]func(http.ResponseWriter, []byte){
			ofOrganization: answer(200, "["+relationJSON("relation-id", theEnterpriseID)+"]"),
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newFakeAttachments(t)
			for call, a := range tc.answers {
				f.answers[call] = a
			}
			resp := createAttachment(t, r)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			if posted := slices.Contains(f.calls, "POST "+relationsPath); posted != tc.posted {
				t.Errorf("posted = %v, calls = %v", posted, f.calls)
			}
			got := providertest.ReadModel[enterpriseOrganizationModel](t, resp.State)
			if got.ID.ValueString() != "relation-id" {
				t.Errorf("state = %+v", got)
			}
		})
	}
}

func TestEnterpriseOrganizationCreateRecoversTheRelation(t *testing.T) {
	f, r := newFakeAttachments(t)
	f.answers[ofOrganization] = sequence(answer(200, `[]`), answer(200, "["+relationJSON("relation-id", theEnterpriseID)+"]"))
	f.answers["POST "+relationsPath] = answer(502, `{}`)
	resp := createAttachment(t, r)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := providertest.ReadModel[enterpriseOrganizationModel](t, resp.State); got.ID.ValueString() != "relation-id" {
		t.Errorf("state = %+v", got)
	}
}

func TestEnterpriseOrganizationCreateFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers map[string]func(http.ResponseWriter, []byte)
		want    string
	}{
		{"other enterprise", map[string]func(http.ResponseWriter, []byte){
			ofOrganization: answer(200, "["+relationJSON("relation-id", otherEnterpriseID)+"]"),
			"GET " + enterprisePath + "/" + otherEnterpriseID: answer(200, `{"id":"x","key":"other-enterprise"}`),
		}, "is in the enterprise other-enterprise"},
		{"other enterprise not readable", map[string]func(http.ResponseWriter, []byte){
			ofOrganization: answer(200, "["+relationJSON("relation-id", otherEnterpriseID)+"]"),
		}, "with the ID " + otherEnterpriseID},
		{"hidden enterprise", map[string]func(http.ResponseWriter, []byte){
			ofOrganization:          answer(200, `[]`),
			"POST " + relationsPath: answer(400, `{"message":"Organization is already part of an enterprise"}`),
		}, "already in an enterprise"},
		{"missing enterprise", map[string]func(http.ResponseWriter, []byte){
			"GET " + enterprisePath + "/" + theEnterpriseID: answer(404, `{}`),
		}, "Enterprise " + theEnterpriseID + " not found"},
		{"enterprise not readable", map[string]func(http.ResponseWriter, []byte){
			"GET " + enterprisePath + "/" + theEnterpriseID: answer(403, `{}`),
		}, "user token"},
		{"already attached, read fails", map[string]func(http.ResponseWriter, []byte){
			ofOrganization:          sequence(answer(200, `[]`), answer(500, `{"message":"boom"}`)),
			"POST " + relationsPath: answer(400, `{"message":"Organization is already part of an enterprise"}`),
		}, "boom"},
		{"refused", map[string]func(http.ResponseWriter, []byte){
			ofOrganization:          answer(200, `[]`),
			"POST " + relationsPath: answer(403, `{"message":"Role 'admin' required"}`),
		}, "user token"},
		{"lost answer", map[string]func(http.ResponseWriter, []byte){
			ofOrganization:          answer(200, `[]`),
			"POST " + relationsPath: answer(502, `{}`),
		}, "possibly attached"},
		{"list fails", map[string]func(http.ResponseWriter, []byte){ofOrganization: answer(500, `{}`)}, "Cannot read the enterprise of the organization"},
		{"missing organization", map[string]func(http.ResponseWriter, []byte){"GET /organizations/organizations": answer(404, `{}`)}, "not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newFakeAttachments(t)
			for call, a := range tc.answers {
				f.answers[call] = a
			}
			resp := createAttachment(t, r)
			providertest.AssertDiagnosticsContain(t, resp.Diagnostics, tc.want)
		})
	}
}

func TestEnterpriseOrganizationRead(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers map[string]func(http.ResponseWriter, []byte)
		removed bool
		want    string
	}{
		{"attached", map[string]func(http.ResponseWriter, []byte){
			ofOrganization: answer(200, "["+relationJSON("new-relation-id", theEnterpriseID)+"]"),
		}, false, ""},
		{"detached", map[string]func(http.ResponseWriter, []byte){ofOrganization: answer(200, `[]`)}, true, ""},
		{"moved", map[string]func(http.ResponseWriter, []byte){
			ofOrganization: answer(200, "["+relationJSON("relation-id", otherEnterpriseID)+"]"),
		}, true, ""},
		{"enterprise gone", map[string]func(http.ResponseWriter, []byte){
			ofOrganization: answer(200, `[]`), "GET " + enterprisePath + "/" + theEnterpriseID: answer(404, `{}`),
		}, true, ""},
		{"organization gone", map[string]func(http.ResponseWriter, []byte){"GET /organizations/organizations": answer(404, `{}`)}, true, ""},
		{"no access", map[string]func(http.ResponseWriter, []byte){
			ofOrganization: answer(200, `[]`), "GET " + enterprisePath + "/" + theEnterpriseID: answer(403, `{}`),
		}, false, "user token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newFakeAttachments(t)
			for call, a := range tc.answers {
				f.answers[call] = a
			}
			state := attachmentState(t)
			resp := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			if tc.want != "" {
				providertest.AssertDiagnosticsContain(t, resp.Diagnostics, tc.want)
				return
			}
			assertRefreshedAttachment(t, resp, tc.removed)
		})
	}
}

func TestEnterpriseOrganizationDelete(t *testing.T) {
	for _, tc := range []struct {
		name      string
		relations string
		detached  bool
		want      string
	}{
		{"detach", "[" + relationJSON("relation-id", theEnterpriseID) + "," + relationJSON("first-relation-id", theEnterpriseID) + "]", true, ""},
		{"last organization", "[" + relationJSON("relation-id", theEnterpriseID) + "]", false, "First remove this resource from the state"},
		{"already detached", "[" + relationJSON("first-relation-id", theEnterpriseID) + "]", false, ""},
		{"empty list", "[]", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newFakeAttachments(t)
			f.answers[ofEnterprise] = answer(200, tc.relations)
			f.answers["DELETE "+relationsPath+"/relation-id"] = answer(204, "")
			state := attachmentState(t)
			resp := resource.DeleteResponse{State: state}
			r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
			if tc.want != "" {
				providertest.AssertDiagnosticsContain(t, resp.Diagnostics, tc.want)
			} else if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			if detached := slices.Contains(f.calls, "DELETE "+relationsPath+"/relation-id"); detached != tc.detached {
				t.Errorf("detached = %v, calls = %v", detached, f.calls)
			}
		})
	}
}

func TestEnterpriseOrganizationDeleteWithoutAccess(t *testing.T) {
	f, r := newFakeAttachments(t)
	f.answers["GET "+enterprisePath+"/"+theEnterpriseID] = answer(403, `{}`)
	f.answers[ofEnterprise] = answer(200, `[]`)
	state := attachmentState(t)
	resp := resource.DeleteResponse{State: state}
	r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "user token")
}

func TestEnterpriseOrganizationAttachErrorHint(t *testing.T) {
	for _, tc := range []struct {
		status int
		hint   bool
	}{{403, true}, {401, true}, {404, false}, {429, false}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			f, r := newFakeAttachments(t)
			f.answers[ofOrganization] = answer(200, `[]`)
			f.answers["POST "+relationsPath] = answer(tc.status, `{"message":"refused"}`)
			resp := createAttachment(t, r)
			providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Cannot attach")
			if got := strings.Contains(fmt.Sprint(resp.Diagnostics), "user token"); got != tc.hint {
				t.Errorf("hint = %v, diagnostics = %v", got, resp.Diagnostics)
			}
		})
	}
}

// sequence gives the answers in order, and the last one after that.
func sequence(answers ...func(http.ResponseWriter, []byte)) func(http.ResponseWriter, []byte) {
	calls := 0
	return func(w http.ResponseWriter, body []byte) {
		a := answers[min(calls, len(answers)-1)]
		calls++
		a(w, body)
	}
}

func TestEnterpriseOrganizationDeleteFails(t *testing.T) {
	f, r := newFakeAttachments(t)
	f.answers[ofEnterprise] = answer(200, "["+relationJSON("relation-id", theEnterpriseID)+","+relationJSON("first-relation-id", theEnterpriseID)+"]")
	f.answers["DELETE "+relationsPath+"/relation-id"] = answer(403, `{}`)
	state := attachmentState(t)
	resp := resource.DeleteResponse{State: state}
	r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "Cannot detach the organization my-org")
}

func TestEnterpriseOrganizationImport(t *testing.T) {
	for _, tc := range []struct {
		name      string
		id        string
		relations string
		want      string
	}{
		{"found", "my-enterprise/my-org", "[" + relationJSON("relation-id", theEnterpriseID) + "]", ""},
		{"not attached", "my-enterprise/my-org", "[]", "is not in the enterprise"},
		{"other enterprise", "my-enterprise/my-org", "[" + relationJSON("relation-id", otherEnterpriseID) + "]", "is not in the enterprise"},
		{"no slash", "my-enterprise", "[]", "Invalid import ID"},
		{"empty part", "/my-org", "[]", "Invalid import ID"},
		{"two slashes", "a/b/c", "[]", "Invalid import ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newFakeAttachments(t)
			f.answers[ofOrganization] = answer(200, tc.relations)
			s := attachmentSchema(t)
			resp := resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
			resp.State.Raw = providertest.SchemaValue(t, s, nil)
			r.ImportState(t.Context(), resource.ImportStateRequest{ID: tc.id}, &resp)
			if tc.want != "" {
				providertest.AssertDiagnosticsContain(t, resp.Diagnostics, tc.want)
				return
			}
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			got := providertest.ReadModel[enterpriseOrganizationModel](t, resp.State)
			if got.ID.ValueString() != "relation-id" || got.EnterpriseID.ValueString() != theEnterpriseID || got.Organization.ValueString() != "my-org" {
				t.Errorf("state = %+v", got)
			}
		})
	}
}

func assertRefreshedAttachment(t *testing.T, resp resource.ReadResponse, removed bool) {
	t.Helper()
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if resp.State.Raw.IsNull() != removed {
		t.Fatalf("removed = %v, want %v", resp.State.Raw.IsNull(), removed)
	}
	if !removed {
		if got := providertest.ReadModel[enterpriseOrganizationModel](t, resp.State); got.ID.ValueString() != "new-relation-id" {
			t.Errorf("state = %+v", got)
		}
	}
}

// Two parallel destroys of the last two organizations detach only one.
func TestEnterpriseOrganizationParallelDeleteKeepsLastOrganization(t *testing.T) {
	f, r := newFakeAttachments(t)
	attached := map[string]bool{"relation-id": true, "first-relation-id": true}
	f.answers[ofEnterprise] = func(w http.ResponseWriter, _ []byte) {
		items := []string{}
		for id := range attached {
			items = append(items, relationJSON(id, theEnterpriseID))
		}
		fmt.Fprint(w, "["+strings.Join(items, ",")+"]")
	}
	for id := range attached {
		f.answers["DELETE "+relationsPath+"/"+id] = func(w http.ResponseWriter, _ []byte) {
			delete(attached, id)
			w.WriteHeader(http.StatusNoContent)
		}
	}
	s := attachmentSchema(t)
	var wg sync.WaitGroup
	results := make([]resource.DeleteResponse, 2)
	for i, id := range []string{"relation-id", "first-relation-id"} {
		values := attachmentValues(id)
		state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, values)}
		results[i] = resource.DeleteResponse{State: state}
		wg.Go(func() { r.Delete(t.Context(), resource.DeleteRequest{State: state}, &results[i]) })
	}
	wg.Wait()
	failed := 0
	for _, resp := range results {
		if resp.Diagnostics.HasError() {
			failed++
		}
	}
	if failed != 1 || len(attached) != 1 {
		t.Errorf("failed = %d, attached = %v", failed, attached)
	}
}

func TestEnterpriseOrganizationReadWarnsWhenEnterpriseNotFound(t *testing.T) {
	f, r := newFakeAttachments(t)
	f.answers[ofOrganization] = answer(200, `[]`)
	f.answers["GET "+enterprisePath+"/"+theEnterpriseID] = answer(404, `{}`)
	state := attachmentState(t)
	resp := resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
	assertRefreshedAttachment(t, resp, true)
	assertWarning(t, resp.Diagnostics, "cannot read it any more")
}

// A 404 for the enterprise is possibly a token without access, so the
// relation of the organization decides.
func TestEnterpriseOrganizationDeleteWhenEnterpriseNotFound(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers map[string]func(http.ResponseWriter, []byte)
		want    string
		warning string
	}{
		{"still attached", map[string]func(http.ResponseWriter, []byte){
			ofOrganization: answer(200, "["+relationJSON("relation-id", theEnterpriseID)+"]"),
		}, "still in the enterprise", ""},
		{"no relation", map[string]func(http.ResponseWriter, []byte){ofOrganization: answer(200, `[]`)}, "", "does not detach"},
		{"other enterprise", map[string]func(http.ResponseWriter, []byte){
			ofOrganization: answer(200, "["+relationJSON("relation-id", otherEnterpriseID)+"]"),
		}, "", "does not detach"},
		{"organization not found", map[string]func(http.ResponseWriter, []byte){
			"GET /organizations/organizations": answer(404, `{}`),
		}, "", "nothing to detach"},
		{"relation read fails", map[string]func(http.ResponseWriter, []byte){ofOrganization: answer(500, `{}`)},
			"Cannot read the enterprise of the organization", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newFakeAttachments(t)
			f.answers["GET "+enterprisePath+"/"+theEnterpriseID] = answer(404, `{}`)
			f.answers["DELETE "+relationsPath+"/relation-id"] = answer(204, "")
			for call, a := range tc.answers {
				f.answers[call] = a
			}
			state := attachmentState(t)
			resp := resource.DeleteResponse{State: state}
			r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
			if tc.want != "" {
				providertest.AssertDiagnosticsContain(t, resp.Diagnostics, tc.want)
			} else {
				assertWarning(t, resp.Diagnostics, tc.warning)
			}
			if slices.Contains(f.calls, "DELETE "+relationsPath+"/relation-id") {
				t.Errorf("detached, calls = %v", f.calls)
			}
		})
	}
}

func assertWarning(t *testing.T, diagnostics diag.Diagnostics, want string) {
	t.Helper()
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	for _, d := range diagnostics.Warnings() {
		if strings.Contains(d.Detail(), want) {
			return
		}
	}
	t.Errorf("no warning mentions %q, got %v", want, diagnostics)
}
