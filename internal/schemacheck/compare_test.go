package schemacheck

import (
	"slices"
	"testing"
)

var (
	replace = PlanModifier{Type: "framework.requiresReplaceIfModifier", Description: "always", Replace: true}
	refuse1 = PlanModifier{Type: "provider.refuseChange", Description: "Refuses a new value."}
)

func TestCompare(t *testing.T) {
	str := Attribute{Kind: "tftypes.String", Optional: true}

	tests := []struct {
		name       string
		base, head Schema
		want       []string
	}{
		{
			name: "no change",
			base: Schema{"a": str},
			head: Schema{"a": str},
		},
		{
			name: "new optional attribute",
			base: Schema{"a": str},
			head: Schema{"a": str, "b": str},
		},
		{
			name: "removed attribute",
			base: Schema{"a": str},
			head: Schema{},
			want: []string{"resource r: a: removed"},
		},
		{
			name: "removed parent reports the parent only",
			base: Schema{"b": {Kind: "ListNestedBlock"}, "b.x": str, "b.y.z": str, "b.y": {Kind: "SingleNestedBlock"}},
			head: Schema{},
			want: []string{"resource r: b: removed"},
		},
		{
			name: "type change",
			base: Schema{"a": str},
			head: Schema{"a": {Kind: "tftypes.Number", Optional: true}},
			want: []string{"resource r: a: type changed from tftypes.String to tftypes.Number"},
		},
		{
			name: "optional to required",
			base: Schema{"a": str},
			head: Schema{"a": {Kind: "tftypes.String", Required: true}},
			want: []string{"resource r: a: now required"},
		},
		{
			name: "required to optional",
			base: Schema{"a": {Kind: "tftypes.String", Required: true}},
			head: Schema{"a": str},
		},
		{
			name: "optional to computed only",
			base: Schema{"a": str},
			head: Schema{"a": {Kind: "tftypes.String", Computed: true}},
			want: []string{"resource r: a: can no longer be set in the configuration"},
		},
		{
			name: "computed to optional and computed",
			base: Schema{"a": {Kind: "tftypes.String", Computed: true}},
			head: Schema{"a": {Kind: "tftypes.String", Optional: true, Computed: true}},
		},
		{
			name: "now sensitive",
			base: Schema{"a": str},
			head: Schema{"a": {Kind: "tftypes.String", Optional: true, Sensitive: true}},
			want: []string{"resource r: a: now sensitive, so an output that shows it must set sensitive = true"},
		},
		{
			name: "no longer sensitive",
			base: Schema{"a": {Kind: "tftypes.String", Optional: true, Sensitive: true}},
			head: Schema{"a": str},
		},
		{
			name: "default changed",
			base: Schema{"a": {Kind: "tftypes.String", Optional: true, Computed: true, Default: "x"}},
			head: Schema{"a": {Kind: "tftypes.String", Optional: true, Computed: true, Default: "y"}},
			want: []string{`resource r: a: default changed from "x" to "y"`},
		},
		{
			name: "required to optional with a default",
			base: Schema{"a": {Kind: "tftypes.String", Required: true}},
			head: Schema{"a": {Kind: "tftypes.String", Optional: true, Computed: true, Default: `"x"`}},
		},
		{
			name: "now requires replacement",
			base: Schema{"a": str},
			head: Schema{"a": {Kind: "tftypes.String", Optional: true, PlanModifiers: []PlanModifier{replace}}},
			want: []string{"resource r: a: now requires replacement (always)"},
		},
		{
			// The framework can change the text of a description in a new
			// version, so the check does not compare replace conditions.
			name: "other replace condition",
			base: Schema{"a": {Kind: "tftypes.String", Optional: true, PlanModifiers: []PlanModifier{
				{Type: replace.Type, Description: "when configured", Replace: true}}}},
			head: Schema{"a": {Kind: "tftypes.String", Optional: true, PlanModifiers: []PlanModifier{replace}}},
		},
		{
			name: "no longer computed",
			base: Schema{"a": {Kind: "tftypes.String", Optional: true, Computed: true}},
			head: Schema{"a": str},
			want: []string{"resource r: a: no longer computed, so a value that the server set is planned as null"},
		},
		{
			name: "now write-only",
			base: Schema{"a": str},
			head: Schema{"a": {Kind: "tftypes.String", Optional: true, WriteOnly: true}},
			want: []string{"resource r: a: now write-only, so a reference to it gives null"},
		},
		{
			name: "no longer write-only",
			base: Schema{"a": {Kind: "tftypes.String", Optional: true, WriteOnly: true}},
			head: Schema{"a": str},
		},
		{
			name: "parent with a new kind reports the parent only",
			base: Schema{"p": {Kind: "SingleNestedAttribute", Optional: true}, "p.x": str},
			head: Schema{"p": {Kind: "ListNestedAttribute", Optional: true}, "p.x": {Kind: "tftypes.Number", Required: true},
				"p.y": {Kind: "tftypes.String", Required: true}},
			want: []string{"resource r: p: type changed from SingleNestedAttribute to ListNestedAttribute"},
		},
		{
			name: "no longer requires replacement",
			base: Schema{"a": {Kind: "tftypes.String", Optional: true, PlanModifiers: []PlanModifier{replace}}},
			head: Schema{"a": str},
		},
		{
			name: "new custom plan modifier",
			base: Schema{"a": str},
			head: Schema{"a": {Kind: "tftypes.String", Optional: true, PlanModifiers: []PlanModifier{refuse1}}},
			want: []string{"resource r: a: new plan modifier provider.refuseChange (Refuses a new value.)"},
		},
		{
			name: "custom plan modifier with new description",
			base: Schema{"a": {Kind: "tftypes.String", Optional: true, PlanModifiers: []PlanModifier{refuse1}}},
			head: Schema{"a": {Kind: "tftypes.String", Optional: true, PlanModifiers: []PlanModifier{
				{Type: refuse1.Type, Description: "Refuses a new value, with a reason."}}}},
		},
		{
			name: "new required attribute",
			base: Schema{"a": str},
			head: Schema{"a": str, "b": {Kind: "tftypes.String", Required: true}},
			want: []string{"resource r: b: new required attribute"},
		},
		{
			name: "new required attribute in an existing block",
			base: Schema{"b": {Kind: "ListNestedBlock"}},
			head: Schema{"b": {Kind: "ListNestedBlock"}, "b.x": {Kind: "tftypes.String", Required: true}},
			want: []string{"resource r: b.x: new required attribute"},
		},
		{
			name: "new required attribute in a new optional parent",
			base: Schema{},
			head: Schema{"p": {Kind: "SingleNestedAttribute", Optional: true}, "p.x": {Kind: "tftypes.String", Required: true}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, c := range Compare(Snapshot{"resource r": tt.base}, Snapshot{"resource r": tt.head}) {
				got = append(got, c.String())
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Compare:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestCompareSchemas(t *testing.T) {
	str := Attribute{Kind: "tftypes.String", Optional: true}
	base := Snapshot{"provider": {"url": str}, "resource a": {"x": str}, "data source b": {"x": str}}
	head := Snapshot{"provider": {"url": str}, "resource a": {"x": str}, "resource c": {"x": str}}

	var got []string
	for _, c := range Compare(base, head) {
		got = append(got, c.String())
	}
	want := []string{"data source b: removed"}
	if !slices.Equal(got, want) {
		t.Errorf("Compare:\n got %q\nwant %q", got, want)
	}
}
