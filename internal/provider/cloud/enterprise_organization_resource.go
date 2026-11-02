package cloud

import (
	"context"
	_ "embed" // for the go:embed of the schema description
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/validate"
)

var (
	_ resource.Resource                = &enterpriseOrganizationResource{}
	_ resource.ResourceWithConfigure   = &enterpriseOrganizationResource{}
	_ resource.ResourceWithImportState = &enterpriseOrganizationResource{}
)

//go:embed enterprise_organization_resource.md
var enterpriseOrganizationDescription string

type enterpriseOrganizationResource struct{ client *client.Client }

type enterpriseOrganizationModel struct {
	ID           types.String `tfsdk:"id"`
	EnterpriseID types.String `tfsdk:"enterprise_id"`
	Organization types.String `tfsdk:"organization"`
}

// NewEnterpriseOrganizationResource returns the
// sonarqube_cloud_enterprise_organization resource.
func NewEnterpriseOrganizationResource() resource.Resource {
	return &enterpriseOrganizationResource{}
}

func (r *enterpriseOrganizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_enterprise_organization"
}

func (r *enterpriseOrganizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{MarkdownDescription: strings.TrimSpace(enterpriseOrganizationDescription), Attributes: map[string]schema.Attribute{
		"enterprise_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
			Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			MarkdownDescription: "UUID of the enterprise, such as `sonarqube_cloud_enterprise.main.id`."},
		"organization": schema.StringAttribute{Required: true, PlanModifiers: replace, Validators: validate.OrganizationKey(),
			MarkdownDescription: "Key of the organization to attach."},
		"id": schema.StringAttribute{Computed: true, MarkdownDescription: "ID of the relation between the enterprise and the organization.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
	}}
}

