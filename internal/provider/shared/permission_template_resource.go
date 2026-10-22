package shared

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	_ resource.Resource                   = &permissionTemplateResource{}
	_ resource.ResourceWithConfigure      = &permissionTemplateResource{}
	_ resource.ResourceWithImportState    = &permissionTemplateResource{}
	_ resource.ResourceWithValidateConfig = &permissionTemplateResource{}
)

// NewPermissionTemplateResource returns the sonarqube_permission_template resource.
func NewPermissionTemplateResource() resource.Resource {
	return &permissionTemplateResource{}
}

type permissionTemplateResource struct {
	client *client.Client
}

type permissionTemplateResourceModel struct {
	ID                        types.String `tfsdk:"id"`
	Organization              types.String `tfsdk:"organization"`
	Name                      types.String `tfsdk:"name"`
	Description               types.String `tfsdk:"description"`
	ProjectKeyPattern         types.String `tfsdk:"project_key_pattern"`
	ProjectCreatorPermissions types.Set    `tfsdk:"project_creator_permissions"`
	Groups                    types.Set    `tfsdk:"group"`
	Users                     types.Set    `tfsdk:"user"`
}

type templateGroupModel struct {
	Name        types.String `tfsdk:"name"`
	Permissions types.Set    `tfsdk:"permissions"`
}

type templateUserModel struct {
	Login       types.String `tfsdk:"login"`
	Permissions types.Set    `tfsdk:"permissions"`
}

var (
	templateGroupType = types.ObjectType{AttrTypes: map[string]attr.Type{"name": types.StringType, "permissions": types.SetType{ElemType: types.StringType}}}
	templateUserType  = types.ObjectType{AttrTypes: map[string]attr.Type{"login": types.StringType, "permissions": types.SetType{ElemType: types.StringType}}}
)

func (r *permissionTemplateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_permission_template"
}

func (r *permissionTemplateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	permissions := func(description string) schema.SetAttribute {
		return schema.SetAttribute{Required: true, ElementType: types.StringType, Description: description,
			Validators: []validator.Set{setvalidator.SizeAtLeast(1), setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))}}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "This resource manages a permission template and all its grants. When a user creates a project, " +
			"the template whose project key pattern matches the key gives its permissions to the new " +
			"project. When no pattern matches, the default template of the organization does. A change to a " +
			"template does not change the projects that exist.\n\n" +
			"## All the grants\n\n" +
			"The resource controls all the grants of the template: the grants of the groups, the users and " +
			"the project creator. A grant that is not in the configuration, also one that a user added in " +
			"the UI, shows as a change in the next plan, and the apply removes it.\n\n" +
			"The resource refuses the `Anyone` group. If the template gives a permission to `Anyone`, for " +
			"example after a change in the UI, the next apply removes it.\n\n" +
			"## Plans\n\n" +
			"SonarQube Cloud needs the Team or the Enterprise plan to create a template or to change its " +
			"grants. On the Free plan, the apply fails. The token must have the Administer permission on the " +
			"organization.\n\n" +
			"## Users\n\n" +
			"A user must be a member of the organization. If the user is not a member, the apply fails.\n\n" +
			"## The default template\n\n" +
			"SonarQube Cloud refuses to delete the default template. Before you destroy a template that is " +
			"the default, set a different template as the default.\n\n" +
			"## Project permissions\n\n" +
			"The project permissions are `admin`, `codeviewer`, `issueadmin`, `securityhotspotadmin`, `scan` " +
			"and `user`. SonarQube Cloud also has `architectureadmin`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Identifier of the template.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"organization": schema.StringAttribute{Required: true, Description: "Key of the organization that owns the template. A change replaces the template.",
				Validators: validate.OrganizationKey(), PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name": schema.StringAttribute{Required: true,
				Description: "Name of the template. The names in an organization must be different, also when the case is ignored.",
				Validators:  []validator.String{stringvalidator.LengthBetween(1, 100)}},
			"description": schema.StringAttribute{Optional: true, Description: "Description of the template.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 4000)}},
			"project_key_pattern": schema.StringAttribute{Optional: true,
				Description: "Java regular expression. The template applies to the new projects whose key matches it. " +
					"Two templates of an organization cannot have the same pattern.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 500)}},
			"project_creator_permissions": schema.SetAttribute{Optional: true, ElementType: types.StringType,
				Description: "Project permissions that the template gives to the user who creates the project.",
				Validators:  []validator.Set{setvalidator.SizeAtLeast(1), setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))}},
		},
		Blocks: map[string]schema.Block{
			"group": schema.SetNestedBlock{Description: "Project permissions of a group. Each group can appear once.",
				NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
					"name": schema.StringAttribute{Required: true, Description: "Exact group name, with the same case. Do not use the Anyone group.",
						Validators: []validator.String{stringvalidator.LengthBetween(1, 255), stringvalidator.NoneOfCaseInsensitive("Anyone")}},
					"permissions": permissions("Project permissions of the group, such as `user` or `admin`."),
				}}},
			"user": schema.SetNestedBlock{Description: "Project permissions of a user. Each user can appear once.",
				NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
					"login": schema.StringAttribute{Required: true,
						Description: "Exact user login, with the same case. The user must be a member of the organization.",
						Validators:  []validator.String{stringvalidator.LengthAtLeast(1)}},
					"permissions": permissions("Project permissions of the user, such as `user` or `admin`."),
				}}},
		},
	}
}

