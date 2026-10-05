package cloud

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
)

var (
	_ resource.Resource                   = &qualityGateResource{}
	_ resource.ResourceWithConfigure      = &qualityGateResource{}
	_ resource.ResourceWithImportState    = &qualityGateResource{}
	_ resource.ResourceWithValidateConfig = &qualityGateResource{}
)

func NewQualityGateResource() resource.Resource { return &qualityGateResource{} }

type qualityGateResource struct{ client *client.Client }

type qualityGateConditionModel struct {
	Metric    types.String `tfsdk:"metric"`
	Operator  types.String `tfsdk:"operator"`
	Threshold types.String `tfsdk:"threshold"`
}

type qualityGateResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	Name         types.String `tfsdk:"name"`
	AIQualified  types.Bool   `tfsdk:"ai_qualified"`
	Conditions   types.Set    `tfsdk:"condition"`
}

func qualityGateConditionType() attr.Type {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"metric": types.StringType, "operator": types.StringType, "threshold": types.StringType,
	}}
}

func (r *qualityGateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_quality_gate"
}

func (r *qualityGateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages a SonarQube Cloud quality gate and all its conditions. The built-in Sonar way gate cannot be managed.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Computed: true, Description: "UUID of the gate.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"organization": schema.StringAttribute{Required: true, Description: "Key of the organization that owns the gate.", PlanModifiers: replace},
			"name":         schema.StringAttribute{Required: true, Description: "Name of the gate.", Validators: []validator.String{stringvalidator.LengthBetween(1, 255)}},
			"ai_qualified": schema.BoolAttribute{Optional: true, Computed: true, Description: "Whether AI Code Assurance qualifies the gate. To set true, the organization must have the AI Code Assurance feature."},
		},
		Blocks: map[string]schema.Block{
			"condition": schema.SetNestedBlock{Description: "Conditions owned by this gate. Each metric may appear once.", NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
				"metric":    schema.StringAttribute{Required: true, Description: "Metric key, such as new_coverage."},
				"operator":  schema.StringAttribute{Required: true, Description: "Comparison operator: LT (less than) or GT (greater than). The metric sets which of the two is valid.", Validators: []validator.String{stringvalidator.OneOf("LT", "GT")}},
				"threshold": schema.StringAttribute{Required: true, Description: "Threshold value accepted by SonarQube Cloud."},
			}}},
		},
	}
}

func (r *qualityGateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_cloud_quality_gate resource manages a quality gate")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *qualityGateResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var conditions types.Set
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("condition"), &conditions)...)
	if !resp.Diagnostics.HasError() {
		validateConditionMetrics(ctx, conditions, &resp.Diagnostics)
	}
}

func validateConditionMetrics(ctx context.Context, conditions types.Set, diagnostics *diag.Diagnostics) {
	if conditions.IsNull() || conditions.IsUnknown() {
		return
	}
	var models []qualityGateConditionModel
	diagnostics.Append(conditions.ElementsAs(ctx, &models, false)...)
	if diagnostics.HasError() {
		return
	}
	seen := make(map[string]bool, len(models))
	for _, model := range models {
		if model.Metric.IsNull() || model.Metric.IsUnknown() {
			continue
		}
		key := model.Metric.ValueString()
		if seen[key] {
			diagnostics.AddError("Duplicate quality gate metric", fmt.Sprintf("Metric %q appears in more than one condition. Each metric may have one condition.", key))
			return
		}
		seen[key] = true
	}
}

