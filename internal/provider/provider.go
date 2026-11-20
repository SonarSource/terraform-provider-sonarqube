// Package provider contains the SonarQube Terraform provider implementation.
package provider

import (
	"cmp"
	"context"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/cloud"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/shared"
)

// The environment variables that the provider reads. The acceptance tests
// read them too.
//
// The SONARQUBE_ prefix is deliberate. SONAR_TOKEN is already set in many
// continuous integration jobs, where it holds an analysis token that cannot
// administer an organization.
const (
	EnvURL    = "SONARQUBE_URL"
	EnvAPIURL = "SONARQUBE_API_URL"
	EnvToken  = "SONARQUBE_TOKEN" //nolint:gosec // G101: the name of an environment variable, not a credential

	// envAppendUserAgent is the Terraform convention for text at the end of
	// the User-Agent header.
	envAppendUserAgent = "TF_APPEND_USER_AGENT"
)

// providerTypeName prefixes every resource and data source type name.
const providerTypeName = "sonarqube"

var _ provider.Provider = &sonarqubeProvider{}

type sonarqubeProvider struct {
	// version is set at build time: "dev" for local builds, the release tag
	// for released binaries.
	version string
	// checkToken makes a call that each valid token can make. Unit tests
	// replace it, so that Configure sends no request.
	checkToken func(context.Context, *client.Client) error
}

// tokenSettings names the two places that give the token.
const tokenSettings = "the token attribute of the provider, or the " + EnvToken + " environment variable"

// tokenCheckTimeout limits the token check, so that a slow API does not hold
// the start of each run.
const tokenCheckTimeout = 10 * time.Second

// New returns the factory that main and the acceptance tests use to build
// the provider.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &sonarqubeProvider{
			version: version,
			checkToken: func(ctx context.Context, c *client.Client) error {
				_, err := c.ListEnterprises(ctx)
				return err
			},
		}
	}
}

type providerModel struct {
	URL     types.String `tfsdk:"url"`
	APIURL  types.String `tfsdk:"api_url"`
	Token   types.String `tfsdk:"token"`
	Product types.String `tfsdk:"product"`

	DisableTelemetry types.Bool `tfsdk:"disable_telemetry"`
}

// Metadata sets the type name, which prefixes every resource and data source,
// for example "sonarqube_cloud_organization".
func (p *sonarqubeProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = providerTypeName
	resp.Version = p.version
}

func (p *sonarqubeProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage SonarQube Cloud resources with Terraform.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Optional: true,
				Description: "Base address of the instance, for example `" + client.CloudURL +
					"`. Can also be given with the `" + EnvURL + "` environment variable. " +
					"Defaults to `" + client.CloudURL + "` when `product` is `cloud`.",
			},
			"api_url": schema.StringAttribute{
				Optional: true,
				Description: "Base address of Web API v2, for example " +
					"`https://api.sonarcloud.io`. Can also be given with the `" + EnvAPIURL +
					"` environment variable. Derived from `url` when absent, which is right " +
					"for every standard deployment.",
			},
			"token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "Token that authenticates against the instance. Can also be " +
					"given with the `" + EnvToken + "` environment variable.",
			},
			"product": schema.StringAttribute{
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf(string(client.ProductCloud), string(client.ProductServer)),
				},
				Description: "SonarQube product to manage: `cloud` or `server`. Defaults to " +
					"`cloud`. Only `cloud` works in this release.",
			},
			"disable_telemetry": schema.BoolAttribute{
				Optional: true,
				Description: "Set to `true` to turn off the optional telemetry. The `" +
					EnvDisableTelemetry + "` environment variable and `" + envDoNotTrack +
					"=1` also turn it off. Defaults to `false`.",
			},
		},
	}
}

