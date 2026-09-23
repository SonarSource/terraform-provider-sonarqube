package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

var (
	_ datasource.DataSource              = &organizationDataSource{}
	_ datasource.DataSourceWithConfigure = &organizationDataSource{}
)

// NewOrganizationDataSource returns the sonarqube_organization data source.
func NewOrganizationDataSource() datasource.DataSource {
	return &organizationDataSource{}
}

type organizationDataSource struct {
	client *client.Client
}

type organizationDataSourceModel struct {
	Key         types.String `tfsdk:"key"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	URL         types.String `tfsdk:"url"`
	AvatarURL   types.String `tfsdk:"avatar_url"`
}

func (d *organizationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (d *organizationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
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

func (d *organizationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	// The framework calls Configure with no data while it validates the
	// configuration, before the provider itself is configured.
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *client.Client, got %T. This is a fault in the provider.", req.ProviderData),
		)
		return
	}

	if !c.IsCloud() {
		resp.Diagnostics.AddError(
			"Organizations need SonarQube Cloud",
			"The sonarqube_organization data source reads an organization, which "+
				"exists in SonarQube Cloud only.",
		)
		return
	}

	d.client = c
}

func (d *organizationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config organizationDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := config.Key.ValueString()

	org, err := d.client.GetOrganization(ctx, key)
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			// An organization that the token may not see answers 404 as well.
			resp.Diagnostics.AddError(
				"Organization "+key+" not found",
				"No organization with this key was found at "+d.client.APIURL()+
					". Check the key, and check that the token can read the organization.",
			)
			return
		}
		resp.Diagnostics.AddError(
			"Cannot read organization "+key,
			"The provider could not read the organization: "+err.Error(),
		)
		return
	}

	state := organizationDataSourceModel{
		Key:         types.StringValue(org.Key),
		Name:        types.StringValue(org.Name),
		Description: types.StringValue(org.Description),
		URL:         types.StringValue(org.URL),
		AvatarURL:   types.StringValue(org.AvatarURL),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
