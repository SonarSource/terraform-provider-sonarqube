package schemacheck

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
)

type refuse struct{}

func (refuse) Description(context.Context) string         { return "Refuses a new value." }
func (refuse) MarkdownDescription(context.Context) string { return "Refuses a new value." }
func (refuse) PlanModifyString(context.Context, planmodifier.StringRequest, *planmodifier.StringResponse) {
}

func TestFromSchema(t *testing.T) {
	ctx := context.Background()
	s := schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"key": schema.StringAttribute{Required: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), refuse{}}},
			"mode":  schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("auto")},
			"token": schema.StringAttribute{Optional: true, Sensitive: true},
			"tags":  schema.SetAttribute{Optional: true, ElementType: types.StringType},
			"settings": schema.SingleNestedAttribute{Optional: true, Attributes: map[string]schema.Attribute{
				"name": schema.StringAttribute{Required: true},
			}},
		},
		Blocks: map[string]schema.Block{
			"condition": schema.ListNestedBlock{NestedObject: schema.NestedBlockObject{
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Attributes:    map[string]schema.Attribute{"metric": schema.StringAttribute{Required: true}},
				Blocks: map[string]schema.Block{"window": schema.SingleNestedBlock{
					Attributes: map[string]schema.Attribute{"days": schema.Int64Attribute{Optional: true}},
				}},
			}},
		},
	}

	want := Schema{
		"id": {Kind: "tftypes.String", Computed: true},
		"key": {Kind: "tftypes.String", Required: true, PlanModifiers: []PlanModifier{
			{Type: "github.com/SonarSource/terraform-provider-sonarqube/internal/schemacheck.refuse",
				Description: "Refuses a new value."},
			{Type: "github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier.requiresReplaceIfModifier",
				Description: "If the value of this attribute changes, Terraform will destroy and recreate the resource.",
				Replace:     true},
		}},
		"mode":          {Kind: "tftypes.String", Optional: true, Computed: true, Default: `"auto"`},
		"token":         {Kind: "tftypes.String", Optional: true, Sensitive: true},
		"tags":          {Kind: "tftypes.Set[tftypes.String]", Optional: true},
		"settings":      {Kind: "SingleNestedAttribute", Optional: true},
		"settings.name": {Kind: "tftypes.String", Required: true},
		"condition": {Kind: "ListNestedBlock", PlanModifiers: []PlanModifier{
			{Type: "github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier.requiresReplaceIfModifier",
				Description: "If the value of this attribute changes, Terraform will destroy and recreate the resource.",
				Replace:     true},
		}},
		"condition.metric":      {Kind: "tftypes.String", Required: true},
		"condition.window":      {Kind: "SingleNestedBlock"},
		"condition.window.days": {Kind: "tftypes.Number", Optional: true},
	}
	got := FromSchema(ctx, s)
	for path, w := range want {
		if g, ok := got[path]; !ok || !reflect.DeepEqual(g, w) {
			t.Errorf("%s:\n got %+v\nwant %+v", path, g, w)
		}
	}
	for path := range got {
		if _, ok := want[path]; !ok {
			t.Errorf("unexpected path %s", path)
		}
	}
}

// Take must read each registered resource and data source, or the check
// misses the changes of the others.
func TestTakeReadsEachType(t *testing.T) {
	ctx := context.Background()
	p := provider.New("test")()

	snapshot, err := Take(ctx, p)
	if err != nil {
		t.Fatal(err)
	}

	want := 1 + len(p.Resources(ctx)) + len(p.DataSources(ctx))
	if len(snapshot) != want {
		t.Errorf("snapshot has %d schemas, want %d", len(snapshot), want)
	}
	for name, s := range snapshot {
		if len(s) == 0 {
			t.Errorf("%s has no attributes", name)
		}
		if name != "provider" && !strings.Contains(name, " sonarqube_") {
			t.Errorf("schema name %q has no type name", name)
		}
	}
}
