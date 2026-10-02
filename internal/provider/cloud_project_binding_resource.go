package provider

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

var (
	_ resource.Resource                = &cloudProjectBindingResource{}
	_ resource.ResourceWithConfigure   = &cloudProjectBindingResource{}
	_ resource.ResourceWithImportState = &cloudProjectBindingResource{}
)

// NewCloudProjectBindingResource returns the sonarqube_cloud_project_binding resource.
func NewCloudProjectBindingResource() resource.Resource {
	return &cloudProjectBindingResource{}
}

type cloudProjectBindingResource struct {
	client *client.Client
}

func (r *cloudProjectBindingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_project_binding"
}

func (r *cloudProjectBindingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// A different project is a different binding. The destroy of the old
	// binding removes nothing on the server. If the old project is deleted in
	// the same apply, its binding goes with it and the new bind succeeds. If
	// it stays, it keeps the repository, and the server refuses the new bind.
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Binds a SonarQube Cloud project to a GitHub repository. The organization " +
			"of the project must be bound to GitHub, and the GitHub application installation of " +
			"the organization must be able to see the repository.\n\n" +
			"A project that is already bound to the same repository is adopted, not bound " +
			"again. A project that is bound to a different repository gives an error: import " +
			"that binding instead.\n\n" +
			"~> **The API cannot remove a binding.** `terraform destroy` drops this resource " +
			"from the state and gives a warning, but the repository stays bound. A binding goes " +
			"away when its project is deleted.\n\n" +
			"A change of `organization` or `project_key` replaces this resource. When the old " +
			"project is deleted in the same apply, for example because `sonarqube_project` " +
			"replaces it for a new key, its binding goes with it and the new project is bound. " +
			"When the old project stays, it keeps the repository, and the server refuses the new " +
			"binding.\n\n" +
			"A replacement of the project for a new name keeps the key, so this resource sees no " +
			"change, and the project loses its binding. Add `replace_triggered_by` on the project " +
			"to the `lifecycle` block of this resource, so that Terraform binds the new project " +
			"in the same apply.\n\n" +
			"A monorepo, where more than one project is bound to the same repository, is not " +
			"supported. The binding does not turn on automatic analysis.",
		Attributes: map[string]schema.Attribute{
			// The identifier is not kept from the state. An update runs only
			// for a new repository, and it binds the project again when a
			// replacement of the project removed the binding, which gives a
			// new identifier.
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Identifier of the binding.",
			},
			"organization": schema.StringAttribute{
				Required: true,
				Description: "Key of the organization that owns the project. A change replaces the " +
					"binding.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
					stringvalidator.RegexMatches(organizationKeyPattern,
						"must hold lower-case letters, digits and dashes only, with no leading and no trailing dash"),
				},
				PlanModifiers: replace,
			},
			"project_key": schema.StringAttribute{
				Required:    true,
				Description: "Key of the project to bind. A change replaces the binding.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 400),
					stringvalidator.RegexMatches(projectKeyPattern,
						"must hold letters, digits, dashes, underscores, dots and colons only, "+
							"with at least one character that is not a digit"),
				},
				PlanModifiers: replace,
			},
			"repository": schema.StringAttribute{
				Required: true,
				Description: "GitHub repository to bind, as `owner/name`. The comparison ignores " +
					"case. A change binds the project to the new repository.\n\n" +
					"A rename of the repository on GitHub does not change the binding, and the plan " +
					"reports no difference. The provider tells repositories apart by `repository_id`.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(gitHubRepositoryPattern,
						"must be a GitHub repository in the form owner/name"),
				},
			},
			"repository_id": schema.StringAttribute{
				Computed:      true,
				Description:   "Identifier that GitHub gives the repository.",
				PlanModifiers: []planmodifier.String{repositoryIDPlanModifier{}},
			},
		},
	}
}

func (r *cloudProjectBindingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := requireCloudClient(req.ProviderData,
		"The sonarqube_cloud_project_binding resource binds a project")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *cloudProjectBindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan cloudProjectBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A binding to a different repository may belong to someone else, so a
	// create does not change it.
	binding := r.ensureBinding(ctx, plan, refuseOtherRepository, &resp.Diagnostics)
	if binding == nil {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, stateFromProjectBinding(binding, plan))...)
}

