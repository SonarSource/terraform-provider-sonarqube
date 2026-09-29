package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

func dopApplicationsSchema(t *testing.T) schema.Schema {
	t.Helper()

	resp := &datasource.SchemaResponse{}
	NewDopApplicationsDataSource().Schema(context.Background(), datasource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// dopApplicationsConfig builds a configuration that names one platform, or
// none when the platform is empty. bindingValue already leaves every
// attribute the caller does not name as null, which is what an absent
// platform needs.
func dopApplicationsConfig(t *testing.T, s schema.Schema, devOpsPlatform string) tfsdk.Config {
	t.Helper()

	attributes := map[string]any{}
	if devOpsPlatform != "" {
		attributes["dev_ops_platform"] = devOpsPlatform
	}
	return tfsdk.Config{Schema: s, Raw: bindingValue(t, s, attributes)}
}

func TestDopApplicationsDataSourceMetadata(t *testing.T) {
	t.Parallel()

	resp := &datasource.MetadataResponse{}
	NewDopApplicationsDataSource().Metadata(
		context.Background(),
		datasource.MetadataRequest{ProviderTypeName: "sonarqube"},
		resp,
	)

	if got, want := resp.TypeName, "sonarqube_dop_applications"; got != want {
		t.Errorf("TypeName = %q, want %q", got, want)
	}
}

func TestDopApplicationsDataSourceSchema(t *testing.T) {
	t.Parallel()

	if err := dopApplicationsSchema(t).ValidateImplementation(context.Background()); err != nil {
		t.Errorf("invalid schema implementation: %v", err)
	}
}

func TestDopApplicationsDataSourceRead(t *testing.T) {
	t.Parallel()

	instance := newFakeApplicationsInstance(`{"devOpsPlatformApplications":[
		{"id":"github#sonarqube-cloud-dev11","devOpsPlatform":"github","applicationKey":"sonarqube-cloud-dev11","bindingType":"integration-dop"}
	]}`)
	d := &dopApplicationsDataSource{client: instance.start(t)}

	s := dopApplicationsSchema(t)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(context.Background(), datasource.ReadRequest{
		Config: dopApplicationsConfig(t, s, client.PlatformGitHub),
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if want := "devOpsPlatform=github"; instance.query != want {
		t.Errorf("query = %q, want %q", instance.query, want)
	}

	var state dopApplicationsModel
	if diags := resp.State.Get(context.Background(), &state); diags.HasError() {
		t.Fatalf("cannot read the state: %v", diags)
	}
	if len(state.Applications) != 1 {
		t.Fatalf("got %d applications, want 1", len(state.Applications))
	}
	if got, want := state.Applications[0].ApplicationKey.ValueString(), "sonarqube-cloud-dev11"; got != want {
		t.Errorf("application_key = %q, want %q", got, want)
	}
}

// An instance with no application gives an empty list, not a null one, so
// that a configuration can count the result without a test for null first.
func TestDopApplicationsDataSourceReadOfAnInstanceWithNoApplication(t *testing.T) {
	t.Parallel()

	d := &dopApplicationsDataSource{
		client: newFakeApplicationsInstance(`{"devOpsPlatformApplications":[]}`).start(t),
	}

	s := dopApplicationsSchema(t)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(context.Background(), datasource.ReadRequest{Config: dopApplicationsConfig(t, s, "")}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state dopApplicationsModel
	if diags := resp.State.Get(context.Background(), &state); diags.HasError() {
		t.Fatalf("cannot read the state: %v", diags)
	}
	if state.Applications == nil {
		t.Error("applications holds no value, want an empty list")
	}
	if len(state.Applications) != 0 {
		t.Errorf("got %d applications, want none", len(state.Applications))
	}
}

func TestDopApplicationsDataSourceReadReportsAFailure(t *testing.T) {
	t.Parallel()

	instance := newFakeApplicationsInstance("")
	instance.status = 500
	d := &dopApplicationsDataSource{client: instance.start(t)}

	s := dopApplicationsSchema(t)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(context.Background(), datasource.ReadRequest{
		Config: dopApplicationsConfig(t, s, client.PlatformGitHub),
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a failure of the API went unreported")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(got, "applications") {
		t.Errorf("summary = %q, want it to name the applications", got)
	}
}

// fakeApplicationsInstance answers one read of the applications, with the
// body and the status that the test asks for.
type fakeApplicationsInstance struct {
	answer string
	status int
	query  string
}

func newFakeApplicationsInstance(answer string) *fakeApplicationsInstance {
	return &fakeApplicationsInstance{answer: answer, status: http.StatusOK}
}

func (f *fakeApplicationsInstance) start(t *testing.T) *client.Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.query = r.URL.RawQuery
		w.WriteHeader(f.status)
		w.Write([]byte(f.answer))
	}))
	t.Cleanup(srv.Close)

	return newTestCloudClient(srv)
}
