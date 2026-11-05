package shared

import (
	"context"
	_ "embed" // for the go:embed of the schema description
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/configure"
)

var (
	_ resource.Resource                   = &qualityProfileRuleResource{}
	_ resource.ResourceWithConfigure      = &qualityProfileRuleResource{}
	_ resource.ResourceWithImportState    = &qualityProfileRuleResource{}
	_ resource.ResourceWithValidateConfig = &qualityProfileRuleResource{}
)

//go:embed quality_profile_rule_resource.md
var qualityProfileRuleDescription string

// cannotActivateRule starts the summary of a failed activation.
const cannotActivateRule = "Cannot activate the rule "

var (
	ruleKeyPattern    = regexp.MustCompile(`^[^:\s/]+:\S+$`)
	softwareQualities = []string{"SECURITY", "RELIABILITY", "MAINTAINABILITY"}
	impactSeverities  = []string{"INFO", "LOW", "MEDIUM", "HIGH", "BLOCKER"}
)

// NewQualityProfileRuleResource returns the sonarqube_quality_profile_rule
// resource.
func NewQualityProfileRuleResource() resource.Resource {
	return &qualityProfileRuleResource{
		readRetries:  []time.Duration{time.Second, 2 * time.Second},
		writeRetries: []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second},
	}
}

type qualityProfileRuleResource struct {
	client *client.Client
	// readRetries are the waits before Read and Delete read a rule that they
	// do not find again. Create and Update already wait until the search shows
	// the change, so these waits are short: a rule that is really gone costs
	// them on each refresh.
	readRetries []time.Duration
	// writeRetries are the waits before Create and Update read the rule
	// again. The search index of the rules can lag behind a write by some
	// seconds.
	writeRetries []time.Duration
}

type qualityProfileRuleResourceModel struct {
	ID                types.String `tfsdk:"id"`
	QualityProfileKey types.String `tfsdk:"quality_profile_key"`
	Rule              types.String `tfsdk:"rule"`
	Impacts           types.Map    `tfsdk:"impacts"`
	Params            types.Map    `tfsdk:"params"`
	Active            types.Bool   `tfsdk:"active"`
}

// inactive reports active = false. A null value, as in a state from before
// the attribute, is active.
func (m qualityProfileRuleResourceModel) inactive() bool {
	return !m.Active.IsNull() && !m.Active.IsUnknown() && !m.Active.ValueBool()
}

func (m qualityProfileRuleResourceModel) importID() string {
	return m.QualityProfileKey.ValueString() + "/" + m.Rule.ValueString()
}

func (r *qualityProfileRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_quality_profile_rule"
}

func (r *qualityProfileRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: qualityProfileRuleDescription,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "`<quality_profile_key>/<rule>`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"quality_profile_key": schema.StringAttribute{Required: true, PlanModifiers: replace,
				Description: "Key of a custom quality profile, for example the `key` of a `sonarqube_quality_profile`. " +
					"Built-in profiles are read-only.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1), stringvalidator.RegexMatches(
					regexp.MustCompile(`^[^/]+$`), "must not contain /")}},
			"rule": schema.StringAttribute{Required: true, PlanModifiers: replace,
				Description: "Key of the rule, as `<repository>:<key>`, for example `java:S107`. The rule must have " +
					"the language of the profile.",
				Validators: []validator.String{stringvalidator.RegexMatches(ruleKeyPattern,
					"must be <repository>:<key>, for example java:S107")}},
			"impacts": schema.MapAttribute{Optional: true, ElementType: types.StringType,
				MarkdownDescription: "Severity for each software quality of the rule. The keys are `SECURITY`, " +
					"`RELIABILITY` and `MAINTAINABILITY`. The values are `INFO`, `LOW`, `MEDIUM`, `HIGH` and " +
					"`BLOCKER`. Set only the software qualities that the rule has. The software qualities that you " +
					"do not set keep the severity of the parent profile or of the rule.",
				Validators: []validator.Map{
					mapvalidator.SizeAtLeast(1),
					mapvalidator.KeysAre(stringvalidator.OneOf(softwareQualities...)),
					mapvalidator.ValueStringsAre(stringvalidator.OneOf(impactSeverities...)),
				}},
			"active": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "When `false`, the profile deactivates a rule that it gets from its parent " +
					"profile. Then do not set `impacts` and `params`. The default is `true`. A change of `active` " +
					"changes the rule in place."},
			"params": schema.MapAttribute{Optional: true, ElementType: types.StringType,
				MarkdownDescription: "Values of the parameters of the rule, by parameter key. The parameters that " +
					"you do not set keep the value of the parent profile or the default value of the rule. The " +
					"server checks the keys and the values.",
				Validators: []validator.Map{
					mapvalidator.SizeAtLeast(1),
					mapvalidator.KeysAre(stringvalidator.RegexMatches(regexp.MustCompile(`^[^;="\s]+$`),
						`must not be empty, and must not contain ;, =, " or a space`)),
					mapvalidator.ValueStringsAre(stringvalidator.RegexMatches(regexp.MustCompile(`^[^"]+$`),
						`must not be empty, and must not contain "`)),
				}},
		},
	}
}

