package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

var (
	_ datasource.DataSource              = &cloudOrganizationDataSource{}
	_ datasource.DataSourceWithConfigure = &cloudOrganizationDataSource{}
)

// NewCloudOrganizationDataSource returns the sonarqube_cloud_organization data source.
func NewCloudOrganizationDataSource() datasource.DataSource {
	return &cloudOrganizationDataSource{}
}

type cloudOrganizationDataSource struct {
	client *client.Client
}

type cloudOrganizationDataSourceModel struct {
	Key         types.String `tfsdk:"key"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	URL         types.String `tfsdk:"url"`
	AvatarURL   types.String `tfsdk:"avatar_url"`
}

func (d *cloudOrganizationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_organization"
}

func (d *cloudOrganizationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads one SonarQube Cloud organization. Organizations exist in " +
			"SonarQube Cloud only.",
		Attributes: map[string]schema.Attribute{
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Key of the organization to read.",
			},
			"name": schema.StringAttribute{
				Computed:    true,
				Description: "Name of the organization.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Description of the organization.",
			},
			"url": schema.StringAttribute{
				Computed:    true,
				Description: "Address of the web page of the organization.",
			},
			"avatar_url": schema.StringAttribute{
				Computed:    true,
				Description: "Address of the avatar of the organization.",
			},
		},
	}
}

func (d *cloudOrganizationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, diagnostics := requireCloudClient(req.ProviderData,
		"The sonarqube_cloud_organization data source reads an organization")
	resp.Diagnostics.Append(diagnostics...)
	d.client = c
}

func (d *cloudOrganizationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config cloudOrganizationDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := config.Key.ValueString()

	org, ok := lookupOrganization(ctx, d.client, key, path.Root("key"), &resp.Diagnostics)
	if !ok {
		return
	}

	state := cloudOrganizationDataSourceModel{
		Key:         types.StringValue(org.Key),
		Name:        types.StringValue(org.Name),
		Description: types.StringValue(org.Description),
		URL:         types.StringValue(org.URL),
		AvatarURL:   types.StringValue(org.AvatarURL),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