func (p *sonarqubeProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// An unknown value comes from another resource that must be applied first.
	// Falling back to the environment would authenticate as somebody else.
	addUnknownError(resp, config.URL, "url", EnvURL)
	addUnknownError(resp, config.APIURL, "api_url", EnvAPIURL)
	addUnknownError(resp, config.Token, "token", EnvToken)
	addUnknownError(resp, config.Product, "product", "")
	addUnknownError(resp, config.DisableTelemetry, "disable_telemetry", EnvDisableTelemetry)
	if resp.Diagnostics.HasError() {
		return
	}

	product := client.Product(config.Product.ValueString())
	if product == "" {
		product = client.ProductCloud
	}
	if product != client.ProductCloud {
		resp.Diagnostics.AddAttributeError(
			path.Root("product"),
			"SonarQube Server is not supported",
			"This release of the provider manages SonarQube Cloud only. Remove the "+
				"product attribute, or set it to \"cloud\".",
		)
		return
	}

	instanceURL := cmp.Or(config.URL.ValueString(), os.Getenv(EnvURL), client.CloudURL)
	apiURL := cmp.Or(config.APIURL.ValueString(), os.Getenv(EnvAPIURL))
	token := cmp.Or(config.Token.ValueString(), os.Getenv(EnvToken))

	// url.Parse accepts an address with no scheme and leaves the host empty,
	// so an address such as "sonarcloud.io" would fail on the first request
	// with a message that names neither the attribute nor the value.
	addressError(resp, instanceURL, "url", EnvURL)
	if apiURL != "" {
		addressError(resp, apiURL, "api_url", EnvAPIURL)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	telemetry, diags := telemetryEnabled(config.DisableTelemetry, os.Getenv)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Missing token",
			"Set "+tokenSettings+".",
		)
		return
	}

	c := client.New(client.Config{
		URL:       instanceURL,
		APIURL:    apiURL,
		Token:     token,
		Product:   product,
		UserAgent: userAgent(req.TerraformVersion, p.version, os.Getenv(envAppendUserAgent)),

		Telemetry:      telemetry,
		TelemetryTypes: p.telemetryTypes(ctx),
	})

	// Each API answers a wrong token in a different way, so one check here
	// gives the same error for each resource. Another failure of the check
	// does not stop Configure: the calls of the resources report it.
	checkCtx, cancel := context.WithTimeout(ctx, tokenCheckTimeout)
	defer cancel()
	if err := p.checkToken(checkCtx, c); client.IsInvalidToken(err) {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Invalid token",
			client.InvalidTokenHint+" Check "+tokenSettings+".",
		)
		return
	}

	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *sonarqubeProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		shared.NewGroupResource,
		shared.NewGroupMemberResource,
		shared.NewQualityProfileResource,
		shared.NewQualityProfileRuleResource,
		shared.NewProjectQualityProfileResource,
		shared.NewQualityProfileEditorResource,
		cloud.NewOrganizationResource,
		cloud.NewOrganizationPermissionsResource,
		cloud.NewProjectPermissionsResource,
		cloud.NewOrganizationQualityGateSettingsResource,
		cloud.NewOrganizationAutomaticAnalysisResource,
		cloud.NewOrganizationBindingResource,
		cloud.NewOrganizationGitLabSelfManagedBindingResource,
		shared.NewProjectResource,
		cloud.NewProjectBindingResource,
		cloud.NewQualityGateResource,
		cloud.NewOrganizationDefaultQualityGateResource,
		cloud.NewOrganizationDefaultQualityProfileResource,
		cloud.NewProjectQualityGateResource,
		cloud.NewProjectAutomaticAnalysisResource,
		cloud.NewScopedOrganizationTokenResource,
		cloud.NewEnterpriseResource,
		cloud.NewEnterpriseOrganizationResource,
	}
}

func (p *sonarqubeProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		shared.NewGroupDataSource,
		shared.NewQualityProfileDataSource,
		cloud.NewOrganizationDataSource,
		cloud.NewOrganizationBindingDataSource,
		cloud.NewDopApplicationsDataSource,
		cloud.NewProjectBindingDataSource,
		cloud.NewQualityGateDataSource,
		cloud.NewEnterpriseDataSource,
	}
}

// addUnknownError takes an empty envVar for an attribute that no environment
// variable can supply.
func addUnknownError(resp *provider.ConfigureResponse, value attr.Value, attribute, envVar string) {
	if !value.IsUnknown() {
		return
	}

	detail := "The value of " + attribute + " is not known while the provider is configured. " +
		"Give it a static value"
	if envVar != "" {
		detail += ", or use the " + envVar + " environment variable"
	}

	resp.Diagnostics.AddAttributeError(
		path.Root(attribute),
		"Unknown value for "+attribute,
		detail+".",
	)
}

// addressError reports an address that cannot reach an instance.
func addressError(resp *provider.ConfigureResponse, value, attribute, envVar string) {
	err := client.ValidateURL(value)
	if err == nil {
		return
	}

	resp.Diagnostics.AddAttributeError(
		path.Root(attribute),
		"Invalid address in "+attribute,
		err.Error()+". Give a full address, for example \"https://sonarcloud.io\", "+
			"in the "+attribute+" attribute or in the "+envVar+" environment variable.",
	)
}
