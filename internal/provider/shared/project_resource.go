package shared

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/eventual"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/validate"
)

var (
	_ resource.Resource                = &projectResource{}
	_ resource.ResourceWithConfigure   = &projectResource{}
	_ resource.ResourceWithImportState = &projectResource{}
)

// NewProjectResource returns the sonarqube_project resource.
func NewProjectResource() resource.Resource {
	return &projectResource{keyWaits: []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}}
}

type projectResource struct {
	client *client.Client
	// keyWaits are the waits before Update reads the project again under its
	// new key. The project list shows a new key some seconds after the change.
	keyWaits []time.Duration
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
	resp.Schema = schema.Schema{
		Description: "Manages a SonarQube Cloud project without a repository binding.\n\n" +
			"A change to `key` or `main_branch` updates the project in place, and the project keeps " +
			"its analysis history. SonarQube Cloud cannot rename a project or move it to a different " +
			"organization, so the plan refuses a change to `name` or `organization`. A new project in " +
			"their place loses the analysis history, the issues and the settings, so Terraform makes it " +
			"only after the user deletes the project.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Project key.",
				PlanModifiers: []planmodifier.String{projectIDFromKey{}},
			},
			"key": schema.StringAttribute{
				Required: true,
				Description: "Key of the project. Must be unique. A new key changes the key in place, and " +
					"the project keeps its analysis history. The resources that refer to the key are " +
					"replaced.",
				Validators: validate.ProjectKey(),
			},
			"name": schema.StringAttribute{
				Required: true,
				Description: "Name of the project. SonarQube Cloud cannot rename a project, so the plan " +
					"refuses a new name. To delete the project and make a new one with a new name, keep the " +
					"old name and run `terraform destroy -target` with the address of the project. Then set " +
					"the new name and run `terraform apply`. The project then loses its analysis history.\n\n" +
					"A name that changed outside Terraform stays out of the state, so Terraform plans no " +
					"change.",
				// The server abbreviates a longer name, which would leave the
				// state holding a name that the project does not have.
				Validators: []validator.String{stringvalidator.LengthBetween(1, 500)},
				PlanModifiers: []planmodifier.String{refuseChange{summary: "Cannot rename the project",
					reason: "SonarQube Cloud cannot rename a project."}},
			},
			"organization": schema.StringAttribute{
				Required: true,
				Description: "Key of the organization that owns the project. SonarQube Cloud cannot move a " +
					"project to a different organization, so the plan refuses a new value, also after a new " +
					"key of the organization. To keep the projects after a new organization key, follow " +
					"\"Change the key\" in the docs of `sonarqube_cloud_organization`.",
				Validators: validate.OrganizationKey(),
				PlanModifiers: []planmodifier.String{refuseChange{summary: "Cannot move the project",
					reason: "SonarQube Cloud cannot move a project to a different organization. When the key of " +
						"the organization changed, see the section \"Change the key\" in the docs of " +
						"sonarqube_cloud_organization to keep the project. To move it anyway, the project is deleted."}},
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

// refuseChange refuses a new value for a project in the state, when SonarQube
// Cloud cannot change the value in place. A replacement would delete the
// analysis history, so the user must delete the project first.
type refuseChange struct {
	summary, reason string
}

func (m refuseChange) Description(context.Context) string {
	return "Refuses a new value. " + m.reason
}

func (m refuseChange) MarkdownDescription(ctx context.Context) string { return m.Description(ctx) }

func (m refuseChange) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || req.PlanValue.IsUnknown() || req.PlanValue.Equal(req.StateValue) {
		return
	}
	resp.Diagnostics.AddAttributeError(req.Path, m.summary,
		m.reason+" Set the value back to "+req.StateValue.String()+". To delete the project and make a new one "+
			"with the new value: with the old value in the configuration, run terraform destroy -target with "+
			"the address of this resource. Then set the new value and run terraform apply. The project then "+
			"loses its analysis history, issues and settings.")
}

// projectIDFromKey plans the id as the planned key, so that a new key also
// changes the id in the plan.
type projectIDFromKey struct{}

func (projectIDFromKey) Description(context.Context) string {
	return "Takes the value of the key attribute."
}

func (m projectIDFromKey) MarkdownDescription(ctx context.Context) string { return m.Description(ctx) }

func (projectIDFromKey) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var key types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("key"), &key)...)
	resp.PlanValue = key
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
	project, branch, err := r.findProject(ctx, state.Organization.ValueString(), key)
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the project "+key, err.Error())
		return
	}
	state.ID = types.StringValue(project.Key)
	state.MainBranch = types.StringValue(branch)
	// Only an import lacks a name. The analysis can change the name on the
	// server, and the plan refuses a new name, so the state keeps its name.
	if state.Name.IsNull() {
		state.Name = types.StringValue(project.Name)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// findProject reads what Read needs: the project and its main branch. A
// project that one of the lists does not show is ErrNotFound.
func (r *projectResource) findProject(ctx context.Context, organization, key string) (*client.Project, string, error) {
	project, err := r.client.GetProject(ctx, organization, key)
	if err != nil {
		return nil, "", err
	}
	branch, err := r.client.GetMainBranch(ctx, key)
	if err != nil {
		return nil, "", fmt.Errorf("cannot read the main branch: %w", err)
	}
	return project, branch, nil
}

func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The key and the main branch change in place. The plan refuses a new
	// name or organization, but a value that was unknown in the plan is
	// known only now.
	if !plan.Name.Equal(state.Name) || !plan.Organization.Equal(state.Organization) {
		resp.Diagnostics.AddError("Cannot rename or move the project "+state.Key.ValueString(),
			"SonarQube Cloud cannot rename a project or move it to a different organization. Set the name "+
				"and the organization back to the values of the project.")
		return
	}
	key := plan.Key.ValueString()
	if !plan.Key.Equal(state.Key) {
		if err := r.client.UpdateProjectKey(ctx, state.Key.ValueString(), key); err != nil {
			resp.Diagnostics.AddError("Cannot change the key of the project "+state.Key.ValueString(), err.Error())
			return
		}
		// The state follows the server before the next calls, so that a
		// failure does not leave the old key in the state.
		state.Key, state.ID = plan.Key, plan.Key
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		// The lists that Read uses show the new key some seconds after the
		// change. A refresh before then removes the project from the state.
		_, err := eventual.ReadAgain(ctx, r.keyWaits,
			func() (string, error) {
				_, branch, err := r.findProject(ctx, plan.Organization.ValueString(), key)
				return branch, err
			},
			func(_ string, err error) bool { return !errors.Is(err, client.ErrNotFound) })
		if err != nil {
			// The key changed, and the state holds the new key. A failed
			// main branch rename below still reports an error.
			resp.Diagnostics.AddWarning("Cannot read the project "+key+" after the change of its key",
				"The key changed. If the next refresh does not find the project, wait some seconds and run it again. "+
					err.Error())
		}
	}
	if !plan.MainBranch.Equal(state.MainBranch) {
		if err := r.client.RenameMainBranch(ctx, key, plan.MainBranch.ValueString()); err != nil {
			resp.Diagnostics.AddError("Cannot set the main branch of the project "+key, err.Error())
			return
		}
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