func (r *qualityGateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan qualityGateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateConditionMetrics(ctx, plan.Conditions, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	org, ok := lookupOrganization(ctx, r.client, plan.Organization.ValueString(), path.Root("organization"), &resp.Diagnostics)
	if !ok {
		return
	}
	gate, err := r.client.CreateQualityGate(ctx, org.UUIDV4, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the quality gate", err.Error())
		return
	}
	if gate.ID == "" {
		resp.Diagnostics.AddError("Cannot create the quality gate", "The API returned no gate identifier.")
		return
	}
	plan.ID = types.StringValue(gate.ID)
	// An unknown value means that the configuration does not set the flag. Then
	// the gate keeps the value that the API gives it.
	writeAIQualified := !plan.AIQualified.IsUnknown() && plan.AIQualified.ValueBool() != gate.AIQualified
	wantAIQualified := plan.AIQualified.ValueBool()
	// Until the write below succeeds, the state holds the flag that the new gate has.
	plan.AIQualified = types.BoolValue(gate.AIQualified)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if writeAIQualified && !r.setAIQualified(ctx, gate.ID, wantAIQualified, &resp.Diagnostics) {
		return
	}
	if !r.reconcileConditions(ctx, gate.ID, plan.Conditions, &resp.Diagnostics) {
		return
	}
	r.readIntoState(ctx, plan, &resp.State, &resp.Diagnostics, false)
}

func (r *qualityGateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state qualityGateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.readIntoState(ctx, state, &resp.State, &resp.Diagnostics, true)
}

func (r *qualityGateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior qualityGateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateConditionMetrics(ctx, plan.Conditions, &resp.Diagnostics)
	if resp.Diagnostics.HasError() || !r.ensureMutableGate(ctx, prior.ID.ValueString(), &resp.Diagnostics, false) {
		return
	}
	id := prior.ID.ValueString()
	if plan.Name.ValueString() != prior.Name.ValueString() {
		if err := r.client.UpdateQualityGate(ctx, id, map[string]any{"name": plan.Name.ValueString()}); err != nil {
			resp.Diagnostics.AddError("Cannot rename the quality gate", err.Error())
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), plan.Name)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	if !plan.AIQualified.IsUnknown() && plan.AIQualified.ValueBool() != prior.AIQualified.ValueBool() {
		if !r.setAIQualified(ctx, id, plan.AIQualified.ValueBool(), &resp.Diagnostics) {
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ai_qualified"), plan.AIQualified)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	if !r.reconcileConditions(ctx, id, plan.Conditions, &resp.Diagnostics) {
		return
	}
	r.readIntoState(ctx, plan, &resp.State, &resp.Diagnostics, false)
}

func (r *qualityGateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state qualityGateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !r.ensureMutableGate(ctx, state.ID.ValueString(), &resp.Diagnostics, true) {
		return
	}
	if err := r.client.DeleteQualityGate(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot delete the quality gate", err.Error())
	}
}

func (r *qualityGateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	organization, name, ok := strings.Cut(req.ID, "/")
	if !ok || organization == "" || name == "" {
		resp.Diagnostics.AddError("Invalid quality gate import ID", "Use <organization>/<name>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}

func (r *qualityGateResource) readIntoState(ctx context.Context, prior qualityGateResourceModel, state *tfsdk.State, diagnostics *diag.Diagnostics, removeMissing bool) {
	id := prior.ID.ValueString()
	if id == "" {
		org, ok := lookupOrganization(ctx, r.client, prior.Organization.ValueString(), path.Root("organization"), diagnostics)
		if !ok {
			return
		}
		gate, err := r.client.FindQualityGate(ctx, org.UUIDV4, prior.Name.ValueString())
		if err != nil {
			diagnostics.AddError("Cannot find the quality gate", err.Error())
			return
		}
		id = gate.ID
	}
	gate, models, err := loadQualityGate(ctx, r.client, id)
	if gate != nil && gate.BuiltIn {
		diagnostics.AddError("Cannot manage built-in quality gate", "The built-in Sonar way gate cannot be managed by this resource.")
		return
	}
	if err != nil {
		if removeMissing && gate == nil && errors.Is(err, client.ErrNotFound) {
			state.RemoveResource(ctx)
			return
		}
		diagnostics.AddError("Cannot read the quality gate", err.Error())
		return
	}
	updated := prior
	updated.ID = types.StringValue(gate.ID)
	updated.Name = types.StringValue(gate.Name)
	updated.AIQualified = types.BoolValue(gate.AIQualified)
	if len(models) > 0 || !prior.Conditions.IsNull() {
		var setDiagnostics diag.Diagnostics
		updated.Conditions, setDiagnostics = types.SetValueFrom(ctx, qualityGateConditionType(), models)
		diagnostics.Append(setDiagnostics...)
		if diagnostics.HasError() {
			return
		}
	}
	diagnostics.Append(state.Set(ctx, &updated)...)
}

// setAIQualified writes the flag and reads it back. For an organization
// without the AI Code Assurance feature, the API accepts true but returns false.
func (r *qualityGateResource) setAIQualified(ctx context.Context, id string, want bool, diagnostics *diag.Diagnostics) bool {
	if err := r.client.UpdateQualityGate(ctx, id, map[string]any{"aiQualified": want}); err != nil {
		diagnostics.AddError("Cannot set the AI qualification", err.Error())
		return false
	}
	gate, err := r.client.GetQualityGate(ctx, id)
	if err != nil {
		diagnostics.AddError("Cannot read the AI qualification", err.Error())
		return false
	}
	if gate.AIQualified != want {
		diagnostics.AddAttributeError(path.Root("ai_qualified"), "AI qualification is not available",
			fmt.Sprintf("SonarQube Cloud did not set the AI qualification to %t on the quality gate. Make sure that the organization has the AI Code Assurance feature, or remove ai_qualified.", want))
		return false
	}
	return true
}

func (r *qualityGateResource) ensureMutableGate(ctx context.Context, id string, diagnostics *diag.Diagnostics, allowMissing bool) bool {
	gate, err := r.client.GetQualityGate(ctx, id)
	if err != nil {
		if allowMissing && errors.Is(err, client.ErrNotFound) {
			return true
		}
		diagnostics.AddError("Cannot read the quality gate", err.Error())
		return false
	}
	if gate.BuiltIn {
		diagnostics.AddError("Cannot manage built-in quality gate", "The built-in Sonar way gate cannot be managed by this resource.")
		return false
	}
	return true
}

func loadQualityGate(ctx context.Context, c *client.Client, id string) (*client.QualityGate, []qualityGateConditionModel, error) {
	gate, err := c.GetQualityGate(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	conditions, err := c.ListQualityGateConditions(ctx, id)
	if err != nil {
		return gate, nil, fmt.Errorf("Cannot read quality gate conditions: %w", err)
	}
	models := make([]qualityGateConditionModel, 0, len(conditions))
	if len(conditions) == 0 {
		return gate, models, nil
	}
	keys, err := c.ListMetrics(ctx)
	if err != nil {
		return gate, nil, fmt.Errorf("Cannot read metric keys: %w", err)
	}
	for _, condition := range conditions {
		key, found := keys[condition.LegacyMetricID]
		if !found {
			return gate, nil, fmt.Errorf("Cannot read quality gate condition: No metric key matches legacy metric identifier %d", condition.LegacyMetricID)
		}
		models = append(models, qualityGateConditionModel{
			Metric: types.StringValue(key), Operator: types.StringValue(condition.Operator), Threshold: types.StringValue(condition.Threshold),
		})
	}
	return gate, models, nil
}

func (r *qualityGateResource) desiredConditions(ctx context.Context, desired types.Set, diagnostics *diag.Diagnostics) map[int]qualityGateConditionModel {
	var models []qualityGateConditionModel
	if !desired.IsNull() {
		if desired.IsUnknown() {
			diagnostics.AddError("Unknown quality gate conditions", "Condition values must be known during apply.")
			return nil
		}
		diagnostics.Append(desired.ElementsAs(ctx, &models, false)...)
		if diagnostics.HasError() {
			return nil
		}
	}
	want := make(map[int]qualityGateConditionModel, len(models))
	if len(models) == 0 {
		return want
	}
	metricKeys, err := r.client.ListMetrics(ctx)
	if err != nil {
		diagnostics.AddError("Cannot read metric keys", err.Error())
		return nil
	}
	metricIDs := make(map[string]int, len(metricKeys))
	for id, key := range metricKeys {
		metricIDs[key] = id
	}
	for _, model := range models {
		if model.Metric.IsUnknown() || model.Operator.IsUnknown() || model.Threshold.IsUnknown() {
			diagnostics.AddError("Unknown quality gate condition", "Metric, operator, and threshold must be known during apply.")
			return nil
		}
		metricID, found := metricIDs[model.Metric.ValueString()]
		if !found {
			diagnostics.AddError("Cannot resolve metric "+model.Metric.ValueString(), "The metrics API returned no metric with this key.")
			return nil
		}
		if _, duplicate := want[metricID]; duplicate {
			diagnostics.AddError("Duplicate quality gate metric", "Each metric may have one condition.")
			return nil
		}
		want[metricID] = model
	}
	return want
}

func (r *qualityGateResource) reconcileConditions(ctx context.Context, id string, desired types.Set, diagnostics *diag.Diagnostics) bool {
	want := r.desiredConditions(ctx, desired, diagnostics)
	if diagnostics.HasError() {
		return false
	}
	current, err := r.client.ListQualityGateConditions(ctx, id)
	if err != nil {
		diagnostics.AddError("Cannot read quality gate conditions", err.Error())
		return false
	}
	if !r.updateExistingConditions(ctx, current, want, diagnostics) {
		return false
	}
	for metricID, model := range want {
		if err := r.client.CreateQualityGateCondition(ctx, client.QualityGateConditionRequest{
			QualityGateID: id, LegacyMetricID: metricID,
			Operator: model.Operator.ValueString(), Threshold: model.Threshold.ValueString(),
		}); err != nil {
			diagnostics.AddError("Cannot create quality gate condition", err.Error())
			return false
		}
	}
	return true
}

func (r *qualityGateResource) updateExistingConditions(ctx context.Context, current []client.QualityGateCondition, want map[int]qualityGateConditionModel, diagnostics *diag.Diagnostics) bool {
	for _, condition := range current {
		model, keep := want[condition.LegacyMetricID]
		if !keep {
			if err := r.client.DeleteQualityGateCondition(ctx, condition.ID); err != nil {
				diagnostics.AddError("Cannot delete quality gate condition", err.Error())
				return false
			}
			continue
		}
		delete(want, condition.LegacyMetricID)
		if condition.Operator == model.Operator.ValueString() && condition.Threshold == model.Threshold.ValueString() {
			continue
		}
		if err := r.client.UpdateQualityGateCondition(ctx, condition.ID, client.QualityGateConditionRequest{
			Operator: model.Operator.ValueString(), Threshold: model.Threshold.ValueString(),
		}); err != nil {
			diagnostics.AddError("Cannot update quality gate condition", err.Error())
			return false
		}
	}
	return true
}