func (r *cloudProjectBindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state cloudProjectBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The binding is read through its project, not by its identifier. An
	// import knows the project only, and a binding of a deleted project is
	// gone as well.
	key := state.ProjectKey.ValueString()
	binding, err := findProjectBinding(ctx, r.client, state.Organization.ValueString(), key)
	if errors.Is(err, client.ErrNotFound) {
		// A project that the token may not see gives the same answer as a
		// deleted one, so the removal from the state is reported.
		resp.Diagnostics.AddWarning(
			"The project "+key+" was not found",
			"Terraform removes the binding of this project from the state. The project was "+
				"deleted, or the token cannot read it any more. In the second case, the next "+
				"apply fails, because the project cannot be found.",
		)
		resp.State.RemoveResource(ctx)
		return
	}
	if errors.Is(err, client.ErrNotBound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the binding of the project "+key, err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, stateFromProjectBinding(binding, state))...)
}

func (r *cloudProjectBindingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan cloudProjectBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	binding := r.ensureBinding(ctx, plan, changeOtherRepository, &resp.Diagnostics)
	if binding == nil {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, stateFromProjectBinding(binding, plan))...)
}

// otherRepository says what ensureBinding does with a project that is bound
// to a different repository than the plan.
type otherRepository int

const (
	// refuseOtherRepository reports an error.
	refuseOtherRepository otherRepository = iota
	// changeOtherRepository binds the project to the repository of the plan.
	changeOtherRepository
)

// ensureBinding makes the project of the plan bound to the repository of the
// plan, and returns the binding. It returns nil after it adds an error.
//
// The binding is found through the project, never taken from the state. A
// replacement of the project in the same apply deletes the project, which
// removes its binding, and makes it again with the same key. The plan shows
// no change to the project attributes of this resource, so the state can name
// a binding that is gone. The project is then bound again.
//
// A binding to the same repository is kept as it is. GitHub ignores the case
// of a slug, so a slug that differs in case only is the same repository.
func (r *cloudProjectBindingResource) ensureBinding(
	ctx context.Context,
	plan cloudProjectBindingModel,
	onOther otherRepository,
	diagnostics *diag.Diagnostics,
) *client.ProjectBinding {
	organization := plan.Organization.ValueString()
	key := plan.ProjectKey.ValueString()
	repository := plan.Repository.ValueString()

	project, err := r.client.GetProject(ctx, organization, key)
	if errors.Is(err, client.ErrNotFound) {
		addProjectNotFound(diagnostics, organization, key)
		return nil
	}
	if err != nil {
		diagnostics.AddError("Cannot read the project "+key, err.Error())
		return nil
	}

	binding, err := r.client.FindProjectBinding(ctx, project)
	switch {
	case errors.Is(err, client.ErrNotFound):
		addProjectNotFound(diagnostics, organization, key)
		return nil
	case errors.Is(err, client.ErrNotBound):
		binding, err = r.client.CreateProjectBinding(ctx, project, repository)
	case err != nil:
		diagnostics.AddError("Cannot read the binding of the project "+key, err.Error())
		return nil
	case strings.EqualFold(binding.Slug, repository):
		return binding
	case onOther == refuseOtherRepository:
		diagnostics.AddAttributeError(
			path.Root("repository"),
			"The project "+key+" is already bound to "+binding.Slug,
			"The configuration asks for "+repository+". Terraform does not change a binding "+
				"that it did not make.\n\n"+
				"To manage this binding, import it with the identifier \""+organization+"/"+key+
				"\", then change the repository.",
		)
		return nil
	default:
		binding, err = r.client.UpdateProjectBinding(ctx, binding.ID, repository)
	}

	if err != nil {
		diagnostics.AddError("Cannot bind the project "+key+" to "+repository, bindFailureDetail(err))
		return nil
	}
	return binding
}

// Delete takes the binding out of the state, and nothing else. The API has no
// operation to remove a binding. The server removes it when it deletes the
// project.
func (r *cloudProjectBindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state cloudProjectBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddWarning(
		"The project binding was not removed",
		"The API has no operation to remove a binding, so the project "+state.ProjectKey.ValueString()+
			" stays bound to "+state.Repository.ValueString()+". Terraform has forgotten the "+
			"binding, but the binding is still there.\n\n"+
			"A binding goes away when its project is deleted. If this destroy also deletes the "+
			"project, the binding goes with it and nothing stays behind.",
	)
}

// ImportState takes "organization/project_key", because a read needs both.
// Nobody knows the identifier of the binding before the import.
func (r *cloudProjectBindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organization, key, separated := strings.Cut(req.ID, "/")
	if !separated || organization == "" || key == "" {
		resp.Diagnostics.AddError(
			"Cannot import the project binding",
			"The identifier must hold the organization and the project key, separated "+
				"by a slash, as in \"my-organization/my-project\". It holds "+req.ID+".")
		return
	}

	// Read fills in everything else.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_key"), key)...)
}