func (r *qualityProfileRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := configure.CloudClient(req.ProviderData, "The sonarqube_quality_profile_rule resource manages a rule of a quality profile")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *qualityProfileRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan qualityProfileRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	planned, diagnostics := plan.values(ctx)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, rule := plan.QualityProfileKey.ValueString(), plan.Rule.ValueString()
	plan.ID = types.StringValue(plan.importID())
	if plan.inactive() {
		resp.Diagnostics.Append(r.deactivate(ctx, plan, &resp.State)...)
		return
	}
	deactivated, err := r.isDeactivatedInherited(ctx, key, rule)
	if err == nil {
		err = r.activate(ctx, key, rule, planned, deactivated)
	}
	if err != nil {
		resp.Diagnostics.AddError(cannotActivateRule+rule, ruleErrorDetail(err))
		return
	}
	// When the check fails, the rule can be active, so the state keeps it,
	// and Terraform marks it as tainted.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(r.checkApplied(ctx, key, rule, planned)...)
}

func (r *qualityProfileRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state qualityProfileRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, rule := state.QualityProfileKey.ValueString(), state.Rule.ValueString()
	state.ID = types.StringValue(state.importID())
	var gone bool
	var err error
	if state.inactive() {
		gone, err = r.readInactive(ctx, &state)
	} else {
		gone, err = r.readActive(ctx, &state)
	}
	switch {
	case err != nil:
		resp.Diagnostics.AddError(readRuleSummary(key, rule), err.Error())
	case gone:
		resp.State.RemoveResource(ctx)
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	}
}

// readActive reads a rule of the state with active = true. A rule that the
// profile gets from its parent, and that someone deactivated outside of
// Terraform, gets active = false, and the plan shows the change. It reports
// gone for a rule that is not active and not inherited, also when the profile
// was deleted.
func (r *qualityProfileRuleResource) readActive(ctx context.Context, state *qualityProfileRuleResourceModel) (gone bool, err error) {
	key, rule := state.QualityProfileKey.ValueString(), state.Rule.ValueString()
	active, err := r.findRule(ctx, key, rule)
	switch {
	case err == nil:
		state.keepManaged(active.Values)
		state.Active = types.BoolValue(true)
		return false, nil
	case !errors.Is(err, client.ErrNotFound):
		return false, err
	}
	gone, err = r.notInherited(ctx, key, rule)
	if err == nil && !gone {
		state.Active = types.BoolValue(false)
		state.setValues(client.RuleValues{})
	}
	return gone, err
}

