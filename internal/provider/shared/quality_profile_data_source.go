package shared

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/validate"
)

var (
	_ datasource.DataSource              = &qualityProfileDataSource{}
	_ datasource.DataSourceWithConfigure = &qualityProfileDataSource{}
)

// NewQualityProfileDataSource returns the sonarqube_quality_profile data source.
func NewQualityProfileDataSource() datasource.DataSource { return &qualityProfileDataSource{} }

type qualityProfileDataSource struct{ client *client.Client }

type qualityProfileDataSourceModel struct {
	ID           types.String `tfsdk:"id"`
	Key          types.String `tfsdk:"key"`
	Organization types.String `tfsdk:"organization"`
	Language     types.String `tfsdk:"language"`
	Name         types.String `tfsdk:"name"`
	IsBuiltIn    types.Bool   `tfsdk:"is_built_in"`
	IsDefault    types.Bool   `tfsdk:"is_default"`
	ParentKey    types.String `tfsdk:"parent_key"`
}

func (d *qualityProfileDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_quality_profile"
}

func (d *qualityProfileDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a quality profile of one language by its exact name, also a built-in profile such as " +
			"`Sonar way core`. Use its `key` as the `parent_key` of a `sonarqube_quality_profile`. The alpha " +
			"supports SonarQube Cloud only.",
		Attributes: map[string]schema.Attribute{
			"id":  schema.StringAttribute{Computed: true, Description: "Key of the profile."},
			"key": schema.StringAttribute{Computed: true, Description: "Key of the profile."},
			"organization": schema.StringAttribute{Required: true, Description: "Key of the organization that owns the profile.",
				Validators: validate.OrganizationKey()},
			"language": schema.StringAttribute{Required: true, Description: "Key of the language of the profile, such as `java`.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"name": schema.StringAttribute{Required: true, Description: "Exact name of the profile, with the same case.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 100)}},
			"is_built_in": schema.BoolAttribute{Computed: true, Description: "Whether the profile is a read-only built-in profile."},
			"is_default":  schema.BoolAttribute{Computed: true, Description: "Whether the profile is the default profile of the language."},
			"parent_key": schema.StringAttribute{Computed: true,
				Description: "Key of the profile that this profile extends. Null when it extends no other profile."},
		},
	}
}

func (d *qualityProfileDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_quality_profile data source reads a quality profile")
	resp.Diagnostics.Append(diagnostics...)
	d.client = c
}

func (d *qualityProfileDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config qualityProfileDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	organization, language := config.Organization.ValueString(), config.Language.ValueString()
	profile, err := d.client.FindQualityProfileByName(ctx, organization, language, config.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot find the quality profile "+config.Name.ValueString(),
			profileLookupDetail(err, organization, language))
		return
	}
	config.ID, config.Key = types.StringValue(profile.Key), types.StringValue(profile.Key)
	config.IsBuiltIn = types.BoolValue(profile.IsBuiltIn)
	config.IsDefault = types.BoolValue(profile.IsDefault)
	config.ParentKey = optionalString(profile.ParentKey)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
