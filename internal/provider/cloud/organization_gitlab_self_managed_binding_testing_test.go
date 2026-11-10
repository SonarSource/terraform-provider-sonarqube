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

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

const (
	theGitLabOrganizationUUID = "00000000-0000-4000-8000-000000000001"
	theConfigurationID        = "00000000-0000-4000-8000-000000000003"
	theGitLabServer           = "https://gitlab.example.com"
	theGitLabGroupID          = "42"
)

func gitlabBindingResourceSchema(t *testing.T) schema.Schema {
	t.Helper()

	resp := &resource.SchemaResponse{}
	NewOrganizationGitLabSelfManagedBindingResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// fakeGitLabInstance answers the calls of a binding to GitLab Self-Managed:
// the read of an organization, the GitLab configurations, and the bindings.
// It names a group by its path on write and by a number on read, as the real
// server does.
type fakeGitLabInstance struct {
	organizations  map[string]client.Organization
	configurations map[string]*client.GitLabConfiguration
	bindings       map[string]*client.OrganizationBinding
	// refuseBind makes the bind call fail with a 400.
	refuseBind bool
	// failBindAfterStore makes the bind call store the binding and then
	// answer 502, as a gateway does after a timeout.
	failBindAfterStore bool
	// refuseConfiguration makes the create of a configuration fail with a 400
	// that names this field.
	refuseConfiguration string
	// failures answers a call, named "METHOD /path", with this status.
	failures map[string]int
	// failureFields names the field that the answer of a failed call blames,
	// as the server does in relatedField. A call that it does not hold
	// blames no field.
	failureFields map[string]string
	calls         []string
	bodies        map[string]map[string]any
}

func newFakeGitLabInstance() *fakeGitLabInstance {
	return &fakeGitLabInstance{
		organizations: map[string]client.Organization{
			"my-org": {ID: theOrganizationID, UUIDV4: theGitLabOrganizationUUID, Key: "my-org"},
		},
		configurations: map[string]*client.GitLabConfiguration{},
		bindings:       map[string]*client.OrganizationBinding{},
		bodies:         map[string]map[string]any{},
		failures:       map[string]int{},
		failureFields:  map[string]string{},
	}
}

func (f *fakeGitLabInstance) start(t *testing.T) *client.Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return providertest.NewCloudClient(srv)
}

func (f *fakeGitLabInstance) serve(w http.ResponseWriter, r *http.Request) {
	call := r.Method + " " + r.URL.Path
	f.calls = append(f.calls, call)

	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	f.bodies[call] = body

	if status, fail := f.failures[call]; fail {
		body := `{"message":"the server refuses this call"}`
		if field, named := f.failureFields[call]; named {
			body = `{"message":"the server refuses this call","relatedField":"` + field + `"}`
		}
		writeStatus(w, status, body)
		return
	}

	switch {
	case r.URL.Path == "/organizations/organizations":
		f.serveOrganization(w, r)
	case strings.HasPrefix(r.URL.Path, gitlabConfigurationsCollection):
		f.serveConfigurations(w, r, body)
	case strings.HasPrefix(r.URL.Path, gitlabBindingsCollection):
		f.serveBindings(w, r, body)
	default:
		writeStatus(w, http.StatusNotFound, `{"message":"no such path"}`)
	}
}

const (
	gitlabConfigurationsCollection = "/gitlab-configurations"
	gitlabBindingsCollection       = "/dop-translation/organization-bindings"
)

func (f *fakeGitLabInstance) serveOrganization(w http.ResponseWriter, r *http.Request) {
	org, found := f.organizations[r.URL.Query().Get("organizationKey")]
	if !found {
		writeStatus(w, http.StatusNotFound, `{"message":"no such organization"}`)
		return
	}
	json.NewEncoder(w).Encode([]client.Organization{org})
}

func (f *fakeGitLabInstance) serveConfigurations(w http.ResponseWriter, r *http.Request, body map[string]any) {
	switch {
	case r.URL.Path == gitlabConfigurationsCollection && r.Method == http.MethodPost:
		f.createConfiguration(w, body)
	case r.URL.Path == gitlabConfigurationsCollection:
		found := []client.GitLabConfiguration{}
		for _, c := range f.configurations {
			if c.OrganizationID == r.URL.Query().Get("organizationId") {
				found = append(found, *c)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"gitlabConfigurations": found})
	default:
		f.serveConfiguration(w, r.Method, strings.TrimPrefix(r.URL.Path, gitlabConfigurationsCollection+"/"), body)
	}
}

func (f *fakeGitLabInstance) serveBindings(w http.ResponseWriter, r *http.Request, body map[string]any) {
	switch {
	case r.URL.Path == gitlabBindingsCollection && r.Method == http.MethodPost:
		f.createBinding(w, body)
	case r.URL.Path == gitlabBindingsCollection:
		found := []client.OrganizationBinding{}
		for _, b := range f.bindings {
			if b.OrganizationID == r.URL.Query().Get("organizationId") {
				found = append(found, *b)
			}
		}
		if len(found) == 0 {
			writeStatus(w, http.StatusNotFound, "")
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"organizationBindings": found})
	default:
		f.serveBinding(w, r.Method, strings.TrimPrefix(r.URL.Path, gitlabBindingsCollection+"/"), body)
	}
}

