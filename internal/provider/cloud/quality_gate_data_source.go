package cloud

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
)

var (
	_ datasource.DataSource              = &qualityGateDataSource{}
	_ datasource.DataSourceWithConfigure = &qualityGateDataSource{}
)

func NewQualityGateDataSource() datasource.DataSource { return &qualityGateDataSource{} }

type qualityGateDataSource struct{ client *client.Client }

type qualityGateDataSourceModel struct {
	ID              types.String `tfsdk:"id"`
	Organization    types.String `tfsdk:"organization"`
	Name            types.String `tfsdk:"name"`
	AICodeAssurance types.Bool   `tfsdk:"ai_code_assurance"`
	Conditions      types.Set    `tfsdk:"condition"`
}

func (d *qualityGateDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_quality_gate"
}

func (d *qualityGateDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads one SonarQube Cloud quality gate by name in an organization.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Computed: true, Description: "UUID of the gate."},
			"organization":      schema.StringAttribute{Required: true, Description: "Key of the organization that owns the gate."},
			"name":              schema.StringAttribute{Required: true, Description: "Exact name of the gate."},
			"ai_code_assurance": schema.BoolAttribute{Computed: true, Description: "Whether AI Code Assurance qualifies the gate."},
			"condition": schema.SetNestedAttribute{Computed: true, Description: "Conditions of the gate.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"metric":    schema.StringAttribute{Computed: true, Description: "Metric key."},
				"operator":  schema.StringAttribute{Computed: true, Description: "Comparison operator: LT (less than) or GT (greater than)."},
				"threshold": schema.StringAttribute{Computed: true, Description: "Threshold value."},
			}}},
		},
	}
}

func (d *qualityGateDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_cloud_quality_gate data source reads a quality gate")
	resp.Diagnostics.Append(diagnostics...)
	d.client = c
}

func (d *qualityGateDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model qualityGateDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	org, ok := lookupOrganization(ctx, d.client, model.Organization.ValueString(), path.Root("organization"), &resp.Diagnostics)
	if !ok {
		return
	}
	found, err := d.client.FindQualityGate(ctx, org.UUIDV4, model.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot find the quality gate", err.Error())
		return
	}
	gate, conditions, err := loadQualityGate(ctx, d.client, found.ID)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the quality gate", err.Error())
		return
	}
	model.ID = types.StringValue(gate.ID)
	model.Name = types.StringValue(gate.Name)
	model.AICodeAssurance = types.BoolValue(gate.AIQualified)
	conditionSet, setDiagnostics := types.SetValueFrom(ctx, qualityGateConditionType(), conditions)
	resp.Diagnostics.Append(setDiagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	model.Conditions = conditionSet
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
