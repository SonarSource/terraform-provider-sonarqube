package shared

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                = &projectResource{}
	_ resource.ResourceWithConfigure   = &projectResource{}
	_ resource.ResourceWithImportState = &projectResource{}
)

// NewProjectResource returns the sonarqube_project resource.
func NewProjectResource() resource.Resource {
	return &projectResource{}
}

type projectResource struct {
	client *client.Client
}

type projectResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Key          types.String `tfsdk:"key"`
	Name         types.String `tfsdk:"name"`
	Organization types.String `tfsdk:"organization"`
	MainBranch   types.String `tfsdk:"main_branch"`
}

func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *projectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages a SonarQube Cloud project without a repository binding.\n\n" +
			"A change to `key`, `name` or `organization` replaces the project. SonarQube Cloud " +
			"cannot rename a project. It cannot move a project to a different organization.\n\n" +
			"A replacement deletes the project. The analysis history, the issues and " +
			"the settings go with it. Terraform then makes an empty project. A change to " +
			"`main_branch` renames the main branch in place, and the project keeps its history.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Project key.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"key": schema.StringAttribute{
				Required:      true,
				Description:   "Key of the project. Must be unique.",
				Validators:    validate.ProjectKey(),
				PlanModifiers: replace,
			},
			"name": schema.StringAttribute{
				Required: true,
				Description: "Name of the project. A new name replaces the project. " +
					"The project then loses its analysis history.\n\n" +
					"A name that changed outside Terraform stays out of the state. " +
					"Terraform thus plans no replacement.",
				// The server abbreviates a longer name, which would leave the
				// state holding a name that the project does not have.
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 500)},
				PlanModifiers: replace,
			},
			"organization": schema.StringAttribute{
				Required:      true,
				Description:   "Key of the organization that owns the project.",
				Validators:    validate.OrganizationKey(),
				PlanModifiers: replace,
			},
			"main_branch": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Name of the main branch of the project. Set it to the default branch of the " +
					"repository, because a pull request can target only a long-lived branch. A change " +
					"renames the main branch in place, and the project keeps its analysis history. " +
					"No other branch of the project can have this name.\n\n" +
					"When you do not set it, SonarQube Cloud chooses the main branch of a new project, " +
					"and Terraform keeps the main branch that the project has.",
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 255)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *projectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData,
		"The sonarqube_project resource manages a project")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := plan.Key.ValueString()
	if err := r.client.CreateProject(ctx, plan.Organization.ValueString(), key, plan.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot create the project "+key, err.Error())
		return
	}

	plan.ID = types.StringValue(key)
	configured := !plan.MainBranch.IsNull() && !plan.MainBranch.IsUnknown()
	if !configured {
		plan.MainBranch = types.StringNull()
	}
	// The state holds the project before the next calls, so that a failure
	// taints the project and does not leave one that Terraform does not know.
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if configured {
		if err := r.client.RenameMainBranch(ctx, key, plan.MainBranch.ValueString()); err != nil {
			resp.Diagnostics.AddError("Cannot set the main branch of the project "+key, err.Error())
		}
		return
	}
	branch, err := r.client.GetMainBranch(ctx, key)
	if err != nil {
		// An error here would taint a project that exists, and the next
		// refresh reads the main branch.
		resp.Diagnostics.AddWarning("Cannot read the main branch of the project "+key, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("main_branch"), branch)...)
}

func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := state.Key.ValueString()
	project, err := r.client.GetProject(ctx, state.Organization.ValueString(), key)
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the project "+key, err.Error())
		return
	}
	branch, err := r.client.GetMainBranch(ctx, key)
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the main branch of the project "+key, err.Error())
		return
	}
	state.ID = types.StringValue(project.Key)
	state.MainBranch = types.StringValue(branch)
	// Only an import lacks a name. Taking any other name from the server would
	// plan a replacement, which deletes the analysis history.
	if state.Name.IsNull() {
		state.Name = types.StringValue(project.Name)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The main branch is the only attribute that does not replace the project.
	key := state.Key.ValueString()
	if err := r.client.RenameMainBranch(ctx, key, plan.MainBranch.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot set the main branch of the project "+key, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key := state.Key.ValueString()
	if err := r.client.DeleteProject(ctx, key); err != nil {
		resp.Diagnostics.AddError("Cannot delete the project "+key, err.Error())
	}
}

// ImportState takes "organization/key", because a read needs both.
func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organization, key, separated := strings.Cut(req.ID, "/")
	if !separated || organization == "" || key == "" {
		resp.Diagnostics.AddError(
			"Cannot import the project",
			"The identifier must hold the organization and the project key, separated "+
				"by a slash, as in \"my-organization/my-project\". It holds "+req.ID+".")
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), key)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), key)...)
}
