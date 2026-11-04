package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

type organizationDefaultQualityProfileModel struct {
	ID                types.String `tfsdk:"id"`
	Organization      types.String `tfsdk:"organization"`
	Language          types.String `tfsdk:"language"`
	QualityProfileKey types.String `tfsdk:"quality_profile_key"`
}

// fakeDefaultProfileInstance holds the organization my-org, the java profiles
// java-a and java-b, and the py profile py-a.
type fakeDefaultProfileInstance struct {
	organizationExists bool
	defaults           map[string]string
	calls              []string
	// setStatus and setMessage make the set call fail when setStatus is not 0.
	setStatus  int
	setMessage string
}

var fakeProfileLanguages = map[string]string{"java-a": "java", "java-b": "java", "py-a": "py"}

func newFakeDefaultProfileInstance() *fakeDefaultProfileInstance {
	return &fakeDefaultProfileInstance{organizationExists: true, defaults: map[string]string{"java": "java-a", "py": "py-a"}}
}

func (f *fakeDefaultProfileInstance) start(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		f.calls = append(f.calls, r.Method+" "+r.URL.Path+" "+r.Form.Encode())
		if !f.organizationExists {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"msg":"No organization with key 'my-org'"}]}`))
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/qualityprofiles/search":
			f.search(t, w, r)
		case "POST /api/qualityprofiles/set_default":
			if f.setStatus != 0 {
				w.WriteHeader(f.setStatus)
				_, _ = w.Write([]byte(`{"errors":[{"msg":"` + f.setMessage + `"}]}`))
				return
			}
			key := r.Form.Get("key")
			f.defaults[fakeProfileLanguages[key]] = key
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return providertest.NewCloudClient(srv)
}

func (f *fakeDefaultProfileInstance) search(t *testing.T, w http.ResponseWriter, r *http.Request) {
	language, defaults := r.Form.Get("language"), r.Form.Get("defaults") == "true"
	profiles := []client.QualityProfile{}
	for key, profileLanguage := range fakeProfileLanguages {
		isDefault := f.defaults[profileLanguage] == key
		if (language != "" && language != profileLanguage) || (defaults && !isDefault) {
			continue
		}
		profiles = append(profiles, client.QualityProfile{Key: key, Name: "Profile " + key, Language: profileLanguage, IsDefault: isDefault})
	}
	if err := json.NewEncoder(w).Encode(map[string]any{"profiles": profiles}); err != nil {
		t.Error(err)
	}
}

func defaultProfileResourceSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewOrganizationDefaultQualityProfileResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func defaultProfilePlan(t *testing.T, s schema.Schema, language, key string) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"organization": "my-org", "language": language, "quality_profile_key": key,
	})}
}

func defaultProfileState(t *testing.T, s schema.Schema, key string) tfsdk.State {
	t.Helper()
	return tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "my-org/java", "organization": "my-org", "language": "java", "quality_profile_key": key,
	})}
}

func newDefaultProfileResource(t *testing.T, instance *fakeDefaultProfileInstance) *organizationDefaultResource {
	t.Helper()
	return &organizationDefaultResource{client: instance.start(t), organizationDefault: defaultQualityProfile}
}

func TestOrganizationDefaultQualityProfileResourceSchema(t *testing.T) {
	t.Parallel()

	s := defaultProfileResourceSchema(t)
	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Fatal(err)
	}
	requiresReplace := stringplanmodifier.RequiresReplace().Description(context.Background())
	for _, name := range []string{"organization", "language"} {
		a, ok := s.Attributes[name].(schema.StringAttribute)
		if !ok || !a.IsRequired() || len(a.PlanModifiers) != 1 ||
			a.PlanModifiers[0].Description(context.Background()) != requiresReplace {
			t.Errorf("%s must be required and require a replacement", name)
		}
	}
	// A different profile changes the default in place.
	if key, ok := s.Attributes["quality_profile_key"].(schema.StringAttribute); !ok || !key.IsRequired() || len(key.PlanModifiers) != 0 {
		t.Error("quality_profile_key must be required and must not require a replacement")
	}
	resp := &resource.MetadataResponse{}
	NewOrganizationDefaultQualityProfileResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "sonarqube"}, resp)
	if resp.TypeName != "sonarqube_cloud_organization_default_quality_profile" {
		t.Errorf("type name = %q", resp.TypeName)
	}
}

func TestOrganizationDefaultQualityProfileResourceLifecycle(t *testing.T) {
	t.Parallel()

	instance := newFakeDefaultProfileInstance()
	r := newDefaultProfileResource(t, instance)
	s := defaultProfileResourceSchema(t)

	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: defaultProfilePlan(t, s, "java", "java-b")}, created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	if instance.defaults["java"] != "java-b" || instance.defaults["py"] != "py-a" {
		t.Errorf("defaults = %v", instance.defaults)
	}
	model := providertest.ReadModel[organizationDefaultQualityProfileModel](t, created.State)
	if model.ID.ValueString() != "my-org/java" || model.Language.ValueString() != "java" ||
		model.QualityProfileKey.ValueString() != "java-b" {
		t.Errorf("state = %+v", model)
	}

	refreshed := &resource.ReadResponse{State: created.State}
	r.Read(context.Background(), resource.ReadRequest{State: created.State}, refreshed)
	if refreshed.Diagnostics.HasError() {
		t.Fatal(refreshed.Diagnostics)
	}
	if !created.State.Raw.Equal(refreshed.State.Raw) {
		t.Errorf("unchanged refresh changed state: before %v, after %v", created.State.Raw, refreshed.State.Raw)
	}

	updated := &resource.UpdateResponse{State: refreshed.State}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan: defaultProfilePlan(t, s, "java", "java-a"), State: refreshed.State,
	}, updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	if instance.defaults["java"] != "java-a" {
		t.Errorf("java default = %q, want java-a", instance.defaults["java"])
	}

	calls := len(instance.calls)
	deleted := &resource.DeleteResponse{}
	r.Delete(context.Background(), resource.DeleteRequest{State: updated.State}, deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	if warnings := deleted.Diagnostics.Warnings(); len(warnings) != 1 ||
		!strings.Contains(warnings[0].Detail(), "the language java of the organization my-org keeps") {
		t.Errorf("delete warnings = %v", warnings)
	}
	if len(instance.calls) != calls {
		t.Errorf("delete called the API: %v", instance.calls[calls:])
	}
}

// A change of the default in the UI shows in the state, so the plan shows it.
func TestOrganizationDefaultQualityProfileResourceReadReportsDrift(t *testing.T) {
	t.Parallel()

	instance := newFakeDefaultProfileInstance()
	r := newDefaultProfileResource(t, instance)
	s := defaultProfileResourceSchema(t)
	state := defaultProfileState(t, s, "java-b")

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := providertest.ReadModel[organizationDefaultQualityProfileModel](t, resp.State).QualityProfileKey.ValueString(); got != "java-a" {
		t.Errorf("quality_profile_key = %q, want java-a", got)
	}
	want := "GET /api/qualityprofiles/search defaults=true&language=java&organization=my-org"
	if len(instance.calls) != 1 || instance.calls[0] != want {
		t.Errorf("calls = %v, want %s", instance.calls, want)
	}
}

func TestOrganizationDefaultQualityProfileResourceReadOfAMissingOrganization(t *testing.T) {
	t.Parallel()

	instance := newFakeDefaultProfileInstance()
	instance.organizationExists = false
	r := newDefaultProfileResource(t, instance)
	s := defaultProfileResourceSchema(t)
	state := defaultProfileState(t, s, "java-a")

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if resp.Diagnostics.WarningsCount() != 1 || !resp.State.Raw.IsNull() {
		t.Errorf("warnings = %d, state = %v", resp.Diagnostics.WarningsCount(), resp.State.Raw)
	}
}

func TestOrganizationDefaultQualityProfileResourceReadRefusesNoDefault(t *testing.T) {
	t.Parallel()

	instance := newFakeDefaultProfileInstance()
	delete(instance.defaults, "java")
	r := newDefaultProfileResource(t, instance)
	s := defaultProfileResourceSchema(t)
	state := defaultProfileState(t, s, "java-a")

	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "The API gave no default quality profile for the language java")
	if !state.Raw.Equal(resp.State.Raw) {
		t.Errorf("a failed read changed the state: %v", resp.State.Raw)
	}
}

func TestOrganizationDefaultQualityProfileResourceCreateFailures(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		prepare func(*fakeDefaultProfileInstance)
		key     string
		want    string
	}{
		"missing organization": {
			prepare: func(f *fakeDefaultProfileInstance) { f.organizationExists = false },
			key:     "java-b", want: "Organization my-org not found",
		},
		"missing profile": {
			prepare: func(*fakeDefaultProfileInstance) { /* the fake has no java-c */ },
			key:     "java-c", want: "Quality profile java-c not found",
		},
		"profile of a different language": {
			prepare: func(*fakeDefaultProfileInstance) { /* py-a is a py profile */ },
			key:     "py-a", want: "is a profile of the language py, not of the language java",
		},
		"MISRA entitlement": {
			prepare: func(f *fakeDefaultProfileInstance) {
				f.setStatus, f.setMessage = http.StatusForbidden, "Enterprise MISRA rules require a MISRA entitlement"
			},
			key: "java-b", want: "does not have the MISRA entitlement",
		},
		"permission": {
			prepare: func(f *fakeDefaultProfileInstance) {
				f.setStatus, f.setMessage = http.StatusForbidden, "Insufficient privileges"
			},
			key: "java-b", want: "Administer Quality Profiles permission",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instance := newFakeDefaultProfileInstance()
			test.prepare(instance)
			r := newDefaultProfileResource(t, instance)
			s := defaultProfileResourceSchema(t)

			resp := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
			r.Create(context.Background(), resource.CreateRequest{Plan: defaultProfilePlan(t, s, "java", test.key)}, resp)
			providertest.AssertDiagnosticsContain(t, resp.Diagnostics, test.want)
			if !resp.State.Raw.IsNull() {
				t.Error("a failed create wrote the state")
			}
			if instance.defaults["py"] != "py-a" {
				t.Errorf("the py default changed to %q", instance.defaults["py"])
			}
		})
	}
}

func TestOrganizationDefaultQualityProfileResourceImportState(t *testing.T) {
	t.Parallel()

	s := defaultProfileResourceSchema(t)
	resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
	NewOrganizationDefaultQualityProfileResource().(*organizationDefaultResource).ImportState(
		context.Background(), resource.ImportStateRequest{ID: "my-org/java"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	model := providertest.ReadModel[organizationDefaultQualityProfileModel](t, resp.State)
	if model.Organization.ValueString() != "my-org" || model.Language.ValueString() != "java" || model.ID.ValueString() != "my-org/java" {
		t.Errorf("state = %+v", model)
	}
}

func TestOrganizationDefaultQualityProfileResourceImportRefusesBadIDs(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"my-org", "my-org/", "/java", "my-org/java/extra"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			s := defaultProfileResourceSchema(t)
			resp := &resource.ImportStateResponse{State: providertest.EmptyState(t, s)}
			NewOrganizationDefaultQualityProfileResource().(*organizationDefaultResource).ImportState(
				context.Background(), resource.ImportStateRequest{ID: id}, resp)
			providertest.AssertDiagnosticsContain(t, resp.Diagnostics, "<organization>/<language>")
		})
	}
}
