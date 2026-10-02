package provider

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// gitHubRepositoryPattern is the slug of a GitHub repository, "owner/name".
// GitHub allows letters, digits and dashes in the owner, and dots and
// underscores as well in the name.
var gitHubRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)

// cloudProjectBindingModel is the state of the resource and of the data source,
// which report the same fields.
type cloudProjectBindingModel struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	ProjectKey   types.String `tfsdk:"project_key"`
	Repository   types.String `tfsdk:"repository"`
	RepositoryID types.String `tfsdk:"repository_id"`
}

// stateFromProjectBinding turns a binding of the API into state.
//
// prior is the plan or the state that the operation started from. It supplies
// the organization and the project key, which the API never reports: it names
// a project by its internal identifier only.
//
// The repository identifier, not the slug, says which repository is bound. A
// slug can change for the same repository: GitHub ignores its case, and a
// rename on GitHub gives a new one. The slug of prior is therefore kept while
// it names the same repository, or the next plan would report a change that
// is not one, and its apply would fail as an inconsistent result.
//
// prior names the same repository when it knows no identifier, which is the
// plan of a bind or of a change: the server accepted that slug for this
// binding. It also does when its identifier is the one of the binding. A
// different identifier means that someone bound another repository, and the
// slug of the server is reported then.
func stateFromProjectBinding(binding *client.ProjectBinding, prior cloudProjectBindingModel) cloudProjectBindingModel {
	repository := types.StringValue(binding.Slug)
	sameRepository := prior.RepositoryID.IsUnknown() || prior.RepositoryID.IsNull() ||
		prior.RepositoryID.ValueString() == binding.RepositoryID
	if !prior.Repository.IsNull() && !prior.Repository.IsUnknown() && sameRepository {
		repository = prior.Repository
	}

	return cloudProjectBindingModel{
		ID:           types.StringValue(binding.ID),
		Organization: prior.Organization,
		ProjectKey:   prior.ProjectKey,
		Repository:   repository,
		RepositoryID: types.StringValue(binding.RepositoryID),
	}
}

// findProjectBinding reads the project, then its binding. A project that is
// gone gives ErrNotFound, and a project that is not bound gives ErrNotBound.
//
// The bindings API names a project by its legacy identifier, which only a
// read of the project reports.
func findProjectBinding(ctx context.Context, c *client.Client, organization, key string) (*client.ProjectBinding, error) {
	project, err := c.GetProject(ctx, organization, key)
	if err != nil {
		return nil, err
	}
	return c.FindProjectBinding(ctx, project)
}

// addProjectNotFound reports a project that the bindings API cannot find.
func addProjectNotFound(diagnostics *diag.Diagnostics, organization, key string) {
	// A project that the token may not see gives the same answer.
	diagnostics.AddAttributeError(
		path.Root("project_key"),
		"Project "+key+" not found",
		"No project with this key was found in the organization "+organization+
			". Check the key and the organization, and check that the token can read "+
			"the project.",
	)
}

// bindFailureDetail adds the causes that the server gives no clear message
// for. The message of the API is not a stable interface, so every refusal of
// the API gets the full list instead of a hint that depends on its text. A
// failure that did not come from the API, such as a network error, gets
// nothing.
func bindFailureDetail(err error) string {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return err.Error()
	}
	return err.Error() + "\n\n" +
		"The server refuses a binding when:\n" +
		"  - the organization is not bound to GitHub,\n" +
		"  - the GitHub application installation of the organization cannot see the " +
		"repository,\n" +
		"  - the project is public and the repository is private,\n" +
		"  - another project of the organization is already bound to the repository. " +
		"Terraform does not support a monorepo."
}

// repositoryIDPlanModifier keeps the prior repository_id while the repository
// stays the same. A new repository has a new identifier, which the apply
// reports. The comparison ignores case, as GitHub does.
type repositoryIDPlanModifier struct{}

func (m repositoryIDPlanModifier) Description(_ context.Context) string {
	return "The identifier stays while the repository does not change."
}

func (m repositoryIDPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m repositoryIDPlanModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// A create has no prior value, and a destroy has no plan.
	if req.StateValue.IsNull() || req.Plan.Raw.IsNull() || !req.PlanValue.IsUnknown() {
		return
	}

	var prior, planned types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("repository"), &prior)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("repository"), &planned)...)
	if resp.Diagnostics.HasError() || planned.IsUnknown() {
		return
	}
	if strings.EqualFold(prior.ValueString(), planned.ValueString()) {
		resp.PlanValue = req.StateValue
	}
}
