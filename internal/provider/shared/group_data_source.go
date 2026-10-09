package shared

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
)

var (
	_ datasource.DataSource              = &groupDataSource{}
	_ datasource.DataSourceWithConfigure = &groupDataSource{}
)

// NewGroupDataSource returns the sonarqube_group data source.
func NewGroupDataSource() datasource.DataSource { return &groupDataSource{} }

type groupDataSource struct{ client *client.Client }

type groupDataSourceModel struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	Default      types.Bool   `tfsdk:"default"`
}

func (d *groupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (d *groupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads a group by its exact name, including a built-in group. The alpha supports SonarQube Cloud only.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Opaque group identifier."},
			"organization": schema.StringAttribute{Required: true, Description: "Key of the organization that owns the group.",
				Validators: organizationKeyValidators()},
			"name":        schema.StringAttribute{Required: true, Description: "Exact group name."},
			"description": schema.StringAttribute{Computed: true, Description: "Group description."},
			"default":     schema.BoolAttribute{Computed: true, Description: "Whether this is the default group for new members."},
		},
	}
}

func (d *groupDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_group data source reads a group")
	resp.Diagnostics.Append(diagnostics...)
	d.client = c
}

func (d *groupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config groupDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	group, err := d.client.FindGroupByName(ctx, config.Organization.ValueString(), config.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot find the group "+config.Name.ValueString(),
			groupLookupDetail(err, config.Organization.ValueString()))
		return
	}
	config.ID = types.StringValue(group.ID)
	config.Description = types.StringValue(group.Description)
	config.Default = types.BoolValue(group.Default)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