func (r *enterpriseOrganizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_cloud_enterprise_organization resource attaches an organization to an enterprise")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

// relationOf returns the relation of an organization, or nil when the
// organization is in no enterprise.
func (r *enterpriseOrganizationResource) relationOf(ctx context.Context, org *client.Organization) (*client.EnterpriseOrganization, error) {
	relations, err := r.client.ListEnterpriseOrganizationsOfOrganization(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	for _, relation := range relations {
		if relation.Matches(org) {
			return &relation, nil
		}
	}
	return nil, nil
}

func (r *enterpriseOrganizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan enterpriseOrganizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	enterpriseID, key := plan.EnterpriseID.ValueString(), plan.Organization.ValueString()
	_, err := r.client.GetEnterprise(ctx, enterpriseID)
	if errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddAttributeError(path.Root("enterprise_id"), "Enterprise "+enterpriseID+" not found",
			"No enterprise with this ID was found. Check the ID, and check that the token user can read the enterprise."+userTokenHint)
		return
	}
	if err != nil {
		addEnterpriseReadError(&resp.Diagnostics, enterpriseID, err)
		return
	}
	org, ok := lookupOrganization(ctx, r.client, key, path.Root("organization"), &resp.Diagnostics)
	if !ok {
		return
	}
	relation, err := r.relationOf(ctx, org)
	if err != nil {
		addRelationReadError(&resp.Diagnostics, key, err)
		return
	}
	if relation == nil {
		relation = r.attach(ctx, enterpriseID, org, &resp.Diagnostics)
		if relation == nil {
			return
		}
	}
	// An organization that is already in the enterprise is adopted.
	if relation.EnterpriseID != enterpriseID {
		r.addOtherEnterpriseError(ctx, key, relation.EnterpriseID, &resp.Diagnostics)
		return
	}
	plan.ID = types.StringValue(relation.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// attach sends the POST. When the answer is lost or has no ID, the server
// possibly attached the organization, so a read finds the relation.
func (r *enterpriseOrganizationResource) attach(ctx context.Context, enterpriseID string, org *client.Organization, diagnostics *diag.Diagnostics) *client.EnterpriseOrganization {
	relation, err := r.client.AttachEnterpriseOrganization(ctx, enterpriseID, org.ID)
	if client.IsAlreadyInEnterprise(err) {
		// Another client attached the organization since the read.
		relation, err = r.relationOf(ctx, org)
		if err != nil {
			addRelationReadError(diagnostics, org.Key, err)
			return nil
		}
		if relation != nil {
			return relation
		}
		diagnostics.AddAttributeError(path.Root("organization"), "Organization "+org.Key+" is already in an enterprise",
			"The organization is in an enterprise that the token user cannot see. Detach it from that enterprise first.")
		return nil
	}
	if refusedByServer(err) {
		detail := err.Error()
		if isAccessDenied(err) {
			detail += userTokenHint
		}
		diagnostics.AddError("Cannot attach the organization "+org.Key+" to the enterprise", detail)
		return nil
	}
	if err == nil && relation.ID != "" {
		return relation
	}
	detail := "The API returned no relation ID."
	if err != nil {
		detail = err.Error()
	}
	recovered, readErr := r.relationOf(ctx, org)
	if readErr != nil || recovered == nil {
		diagnostics.AddError("Cannot attach the organization "+org.Key+" to the enterprise",
			detail+"\n\nThe organization was possibly attached. Run apply again, or import the attachment.")
		return nil
	}
	return recovered
}

func (r *enterpriseOrganizationResource) addOtherEnterpriseError(ctx context.Context, key, otherID string, diagnostics *diag.Diagnostics) {
	other := "with the ID " + otherID
	if enterprise, err := r.client.GetEnterprise(ctx, otherID); err == nil {
		other = enterprise.Key
	}
	diagnostics.AddAttributeError(path.Root("organization"), "The organization "+key+" is in a different enterprise",
		"The organization is in the enterprise "+other+". An organization can be in one enterprise only. "+
			"Detach it from that enterprise first.")
}

func (r *enterpriseOrganizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state enterpriseOrganizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, enterpriseID := state.Organization.ValueString(), state.EnterpriseID.ValueString()
	org, ok := refreshOrganization(ctx, r.client, key, "the attachment", resp)
	if !ok {
		return
	}
	relation, err := r.relationOf(ctx, org)
	if err != nil {
		addRelationReadError(&resp.Diagnostics, key, err)
		return
	}
	if relation == nil || relation.EnterpriseID != enterpriseID {
		// The list is empty also when the token cannot read the enterprise.
		// A read of the enterprise tells that case from a detach.
		_, err := r.client.GetEnterprise(ctx, enterpriseID)
		switch {
		case errors.Is(err, client.ErrNotFound):
			// A token that cannot read the enterprise possibly gets 404 too.
			resp.Diagnostics.AddWarning(enterpriseNotFound(enterpriseID),
				"Terraform removes the attachment of "+key+" from the state. The enterprise was deleted, or the "+
					"token cannot read it any more. In the second case, the organization can stay attached."+userTokenHint)
		case err != nil:
			addEnterpriseReadError(&resp.Diagnostics, enterpriseID, err)
			return
		}
		resp.State.RemoveResource(ctx)
		return
	}
	state.ID = types.StringValue(relation.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: each attribute requires a replacement.
func (r *enterpriseOrganizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.Append(resp.State.Set(ctx, req.Plan.Raw)...)
}

func (r *enterpriseOrganizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state enterpriseOrganizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, id, enterpriseID := state.Organization.ValueString(), state.ID.ValueString(), state.EnterpriseID.ValueString()
	// Two detaches at the same time could both see two organizations, and
	// together detach the last one. Each enterprise has one lock.
	lock, _ := detachLocks.LoadOrStore(enterpriseID, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()
	// The list is empty when the token cannot read the enterprise, so the
	// enterprise is read first. An enterprise that is gone has no relations.
	_, err := r.client.GetEnterprise(ctx, enterpriseID)
	if errors.Is(err, client.ErrNotFound) {
		r.confirmDetached(ctx, key, enterpriseID, &resp.Diagnostics)
		return
	}
	if err != nil {
		addEnterpriseReadError(&resp.Diagnostics, enterpriseID, err)
		return
	}
	relations, err := r.client.ListEnterpriseOrganizationsOfEnterprise(ctx, enterpriseID)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the organizations of the enterprise "+enterpriseID, err.Error())
		return
	}
	attached := false
	for _, relation := range relations {
		attached = attached || relation.ID == id
	}
	if !attached {
		return
	}
	// The server deletes the enterprise when its last organization is
	// detached. The provider does not do that as a side effect.
	if len(relations) == 1 {
		resp.Diagnostics.AddError("Cannot detach the last organization "+key+" of the enterprise",
			"A detach of the last organization deletes the enterprise, so the provider does not do it. First "+
				"remove this resource from the state with a removed block (Terraform 1.7 or later) or terraform "+
				"state rm. Then, to delete the enterprise, destroy the sonarqube_cloud_enterprise resource. The "+
				"destroy of this resource runs before the destroy of the enterprise, so it cannot be done in the "+
				"other order.")
		return
	}
	if err := r.client.DetachEnterpriseOrganization(ctx, id); err != nil {
		resp.Diagnostics.AddError("Cannot detach the organization "+key+" from the enterprise", err.Error())
	}
}

func (r *enterpriseOrganizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	enterpriseKey, organizationKey, found := strings.Cut(req.ID, "/")
	if !found || enterpriseKey == "" || organizationKey == "" || strings.Contains(organizationKey, "/") {
		resp.Diagnostics.AddError("Invalid import ID",
			"The import ID must be <enterprise_key>/<organization_key>, such as my-enterprise/my-organization. Got "+req.ID+".")
		return
	}
	enterprise, ok := lookupEnterprise(ctx, r.client, enterpriseKey, &resp.Diagnostics)
	if !ok {
		return
	}
	org, ok := lookupOrganization(ctx, r.client, organizationKey, path.Root("organization"), &resp.Diagnostics)
	if !ok {
		return
	}
	relation, err := r.relationOf(ctx, org)
	if err != nil {
		addRelationReadError(&resp.Diagnostics, organizationKey, err)
		return
	}
	if relation == nil || relation.EnterpriseID != enterprise.ID {
		resp.Diagnostics.AddError("Organization "+organizationKey+" is not in the enterprise "+enterpriseKey,
			"There is nothing to import. Attach the organization with this resource instead.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &enterpriseOrganizationModel{
		ID: types.StringValue(relation.ID), EnterpriseID: types.StringValue(enterprise.ID), Organization: types.StringValue(org.Key),
	})...)
}

var detachLocks sync.Map

// confirmDetached handles a delete when the enterprise read gives 404. That
// is a deleted enterprise, or possibly a token that cannot read it. The
// relation of the organization tells the two cases apart when the token can
// read it. In other cases, a warning tells that the detach was skipped.
func (r *enterpriseOrganizationResource) confirmDetached(ctx context.Context, key, enterpriseID string, diagnostics *diag.Diagnostics) {
	org, err := r.client.GetOrganization(ctx, key)
	if errors.Is(err, client.ErrNotFound) {
		diagnostics.AddWarning(enterpriseNotFound(enterpriseID)+", and the organization "+key+" was not found",
			"There is nothing to detach. If the token cannot read them, the organization can stay attached."+userTokenHint)
		return
	}
	if err != nil {
		diagnostics.AddError("Cannot read the organization "+key, err.Error())
		return
	}
	relation, err := r.relationOf(ctx, org)
	if err != nil {
		addRelationReadError(diagnostics, key, err)
		return
	}
	if relation != nil && relation.EnterpriseID == enterpriseID {
		diagnostics.AddError("Cannot read the enterprise "+enterpriseID,
			"The organization "+key+" is still in the enterprise, but the token cannot read the enterprise, so "+
				"the provider cannot check that the organization is not the last one."+userTokenHint)
		return
	}
	diagnostics.AddWarning(enterpriseNotFound(enterpriseID),
		"Terraform removes the attachment of "+key+" from the state, and does not detach the organization. The "+
			"enterprise was deleted, or the token cannot read it. In the second case, the organization can stay attached."+userTokenHint)
}

func addEnterpriseReadError(diagnostics *diag.Diagnostics, enterpriseID string, err error) {
	diagnostics.AddError("Cannot read the enterprise "+enterpriseID, err.Error()+userTokenHint)
}

// isAccessDenied reports a 401 or a 403 answer.
func isAccessDenied(err error) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden)
}

func addRelationReadError(diagnostics *diag.Diagnostics, organizationKey string, err error) {
	diagnostics.AddError("Cannot read the enterprise of the organization "+organizationKey, err.Error())
}

func enterpriseNotFound(enterpriseID string) string {
	return "The enterprise " + enterpriseID + " was not found"
}