func (r *permissionTemplateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_permission_template resource manages a permission template")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

// ValidateConfig refuses a group or a user that appears in two blocks.
func (r *permissionTemplateResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	refuseDuplicateBlocks[templateGroupModel](ctx, req.Config, "group", &resp.Diagnostics)
	refuseDuplicateBlocks[templateUserModel](ctx, req.Config, "user", &resp.Diagnostics)
}

func (r *permissionTemplateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan permissionTemplateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	desired := plan.grants(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	organization := plan.Organization.ValueString()
	id, err := r.client.CreatePermissionTemplate(ctx, organization, plan.template())
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the permission template "+plan.Name.ValueString(), organizationErrorDetail(err, organization))
		return
	}
	plan.ID = types.StringValue(id)

	// The template exists now. When a grant fails, the state keeps it
	// without grants, and Terraform marks it to be replaced.
	withoutGrants := plan
	withoutGrants.setGrants(ctx, nil, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &withoutGrants)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.syncGrants(ctx, plan, desired, &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

func (r *permissionTemplateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan permissionTemplateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	desired := plan.grants(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	organization := plan.Organization.ValueString()
	template := plan.template()
	template.ID = plan.ID.ValueString()
	if err := r.client.UpdatePermissionTemplate(ctx, organization, template); err != nil {
		resp.Diagnostics.AddError("Cannot update the permission template "+plan.Name.ValueString(), organizationErrorDetail(err, organization))
		return
	}
	r.syncGrants(ctx, plan, desired, &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

// syncGrants makes the grants of the template equal to desired. It compares
// desired with the grants on the server, not with the state, so that a grant
// added after the last refresh goes too.
func (r *permissionTemplateResource) syncGrants(ctx context.Context, plan permissionTemplateResourceModel, desired []client.TemplateGrant, diagnostics *diag.Diagnostics) {
	organization, id := plan.Organization.ValueString(), plan.ID.ValueString()
	summary := "Cannot change the grants of the permission template " + plan.Name.ValueString()
	current, err := r.client.GetPermissionTemplate(ctx, organization, id)
	if err != nil {
		diagnostics.AddError(summary, organizationErrorDetail(err, organization))
		return
	}
	adds, removes := grantDiff(current.Grants, desired)
	for _, grant := range adds {
		if err := r.client.AddPermissionTemplateGrant(ctx, organization, id, grant); err != nil {
			diagnostics.AddError(summary, "Give "+describeGrant(grant)+": "+organizationErrorDetail(err, organization))
			return
		}
	}
	for _, grant := range removes {
		if err := r.client.RemovePermissionTemplateGrant(ctx, organization, id, grant); err != nil {
			diagnostics.AddError(summary, "Remove "+describeGrant(grant)+": "+organizationErrorDetail(err, organization))
			return
		}
	}
}

func (r *permissionTemplateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state permissionTemplateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization := state.Organization.ValueString()
	template, err := r.client.GetPermissionTemplate(ctx, organization, state.ID.ValueString())
	switch {
	case errors.Is(err, client.ErrOrganizationNotFound):
		resp.Diagnostics.AddWarning(organizationNotFound(organization),
			"Terraform removes the permission template from the state. The organization was deleted, or "+
				"the token cannot read it any more.")
		resp.State.RemoveResource(ctx)
	case errors.Is(err, client.ErrNotFound):
		resp.State.RemoveResource(ctx)
	case err != nil:
		resp.Diagnostics.AddError("Cannot read the permission template "+state.Name.ValueString(), err.Error())
	default:
		state.setTemplate(ctx, template, &resp.Diagnostics)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	}
}

func (r *permissionTemplateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state permissionTemplateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization := state.Organization.ValueString()
	if err := r.client.DeletePermissionTemplate(ctx, organization, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot delete the permission template "+state.Name.ValueString(), organizationErrorDetail(err, organization))
	}
}

// ImportState takes "<organization>/<name>". An organization key has no "/",
// but a template name can. Read fills in the other attributes.
func (r *permissionTemplateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organization, name, _ := strings.Cut(req.ID, "/")
	if !validate.OrganizationKeyPattern.MatchString(organization) || name == "" {
		resp.Diagnostics.AddError("Invalid permission template import ID", "Use <organization>/<name>.")
		return
	}
	id, err := r.client.FindPermissionTemplateID(ctx, organization, name)
	if errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Permission template "+name+" not found",
			"No template of the organization "+organization+" has this name.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot find the permission template "+name, organizationErrorDetail(err, organization))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
}

func organizationNotFound(organization string) string {
	return "The organization " + organization + " was not found"
}

// organizationErrorDetail explains a missing organization. The client already
// adds hints to the other errors.
func organizationErrorDetail(err error, organization string) string {
	if errors.Is(err, client.ErrOrganizationNotFound) {
		return organizationNotFound(organization) + ". Check the key, and check that the token can read the organization."
	}
	return err.Error()
}
