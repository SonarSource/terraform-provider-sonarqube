// Package provider contains the SonarQube Terraform provider implementation.
package provider

import (
	"cmp"
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// The SONARQUBE_ prefix is deliberate. SONAR_TOKEN is already set in many
// continuous integration jobs, where it holds an analysis token that cannot
// administer an organization.
const (
	envURL    = "SONARQUBE_URL"
	envAPIURL = "SONARQUBE_API_URL"
	envToken  = "SONARQUBE_TOKEN"
)

var _ provider.Provider = &sonarqubeProvider{}

type sonarqubeProvider struct {
	// version is set at build time: "dev" for local builds, the release tag
	// for released binaries.
	version string
}

// New returns the factory that main and the acceptance tests use to build
// the provider.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &sonarqubeProvider{
			version: version,
		}
	}
}

type providerModel struct {
	URL     types.String `tfsdk:"url"`
	APIURL  types.String `tfsdk:"api_url"`
	Token   types.String `tfsdk:"token"`
	Product types.String `tfsdk:"product"`
}

// Metadata sets the type name, which prefixes every resource and data source,
// for example "sonarqube_organization".
func (p *sonarqubeProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "sonarqube"
	resp.Version = p.version
}

func (p *sonarqubeProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage SonarQube Cloud resources with Terraform.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Optional: true,
				Description: "Base address of the instance, for example `" + client.CloudURL +
					"`. Can also be given with the `" + envURL + "` environment variable. " +
					"Defaults to `" + client.CloudURL + "` when `product` is `cloud`.",
			},
			"api_url": schema.StringAttribute{
				Optional: true,
				Description: "Base address of Web API v2, for example " +
					"`https://api.sonarcloud.io`. Can also be given with the `" + envAPIURL +
					"` environment variable. Derived from `url` when absent, which is right " +
					"for every standard deployment.",
			},
			"token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "Token that authenticates against the instance. Can also be " +
					"given with the `" + envToken + "` environment variable.",
			},
			"product": schema.StringAttribute{
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf(string(client.ProductCloud), string(client.ProductServer)),
				},
				Description: "SonarQube product to manage: `cloud` or `server`. Defaults to " +
					"`cloud`. Only `cloud` works in this release.",
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
	addUnknownError(resp, config.URL, "url", envURL)
	addUnknownError(resp, config.APIURL, "api_url", envAPIURL)
	addUnknownError(resp, config.Token, "token", envToken)
	addUnknownError(resp, config.Product, "product", "")
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

	instanceURL := cmp.Or(config.URL.ValueString(), os.Getenv(envURL), client.CloudURL)
	apiURL := cmp.Or(config.APIURL.ValueString(), os.Getenv(envAPIURL))
	token := cmp.Or(config.Token.ValueString(), os.Getenv(envToken))

	// url.Parse accepts an address with no scheme and leaves the host empty,
	// so an address such as "sonarcloud.io" would fail on the first request
	// with a message that names neither the attribute nor the value.
	addressError(resp, instanceURL, "url", envURL)
	if apiURL != "" {
		addressError(resp, apiURL, "api_url", envAPIURL)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Missing token",
			"Set the token attribute of the provider, or the "+envToken+
				" environment variable.",
		)
		return
	}

	c := client.New(client.Config{
		URL:     instanceURL,
		APIURL:  apiURL,
		Token:   token,
		Product: product,
	})

	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *sonarqubeProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{}
}

func (p *sonarqubeProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewOrganizationDataSource,
	}
}

// addUnknownError takes an empty envVar for an attribute that no environment
// variable can supply.
func addUnknownError(resp *provider.ConfigureResponse, value types.String, attribute, envVar string) {
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