// Update sends the planned values. activate_rule keeps each value that it
// does not get, so when the plan removes a parameter or an impact, Update
// first resets the rule. The removed value then comes from the parent
// profile, or is the default value of the rule. A change to active = false
// deactivates the rule, and a change to active = true activates it again,
// also without values.
func (r *qualityProfileRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state qualityProfileRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	planned, diagnostics := plan.values(ctx)
	resp.Diagnostics.Append(diagnostics...)
	current, diagnostics := state.values(ctx)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, rule := state.QualityProfileKey.ValueString(), state.Rule.ValueString()
	if plan.inactive() {
		resp.Diagnostics.Append(r.deactivate(ctx, plan, &resp.State)...)
		return
	}
	// The state can be out of date, for example with -refresh=false, so the
	// server tells if the rule needs the activation in two steps.
	deactivated, err := r.isDeactivatedInherited(ctx, key, rule)
	if err != nil {
		resp.Diagnostics.AddError(readRuleSummary(key, rule), err.Error())
		return
	}
	if state.inactive() || deactivated {
		current = client.RuleValues{}
	}
	reset := hasRemovedKey(current, planned)
	if reset {
		if err := r.client.ResetQualityProfileRule(ctx, key, rule); err != nil {
			resp.Diagnostics.AddError("Cannot reset the rule "+rule, ruleErrorDetail(err))
			return
		}
	}
	// A rule with active = false needs an activation also without values.
	if !planned.IsEmpty() || state.inactive() || deactivated {
		if err := r.activate(ctx, key, rule, planned, deactivated); err != nil {
			detail := ruleErrorDetail(err)
			if reset {
				detail += "\n\nThe provider reset the rule before the change, so the rule now has the values of " +
					"the parent profile or the default values. Run the apply again."
			}
			resp.Diagnostics.AddError("Cannot change the rule "+rule, detail)
			return
		}
	}
	resp.Diagnostics.Append(r.checkApplied(ctx, key, rule, planned)...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

// Delete deactivates a rule that the profile activates itself. A rule that
// the profile gets from its parent gets the values of the parent again, and a
// rule with active = false is activated again with them. The inheritance is
// read now, because the parent of the profile can change after the create.
func (r *qualityProfileRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state qualityProfileRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, rule := state.QualityProfileKey.ValueString(), state.Rule.ValueString()
	if state.inactive() {
		if err := r.activateInherited(ctx, key, rule); err != nil {
			resp.Diagnostics.AddError(cannotActivateRule+rule+" in the quality profile "+key, ruleErrorDetail(err))
		}
		return
	}
	active, err := r.findRule(ctx, key, rule)
	switch {
	case errors.Is(err, client.ErrNotFound):
		// Someone deactivated the inherited rule outside of Terraform after
		// the refresh. The destroy still gives it the values of the parent.
		if err := r.activateInherited(ctx, key, rule); err != nil {
			resp.Diagnostics.AddError(cannotActivateRule+rule+" in the quality profile "+key, ruleErrorDetail(err))
		}
		return
	case err != nil:
		resp.Diagnostics.AddError(readRuleSummary(key, rule), err.Error())
		return
	}
	if active.Inherit == client.RuleInheritNone {
		err = r.client.DeactivateQualityProfileRule(ctx, key, rule)
	} else {
		err = r.client.ResetQualityProfileRule(ctx, key, rule)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot remove the rule "+rule+" from the quality profile "+key, ruleErrorDetail(err))
	}
}

// ImportState takes "<quality_profile_key>/<rule>". A profile key has no "/".
// The server gives all the impacts and parameters, so the state gets only the
// ones that differ from the values of the parent profile, for an inherited
// rule, or from the defaults of the rule.
func (r *qualityProfileRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	key, rule, _ := strings.Cut(req.ID, "/")
	if key == "" || !ruleKeyPattern.MatchString(rule) {
		resp.Diagnostics.AddError("Invalid quality profile rule import ID",
			"Use <quality_profile_key>/<rule>, for example my-profile-key/java:S107.")
		return
	}
	state := qualityProfileRuleResourceModel{
		QualityProfileKey: types.StringValue(key),
		Rule:              types.StringValue(rule),
		Active:            types.BoolValue(true),
	}
	state.ID = types.StringValue(state.importID())
	active, err := r.client.GetActiveRule(ctx, key, rule)
	if errors.Is(err, client.ErrNotFound) {
		// An inactive rule that the parent activates is a deactivated
		// inherited rule.
		gone, err := r.notInherited(ctx, key, rule)
		switch {
		case err != nil:
			resp.Diagnostics.AddError(readRuleSummary(key, rule), err.Error())
		case gone:
			resp.Diagnostics.AddError("Cannot import the rule "+rule,
				"The rule is not active in the quality profile "+key+", and the profile does not get it from its "+
					"parent. Or the profile does not exist.")
		default:
			state.Active = types.BoolValue(false)
			state.setValues(client.RuleValues{})
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		}
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(readRuleSummary(key, rule), err.Error())
		return
	}
	base, err := r.inheritedValues(ctx, key, rule, active)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the parent of the quality profile "+key, err.Error())
		return
	}
	state.setValues(changedValues(active.Values, base))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// activate activates the rule with the values. deactivated tells that the
// profile gets the rule from its parent but does not activate it. With
// values, the server would then make it the profile's own rule, and a destroy
// would deactivate it. So activate first activates it without values, which
// gives the rule of the parent, and then sends the values, which override it.
// When the values fail, it deactivates the rule again, so that nothing
// changes.
func (r *qualityProfileRuleResource) activate(ctx context.Context, key, rule string, values client.RuleValues, deactivated bool) error {
	if !deactivated {
		return r.client.ActivateQualityProfileRule(ctx, key, rule, values)
	}
	if err := r.client.ActivateQualityProfileRule(ctx, key, rule, client.RuleValues{}); err != nil || values.IsEmpty() {
		return err
	}
	err := r.client.ActivateQualityProfileRule(ctx, key, rule, values)
	if err != nil {
		if rollbackErr := r.client.DeactivateQualityProfileRule(ctx, key, rule); rollbackErr != nil {
			return fmt.Errorf("%w\n\nThe provider could not deactivate the rule again, so it is now active with "+
				"the values of the parent profile: %s", err, rollbackErr.Error())
		}
	}
	return err
}

// inheritedValues returns the values that the rule has without a change in
// this profile: the values of the parent for an inherited rule, or else the
// defaults of the rule.
func (r *qualityProfileRuleResource) inheritedValues(ctx context.Context, key, rule string, active *client.ActiveRule) (client.RuleValues, error) {
	if active.Inherit == client.RuleInheritNone {
		return active.Defaults, nil
	}
	parent, err := r.parentRule(ctx, key, rule)
	switch {
	case errors.Is(err, client.ErrNotFound):
		return active.Defaults, nil
	case err != nil:
		return client.RuleValues{}, err
	}
	return parent.Values, nil
}

func (r *qualityProfileRuleResource) getRule(ctx context.Context, key, rule string) func() (*client.ActiveRule, error) {
	return func() (*client.ActiveRule, error) { return r.client.GetActiveRule(ctx, key, rule) }
}

// findRule reads the rule, and reads it again after each wait of readRetries
// while the search does not find it, because the index can lag.
func (r *qualityProfileRuleResource) findRule(ctx context.Context, key, rule string) (*client.ActiveRule, error) {
	return readAgain(ctx, r.readRetries, r.getRule(ctx, key, rule),
		func(_ *client.ActiveRule, err error) bool { return !errors.Is(err, client.ErrNotFound) })
}

// checkApplied reads the rule until the search shows the planned values, and
// reports each value that the rule did not get. The server ignores an impact
// for a software quality that the rule does not have, so this is the only way
// to find that fault.
func (r *qualityProfileRuleResource) checkApplied(ctx context.Context, key, rule string, planned client.RuleValues) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	active, err := readAgain(ctx, r.writeRetries, r.getRule(ctx, key, rule),
		func(active *client.ActiveRule, err error) bool {
			return (err != nil && !errors.Is(err, client.ErrNotFound)) || (err == nil && len(mismatches(active.Values, planned)) == 0)
		})
	switch {
	case errors.Is(err, client.ErrNotFound):
		diagnostics.AddError("The rule "+rule+" is not active after the change",
			"The search does not show the rule as active in the quality profile "+key+". The search index can "+
				"lag behind a change. Run the apply again.")
	case err != nil:
		diagnostics.AddError(readRuleSummary(key, rule), err.Error())
	default:
		if faults := mismatches(active.Values, planned); len(faults) > 0 {
			diagnostics.AddError("The rule "+rule+" did not get the configured values", strings.Join(faults, "\n"))
		}
	}
	return diagnostics
}

// mismatches describes each planned value that the server values do not have.
func mismatches(server, planned client.RuleValues) []string {
	var faults []string
	for _, quality := range slices.Sorted(maps.Keys(planned.Impacts)) {
		severity, ok := server.Impacts[quality]
		switch {
		case !ok:
			faults = append(faults, "The rule has no "+quality+" impact. Set only the software qualities that "+
				"the rule has: "+strings.Join(slices.Sorted(maps.Keys(server.Impacts)), ", ")+".")
		case severity != planned.Impacts[quality]:
			faults = append(faults, fmt.Sprintf("The %s impact is %s, not %s.", quality, severity, planned.Impacts[quality]))
		}
	}
	for _, param := range slices.Sorted(maps.Keys(planned.Params)) {
		value, ok := server.Params[param]
		switch {
		case !ok:
			faults = append(faults, "The rule has no parameter "+param+".")
		case value != planned.Params[param]:
			faults = append(faults, fmt.Sprintf("The parameter %s is %q, not %q.", param, value, planned.Params[param]))
		}
	}
	return faults
}

// values returns the impacts and params of the model. A null map is an empty
// map.
func (m qualityProfileRuleResourceModel) values(ctx context.Context) (client.RuleValues, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	v := client.RuleValues{Impacts: map[string]string{}, Params: map[string]string{}}
	if !m.Impacts.IsNull() {
		diagnostics.Append(m.Impacts.ElementsAs(ctx, &v.Impacts, false)...)
	}
	if !m.Params.IsNull() {
		diagnostics.Append(m.Params.ElementsAs(ctx, &v.Params, false)...)
	}
	return v, diagnostics
}

func (m *qualityProfileRuleResourceModel) setValues(v client.RuleValues) {
	m.Impacts, m.Params = stringMap(v.Impacts), stringMap(v.Params)
}

// keepManaged sets the server values of the keys that the model has, so that
// the values that the configuration does not set show no change. A key that
// the server does not have any more is not kept, so the plan shows it.
func (m *qualityProfileRuleResourceModel) keepManaged(server client.RuleValues) {
	m.Impacts = managedValues(m.Impacts, server.Impacts)
	m.Params = managedValues(m.Params, server.Params)
}

func managedValues(state types.Map, server map[string]string) types.Map {
	if state.IsNull() {
		return state
	}
	values := map[string]string{}
	for key := range state.Elements() {
		if value, ok := server[key]; ok {
			values[key] = value
		}
	}
	return stringMap(values)
}

// changedValues keeps the values that differ from base.
func changedValues(values, base client.RuleValues) client.RuleValues {
	changed := func(values, base map[string]string) map[string]string {
		result := map[string]string{}
		for key, value := range values {
			if baseValue, ok := base[key]; !ok || baseValue != value {
				result[key] = value
			}
		}
		return result
	}
	return client.RuleValues{Impacts: changed(values.Impacts, base.Impacts), Params: changed(values.Params, base.Params)}
}

// stringMap returns null for an empty map, because the schema refuses an
// empty map.
func stringMap(values map[string]string) types.Map {
	if len(values) == 0 {
		return types.MapNull(types.StringType)
	}
	elements := make(map[string]attr.Value, len(values))
	for key, value := range values {
		elements[key] = types.StringValue(value)
	}
	return types.MapValueMust(types.StringType, elements)
}

// hasRemovedKey reports a key of current that planned does not have.
func hasRemovedKey(current, planned client.RuleValues) bool {
	removed := func(current, planned map[string]string) bool {
		for key := range current {
			if _, ok := planned[key]; !ok {
				return true
			}
		}
		return false
	}
	return removed(current.Impacts, planned.Impacts) || removed(current.Params, planned.Params)
}

func readRuleSummary(key, rule string) string {
	return "Cannot read the rule " + rule + " of the quality profile " + key
}

// ruleErrorDetail explains a 404, which names a rule or a profile that does
// not exist.
func ruleErrorDetail(err error) string {
	if errors.Is(err, client.ErrNotFound) {
		return err.Error() + "\n\nCheck the key of the quality profile and the key of the rule. The rule must " +
			"have the language of the profile."
	}
	return err.Error()
}
