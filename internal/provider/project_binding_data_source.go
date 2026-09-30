package provider

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

var (
	_ datasource.DataSource              = &projectBindingDataSource{}
	_ datasource.DataSourceWithConfigure = &projectBindingDataSource{}
)

// NewProjectBindingDataSource returns the sonarqube_project_binding data
// source.
func NewProjectBindingDataSource() datasource.DataSource {
	return &projectBindingDataSource{}
}

type projectBindingDataSource struct {
	client *client.Client
}

func (d *projectBindingDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_binding"
}

func (d *projectBindingDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the repository binding of a SonarQube Cloud project.",
		Attributes: map[string]schema.Attribute{
			"organization": schema.StringAttribute{
				Required:    true,
				Description: "Key of the organization that owns the project.",
			},
			"project_key": schema.StringAttribute{
				Required:    true,
				Description: "Key of the project whose binding is read.",
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Identifier of the binding.",
			},
			"repository": schema.StringAttribute{
				Computed:    true,
				Description: "Repository that the project is bound to, as `owner/name`.",
			},
			"repository_id": schema.StringAttribute{
				Computed:    true,
				Description: "Identifier that the DevOps platform gives the repository.",
			},
		},
	}
}

func (d *projectBindingDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, diagnostics := requireCloudClient(req.ProviderData,
		"The sonarqube_project_binding data source reads the binding of a project")
	resp.Diagnostics.Append(diagnostics...)
	d.client = c
}

func (d *projectBindingDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config projectBindingModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organization := config.Organization.ValueString()
	key := config.ProjectKey.ValueString()

	binding, err := findProjectBinding(ctx, d.client, organization, key)
	switch {
	case errors.Is(err, client.ErrNotFound):
		addProjectNotFound(&resp.Diagnostics, organization, key)
		return
	case errors.Is(err, client.ErrNotBound):
		resp.Diagnostics.AddError("The project "+key+" has no binding",
			"The project exists, but it is not bound to a repository.")
		return
	case err != nil:
		resp.Diagnostics.AddError("Cannot read the binding of the project "+key, err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, stateFromProjectBinding(binding, config))...)
}