func writeStatus(w http.ResponseWriter, status int, body string) {
	w.WriteHeader(status)
	w.Write([]byte(body))
}

func (f *fakeGitLabInstance) createConfiguration(w http.ResponseWriter, body map[string]any) {
	if f.refuseConfiguration != "" {
		writeStatus(w, http.StatusBadRequest,
			`{"message":"the server refuses this field","relatedField":"`+f.refuseConfiguration+`"}`)
		return
	}
	organizationID, _ := body["organizationId"].(string)
	for _, c := range f.configurations {
		if c.OrganizationID == organizationID {
			writeStatus(w, http.StatusConflict, `{"message":"A GitLab configuration already exists"}`)
			return
		}
	}
	configuration := &client.GitLabConfiguration{
		ID:             theConfigurationID,
		OrganizationID: organizationID,
		APIURL:         body["apiUrl"].(string),
		WebURL:         body["webUrl"].(string),
	}
	f.configurations[configuration.ID] = configuration
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(configuration)
}

func (f *fakeGitLabInstance) serveConfiguration(w http.ResponseWriter, method, id string, body map[string]any) {
	configuration, found := f.configurations[id]
	if !found {
		writeStatus(w, http.StatusNotFound, `{"message":"no such configuration"}`)
		return
	}
	switch method {
	case http.MethodDelete:
		delete(f.configurations, id)
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodPatch:
		if v, ok := body["webUrl"].(string); ok {
			configuration.WebURL = v
		}
		if v, ok := body["apiUrl"].(string); ok {
			configuration.APIURL = v
		}
	}
	json.NewEncoder(w).Encode(configuration)
}

func (f *fakeGitLabInstance) createBinding(w http.ResponseWriter, body map[string]any) {
	if f.refuseBind {
		writeStatus(w, http.StatusBadRequest, `{"message":"The GitLab access token is not valid.","relatedField":"accessToken"}`)
		return
	}
	group, _ := body["devOpsPlatformOrganizationId"].(string)
	binding := f.bindingOf(body["organizationId"].(string), group)
	if f.failBindAfterStore {
		writeStatus(w, http.StatusBadGateway, `{"message":"the gateway timed out"}`)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(binding)
}

func (f *fakeGitLabInstance) serveBinding(w http.ResponseWriter, method, id string, body map[string]any) {
	binding, found := f.bindings[id]
	if !found {
		writeStatus(w, http.StatusNotFound, `{"message":"no such binding"}`)
		return
	}
	if method == http.MethodPatch {
		if v, ok := body["tenantId"].(string); ok {
			binding.TenantID = v
		}
		if v, ok := body["devOpsPlatformUrl"].(string); ok {
			binding.DevOpsPlatformURL = v
		}
	}
	json.NewEncoder(w).Encode(binding)
}

// bindingOf stores a binding of the organization to the group, as the server
// reports it after the bind.
func (f *fakeGitLabInstance) bindingOf(organizationID, group string) *client.OrganizationBinding {
	binding := &client.OrganizationBinding{
		ID:                           theBindingID,
		OrganizationID:               organizationID,
		OrganizationUUIDV4:           theGitLabOrganizationUUID,
		DevOpsPlatform:               client.PlatformGitLab,
		BindingType:                  "integration-dop",
		DopFlavor:                    client.FlavorGitLabSelfManaged,
		DevOpsPlatformOrganizationID: theGitLabGroupID,
		DevOpsPlatformURL:            theGitLabServer + "/groups/" + group,
		TenantID:                     theGitLabServer,
	}
	f.bindings[binding.ID] = binding
	return binding
}

// bound puts a configuration and a binding of my-org into the instance, as
// they are after a create that Terraform already recorded.
func (f *fakeGitLabInstance) bound() {
	f.configurations[theConfigurationID] = &client.GitLabConfiguration{
		ID:             theConfigurationID,
		OrganizationID: theGitLabOrganizationUUID,
		APIURL:         theGitLabServer + gitlabAPIPath,
		WebURL:         theGitLabServer,
	}
	f.bindingOf(theOrganizationID, "my-group")
}

// boundState is the state of the binding that bound makes.
func boundState() map[string]any {
	return map[string]any{
		"id":                   theBindingID,
		"organization_key":     "my-org",
		"server_url":           theGitLabServer,
		"api_url":              theGitLabServer + gitlabAPIPath,
		"group":                "my-group",
		"access_token":         "test-token",
		"configuration_id":     theConfigurationID,
		"group_id":             theGitLabGroupID,
		"dev_ops_platform_url": theGitLabServer + "/groups/my-group",
		"organization_id":      theOrganizationID,
		"organization_uuid_v4": theGitLabOrganizationUUID,
	}
}

// withChanges copies attributes and applies the changes.
func withChanges(attributes, changes map[string]any) map[string]any {
	out := make(map[string]any, len(attributes))
	for k, v := range attributes {
		out[k] = v
	}
	for k, v := range changes {
		out[k] = v
	}
	return out
}
