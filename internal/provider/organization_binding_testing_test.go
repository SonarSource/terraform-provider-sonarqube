package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// theBindingID is the identifier that the instance below gives to the one
// binding it makes.
const theBindingID = "0206a6e1-15dc-4481-888b-de0877286b27"

// schemaTyper is what bindingValue needs of a schema, and it is all that the
// resource schema and the data source schema have in common.
type schemaTyper interface {
	Type() attr.Type
}

func organizationBindingResourceSchema(t *testing.T) schema.Schema {
	t.Helper()

	resp := &resource.SchemaResponse{}
	NewOrganizationBindingResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// bindingValue builds a value of a schema that holds strings and booleans.
// Every attribute the caller leaves out is null, as it is for an attribute
// absent from the configuration.
//
// organizationValue cannot do this: it writes every attribute as a string,
// and this schema carries a boolean.
//
// The parameter is an interface, because the resource and the data source
// describe the same binding with two schema types of the framework.
func bindingValue(t *testing.T, s schemaTyper, attributes map[string]any) tftypes.Value {
	t.Helper()

	objectType, ok := s.Type().TerraformType(context.Background()).(tftypes.Object)
	if !ok {
		t.Fatalf("the schema is not an object type")
	}

	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attributeType := range objectType.AttributeTypes {
		given, present := attributes[name]
		if !present {
			values[name] = tftypes.NewValue(attributeType, nil)
			continue
		}
		values[name] = tftypes.NewValue(attributeType, given)
	}
	return tftypes.NewValue(objectType, values)
}

// readBindingModel reads the state back into the model of the binding.
func readBindingModel(t *testing.T, state tfsdk.State) organizationBindingModel {
	t.Helper()

	var model organizationBindingModel
	if diags := state.Get(context.Background(), &model); diags.HasError() {
		t.Fatalf("cannot read the state: %v", diags)
	}
	return model
}

// fakeBoundInstance answers the calls that a binding needs: the read of an
// organization through Web API v2, and the four operations of the bindings
// API.
type fakeBoundInstance struct {
	// organizations maps an organization key to its internal identifier.
	organizations map[string]string
	bindings      map[string]*client.OrganizationBinding
	// autoImportAllowed carries the feature that decides whether the server
	// accepts an automatic import of repositories. The server writes false
	// when it is off, whatever the request asks for.
	autoImportAllowed bool
	calls             []string
}

func newFakeBoundInstance() *fakeBoundInstance {
	return &fakeBoundInstance{
		organizations:     map[string]string{},
		bindings:          map[string]*client.OrganizationBinding{},
		autoImportAllowed: true,
	}
}

func (f *fakeBoundInstance) start(t *testing.T) *client.Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)

	return newTestCloudClient(srv)
}

func (f *fakeBoundInstance) notFound(w http.ResponseWriter, message string) {
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`{"message":"` + message + `"}`))
}

func (f *fakeBoundInstance) serve(w http.ResponseWriter, r *http.Request) {
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)

	if r.URL.Path == "/organizations/organizations" {
		f.serveOrganization(w, r)
		return
	}

	const collection = "/dop-translation/organization-bindings"
	switch {
	case r.URL.Path == collection && r.Method == http.MethodPost:
		f.serveCreate(w, r)
	case r.URL.Path == collection && r.Method == http.MethodGet:
		f.serveSearch(w, r)
	case strings.HasPrefix(r.URL.Path, collection+"/"):
		f.serveOne(w, r, strings.TrimPrefix(r.URL.Path, collection+"/"))
	default:
		f.notFound(w, "no such path")
	}
}

func (f *fakeBoundInstance) serveOrganization(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("organizationKey")
	id, found := f.organizations[key]
	if !found {
		f.notFound(w, "no such organization")
		return
	}
	json.NewEncoder(w).Encode([]client.Organization{{ID: id, Key: key, Name: key}})
}

func (f *fakeBoundInstance) serveCreate(w http.ResponseWriter, r *http.Request) {
	var req client.CreateBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	autoImport := f.autoImportAllowed && req.RepoAutoImportEnabled != nil && *req.RepoAutoImportEnabled
	binding := &client.OrganizationBinding{
		ID:                    theBindingID,
		OrganizationID:        req.OrganizationID,
		OrganizationUUIDV4:    "3ddd1f8f-2ab4-443f-a3be-a19ca418ca75",
		DevOpsPlatform:        req.DevOpsPlatform,
		BindingType:           "integration-dop",
		InstallationID:        req.InstallationID,
		DevOpsPlatformURL:     "https://github.com/my-github-org",
		RepoAutoImportEnabled: &autoImport,
	}
	f.bindings[binding.ID] = binding
	json.NewEncoder(w).Encode(binding)
}

func (f *fakeBoundInstance) serveSearch(w http.ResponseWriter, r *http.Request) {
	organizationID := r.URL.Query().Get("organizationId")

	found := []client.OrganizationBinding{}
	for _, binding := range f.bindings {
		if binding.OrganizationID == organizationID {
			found = append(found, *binding)
		}
	}
	json.NewEncoder(w).Encode(map[string]any{"organizationBindings": found})
}

func (f *fakeBoundInstance) serveOne(w http.ResponseWriter, r *http.Request, id string) {
	binding, found := f.bindings[id]
	if !found {
		f.notFound(w, "no such binding")
		return
	}

	if r.Method == http.MethodPatch {
		var req client.PatchBindingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.RepoAutoImportEnabled != nil {
			autoImport := f.autoImportAllowed && *req.RepoAutoImportEnabled
			binding.RepoAutoImportEnabled = &autoImport
		}
	}

	json.NewEncoder(w).Encode(binding)
}

// bindTo puts an organization and a binding to one platform into the
// instance. A binding to a platform that is not GitHub carries no
// installation, which is what dev11 reports for GitLab.
func (f *fakeBoundInstance) bindTo(organizationKey, organizationID, platform string) *client.OrganizationBinding {
	binding := f.bind(organizationKey, organizationID)
	binding.DevOpsPlatform = platform
	if platform != client.PlatformGitHub {
		binding.InstallationID = ""
	}
	return binding
}

// bind puts an organization and its binding into the instance, as they are
// after a create that Terraform already recorded.
func (f *fakeBoundInstance) bind(organizationKey, organizationID string) *client.OrganizationBinding {
	autoImport := false
	f.organizations[organizationKey] = organizationID
	binding := &client.OrganizationBinding{
		ID:                    theBindingID,
		OrganizationID:        organizationID,
		OrganizationUUIDV4:    "3ddd1f8f-2ab4-443f-a3be-a19ca418ca75",
		DevOpsPlatform:        client.PlatformGitHub,
		BindingType:           "integration-dop",
		InstallationID:        "65381777",
		DevOpsPlatformURL:     "https://github.com/my-github-org",
		RepoAutoImportEnabled: &autoImport,
	}
	f.bindings[binding.ID] = binding
	return binding
}
