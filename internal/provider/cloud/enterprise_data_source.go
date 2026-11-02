package cloud

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
)

var (
	_ datasource.DataSource              = &enterpriseDataSource{}
	_ datasource.DataSourceWithConfigure = &enterpriseDataSource{}
)

// NewEnterpriseDataSource returns the sonarqube_cloud_enterprise data source.
func NewEnterpriseDataSource() datasource.DataSource {
	return &enterpriseDataSource{}
}

type enterpriseDataSource struct{ client *client.Client }

type enterpriseDataSourceModel struct {
	ID     types.String `tfsdk:"id"`
	Key    types.String `tfsdk:"key"`
	Name   types.String `tfsdk:"name"`
	Avatar types.String `tfsdk:"avatar"`
}

func (d *enterpriseDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_enterprise"
}

func (d *enterpriseDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads one SonarQube Cloud enterprise by its key. The provider must use a user token. " +
			"Scoped organization tokens are not supported.",
		Attributes: map[string]schema.Attribute{
			"key":    schema.StringAttribute{Required: true, MarkdownDescription: "Key of the enterprise to read."},
			"id":     schema.StringAttribute{Computed: true, MarkdownDescription: "Enterprise UUID."},
			"name":   schema.StringAttribute{Computed: true, MarkdownDescription: "Enterprise name."},
			"avatar": schema.StringAttribute{Computed: true, MarkdownDescription: "Address of the avatar image, or an empty string."},
		},
	}
}

func (d *enterpriseDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_cloud_enterprise data source reads an enterprise")
	resp.Diagnostics.Append(diagnostics...)
	d.client = c
}

func (d *enterpriseDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config enterpriseDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	enterprise, ok := lookupEnterprise(ctx, d.client, config.Key.ValueString(), &resp.Diagnostics)
	if !ok {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &enterpriseDataSourceModel{
		ID:     types.StringValue(enterprise.ID),
		Key:    types.StringValue(enterprise.Key),
		Name:   types.StringValue(enterprise.Name),
		Avatar: types.StringValue(enterprise.Avatar),
	})...)
}

// lookupEnterprise reads the enterprise that carries a key for the data
// source and for an import, and turns a failure into a diagnostic.
func lookupEnterprise(ctx context.Context, c *client.Client, key string, diagnostics *diag.Diagnostics) (*client.Enterprise, bool) {
	enterprise, err := c.GetEnterpriseByKey(ctx, key)
	if errors.Is(err, client.ErrNotFound) {
		diagnostics.AddAttributeError(path.Root("key"), "Enterprise "+key+" not found",
			"No enterprise with this key was found at "+c.APIURL()+". Check the key, and check that the token "+
				"user can read the enterprise. Scoped organization tokens are not supported.")
		return nil, false
	}
	if err != nil {
		diagnostics.AddError("Cannot read the enterprise "+key, err.Error())
		return nil, false
	}
	return enterprise, true
}
