package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

var (
	_ datasource.DataSource              = &organizationBindingDataSource{}
	_ datasource.DataSourceWithConfigure = &organizationBindingDataSource{}
)

// NewOrganizationBindingDataSource returns the
// sonarqube_organization_binding data source.
func NewOrganizationBindingDataSource() datasource.DataSource {
	return &organizationBindingDataSource{}
}

type organizationBindingDataSource struct {
	client *client.Client
}

func (d *organizationBindingDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_binding"
}

func (d *organizationBindingDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the DevOps platform binding of a SonarQube Cloud organization.",
		Attributes: map[string]schema.Attribute{
			"organization_key": schema.StringAttribute{
				Required:    true,
				Description: "Key of the organization whose binding is read.",
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Identifier of the binding.",
			},
			"organization_id": schema.StringAttribute{
				Computed:    true,
				Description: "Internal identifier of the bound organization.",
			},
			"organization_uuid_v4": schema.StringAttribute{
				Computed:    true,
				Description: "Identifier of the bound organization in the form of a UUID.",
			},
			"dev_ops_platform": schema.StringAttribute{
				Computed:    true,
				Description: "DevOps platform that the organization is bound to.",
			},
			"installation_id": schema.StringAttribute{
				Computed:    true,
				Description: "Identifier of the application installation.",
			},
			"dev_ops_platform_url": schema.StringAttribute{
				Computed:    true,
				Description: "Address of the organization on the DevOps platform.",
			},
			"repo_auto_import_enabled": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether repositories are imported automatically.",
			},
			"dop_flavor": schema.StringAttribute{
				Computed: true,
				Description: "Variant of the binding, such as `GITHUB_GHEC_DR`. Holds no value " +
					"for a binding to github.com.",
			},
			"binding_type": schema.StringAttribute{
				Computed:    true,
				Description: "Type of the binding, as the API reports it.",
			},
		},
	}
}

func (d *organizationBindingDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, diagnostics := requireCloudClient(req.ProviderData,
		"The sonarqube_organization_binding data source reads the binding of an organization")
	resp.Diagnostics.Append(diagnostics...)
	d.client = c
}

func (d *organizationBindingDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	// The data source reports the same fields as the resource, so it reads
	// into the same model.
	var config organizationBindingModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationKey := config.OrganizationKey.ValueString()

	org, ok := resolveOrganization(ctx, d.client, organizationKey, &resp.Diagnostics)
	if !ok {
		return
	}

	binding, ok := findOrganizationBinding(ctx, d.client, org, "", &resp.Diagnostics)
	if !ok {
		return
	}

	state := stateFromBinding(binding, config)
	state.OrganizationKey = types.StringValue(organizationKey)
	// This data source reads a binding of any platform, and a binding to
	// GitLab or to Azure DevOps carries no installation. The attribute is
	// computed here, so it holds no value rather than an empty string. The
	// resource requires the attribute and must keep it filled, which is why
	// stateFromBinding does not do this.
	state.InstallationID = nullIfEmpty(binding.InstallationID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
