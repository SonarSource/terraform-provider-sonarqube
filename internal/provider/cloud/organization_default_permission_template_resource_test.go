package cloud

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
)

// organizationDefaultPermissionTemplateModel reads the state of the resource
// in the tests.
type organizationDefaultPermissionTemplateModel struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	TemplateID   types.String `tfsdk:"template_id"`
}

// fakeDefaultTemplateInstance holds the organization my-org and the templates
// template-a and template-b.
type fakeDefaultTemplateInstance struct {
	organizationExists bool
	defaultTemplate    string
	calls              []string
	// setStatus makes set_default_template fail with this status when it is
	// not 0.
	setStatus int
}

func (f *fakeDefaultTemplateInstance) start(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.calls = append(f.calls, r.URL.Path)
		if !f.organizationExists || r.Form.Get("organization") != "my-org" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"errors":[{"msg":"No organization with key '%s'"}]}`, r.Form.Get("organization"))
			return
		}
		switch r.URL.Path {
		case "/api/permissions/search_templates":
			_, _ = fmt.Fprintf(w, `{"permissionTemplates":[{"id":"template-a"},{"id":"template-b"}],`+
				`"defaultTemplates":[{"templateId":%q,"qualifier":"TRK"}]}`, f.defaultTemplate)
		case "/api/permissions/set_default_template":
			if f.setStatus != 0 {
				w.WriteHeader(f.setStatus)
				_, _ = fmt.Fprint(w, `{"errors":[{"msg":"refused"}]}`)
				return
			}
			id := r.Form.Get("templateId")
			if id != "template-a" && id != "template-b" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = fmt.Fprintf(w, `{"errors":[{"msg":"Permission template with id '%s' is not found"}]}`, id)
				return
			}
			f.defaultTemplate = id
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return providertest.NewCloudClient(srv)
}

func defaultTemplateResourceSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewOrganizationDefaultPermissionTemplateResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func defaultTemplatePlan(t *testing.T, s schema.Schema, template string) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"organization": "my-org", "template_id": template,
	})}
}

func TestOrganizationDefaultPermissionTemplateResourceSchema(t *testing.T) {
	t.Parallel()
	s := defaultTemplateResourceSchema(t)
	if err := s.ValidateImplementation(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"organization", "template_id"} {
		if !s.Attributes[name].IsRequired() {
			t.Errorf("%s must be required", name)
		}
	}
	// A different template changes the default in place.
	if template, ok := s.Attributes["template_id"].(schema.StringAttribute); !ok || len(template.PlanModifiers) != 0 {
		t.Error("template_id must not require a replacement")
	}
}

func TestOrganizationDefaultPermissionTemplateResourceLifecycleAndDrift(t *testing.T) {
	t.Parallel()
	instance := &fakeDefaultTemplateInstance{organizationExists: true, defaultTemplate: "template-original"}
	r := &organizationDefaultResource{client: instance.start(t), organizationDefault: defaultPermissionTemplate}
	s := defaultTemplateResourceSchema(t)

	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: defaultTemplatePlan(t, s, "template-a")}, created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	model := providertest.ReadModel[organizationDefaultPermissionTemplateModel](t, created.State)
	if instance.defaultTemplate != "template-a" || model.ID.ValueString() != "my-org" {
		t.Fatalf("default = %q, state = %+v", instance.defaultTemplate, model)
	}

	refreshed := &resource.ReadResponse{State: created.State}
	r.Read(context.Background(), resource.ReadRequest{State: created.State}, refreshed)
	if refreshed.Diagnostics.HasError() || !created.State.Raw.Equal(refreshed.State.Raw) {
		t.Fatalf("an unchanged refresh changed the state: %v", refreshed.Diagnostics)
	}

	// A change of the default in the UI shows in the state.
	instance.defaultTemplate = "template-b"
	drift := &resource.ReadResponse{State: created.State}
	r.Read(context.Background(), resource.ReadRequest{State: created.State}, drift)
	if drift.Diagnostics.HasError() {
		t.Fatal(drift.Diagnostics)
	}
	if got := providertest.ReadModel[organizationDefaultPermissionTemplateModel](t, drift.State).TemplateID.ValueString(); got != "template-b" {
		t.Errorf("template_id after drift = %q", got)
	}

	calls := len(instance.calls)
	deleted := &resource.DeleteResponse{}
	r.Delete(context.Background(), resource.DeleteRequest{State: drift.State}, deleted)
	if deleted.Diagnostics.HasError() || deleted.Diagnostics.WarningsCount() != 1 || len(instance.calls) != calls {
		t.Fatalf("delete: %v, calls %v", deleted.Diagnostics, instance.calls[calls:])
	}
}

func TestOrganizationDefaultPermissionTemplateResourceMissingTemplate(t *testing.T) {
	t.Parallel()
	instance := &fakeDefaultTemplateInstance{organizationExists: true, defaultTemplate: "template-a"}
	r := &organizationDefaultResource{client: instance.start(t), organizationDefault: defaultPermissionTemplate}
	s := defaultTemplateResourceSchema(t)
	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: defaultTemplatePlan(t, s, "template-c")}, created)
	providertest.AssertDiagnosticsContain(t, created.Diagnostics, "Permission template template-c not found")
	if instance.defaultTemplate != "template-a" || slices.Contains(instance.calls, "/api/permissions/set_default_template") {
		t.Errorf("a template of another organization was sent: default %q, calls %v", instance.defaultTemplate, instance.calls)
	}
}

func TestOrganizationDefaultPermissionTemplateResourceWarnsWhenOrganizationIsGone(t *testing.T) {
	t.Parallel()
	instance := &fakeDefaultTemplateInstance{}
	r := &organizationDefaultResource{client: instance.start(t), organizationDefault: defaultPermissionTemplate}
	s := defaultTemplateResourceSchema(t)
	state := tfsdk.State{Schema: s, Raw: providertest.SchemaValue(t, s, map[string]string{
		"id": "my-org", "organization": "my-org", "template_id": "template-a",
	})}
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() || resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("a missing organization: %v", resp.Diagnostics)
	}
	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: defaultTemplatePlan(t, s, "template-a")}, created)
	providertest.AssertDiagnosticsContain(t, created.Diagnostics, "Organization my-org not found")
}

func TestOrganizationDefaultPermissionTemplateResourceRefusedSet(t *testing.T) {
	t.Parallel()
	instance := &fakeDefaultTemplateInstance{organizationExists: true, defaultTemplate: "template-a", setStatus: http.StatusForbidden}
	r := &organizationDefaultResource{client: instance.start(t), organizationDefault: defaultPermissionTemplate}
	s := defaultTemplateResourceSchema(t)
	created := &resource.CreateResponse{State: providertest.EmptyState(t, s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: defaultTemplatePlan(t, s, "template-b")}, created)
	providertest.AssertDiagnosticsContain(t, created.Diagnostics, "Cannot set the default permission template of the organization my-org")
	if !created.State.Raw.IsNull() {
		t.Error("a failed create wrote the state")
	}
}
