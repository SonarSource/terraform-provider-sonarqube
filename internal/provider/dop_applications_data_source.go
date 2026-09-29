package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

var (
	_ datasource.DataSource              = &dopApplicationsDataSource{}
	_ datasource.DataSourceWithConfigure = &dopApplicationsDataSource{}
)

// NewDopApplicationsDataSource returns the sonarqube_dop_applications data
// source.
func NewDopApplicationsDataSource() datasource.DataSource {
	return &dopApplicationsDataSource{}
}

type dopApplicationsDataSource struct {
	client *client.Client
}

type dopApplicationsModel struct {
	DevOpsPlatform types.String          `tfsdk:"dev_ops_platform"`
	Applications   []dopApplicationModel `tfsdk:"applications"`
}

type dopApplicationModel struct {
	ID             types.String `tfsdk:"id"`
	DevOpsPlatform types.String `tfsdk:"dev_ops_platform"`
	ApplicationKey types.String `tfsdk:"application_key"`
	BindingType    types.String `tfsdk:"binding_type"`
}

func (d *dopApplicationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dop_applications"
}

func (d *dopApplicationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the applications that this SonarQube Cloud instance owns on the " +
			"DevOps platforms.\n\n" +
			"A binding to github.com accepts an installation of one of these applications " +
			"only: the server looks an installation up in its own records instead of asking " +
			"GitHub, so an installation of a different application means nothing to it. " +
			"Install the application from `https://github.com/apps/<application_key>`.",
		Attributes: map[string]schema.Attribute{
			"dev_ops_platform": schema.StringAttribute{
				Optional: true,
				Description: "Report the applications of one platform only. Only `" +
					client.PlatformGitHub + "` is supported.",
				Validators: []validator.String{
					stringvalidator.OneOf(client.PlatformGitHub),
				},
			},
			"applications": schema.ListNestedAttribute{
				Computed:    true,
				Description: "The applications that the instance owns.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Identifier of the application.",
						},
						"dev_ops_platform": schema.StringAttribute{
							Computed:    true,
							Description: "Platform that the application belongs to.",
						},
						"application_key": schema.StringAttribute{
							Computed: true,
							Description: "Key of the application. For GitHub this is the last " +
								"segment of `https://github.com/apps/<application_key>`.",
						},
						"binding_type": schema.StringAttribute{
							Computed:    true,
							Description: "Type of binding that the application serves.",
						},
					},
				},
			},
		},
	}
}

func (d *dopApplicationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, diagnostics := requireCloudClient(req.ProviderData,
		"The sonarqube_dop_applications data source reads the applications of an instance")
	resp.Diagnostics.Append(diagnostics...)
	d.client = c
}

func (d *dopApplicationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config dopApplicationsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	applications, err := d.client.ListDopApplications(ctx, config.DevOpsPlatform.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Cannot read the DevOps platform applications",
			"The provider could not read the applications of the instance: "+err.Error(),
		)
		return
	}

	// An instance with no application gives an empty list rather than no
	// value, so that a configuration can count the result without a test for
	// null first.
	state := dopApplicationsModel{
		DevOpsPlatform: config.DevOpsPlatform,
		Applications:   make([]dopApplicationModel, 0, len(applications)),
	}
	for _, application := range applications {
		state.Applications = append(state.Applications, dopApplicationModel{
			ID:             nullIfEmpty(application.ID),
			DevOpsPlatform: nullIfEmpty(application.DevOpsPlatform),
			ApplicationKey: nullIfEmpty(application.ApplicationKey),
			BindingType:    nullIfEmpty(application.BindingType),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
