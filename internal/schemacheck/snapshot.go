// Package schemacheck finds the schema changes that can break the
// configuration or the state of a user.
package schemacheck

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Snapshot has one Schema for each key: "provider", "resource <type name>"
// and "data source <type name>".
type Snapshot map[string]Schema

// Schema has one Attribute for each attribute and block, by path. A nested
// path joins the names with dots.
type Schema map[string]Attribute

// Attribute is the part of an attribute or a block that a change can make
// incompatible. Descriptions are not in it.
type Attribute struct {
	// Kind is the Terraform type of an attribute, or the Go type of a nested
	// attribute or a block, for example "ListNestedBlock".
	Kind          string         `json:"kind"`
	Required      bool           `json:"required,omitempty"`
	Optional      bool           `json:"optional,omitempty"`
	Computed      bool           `json:"computed,omitempty"`
	Sensitive     bool           `json:"sensitive,omitempty"`
	WriteOnly     bool           `json:"write_only,omitempty"`
	Default       string         `json:"default,omitempty"`
	PlanModifiers []PlanModifier `json:"plan_modifiers,omitempty"`
}

// PlanModifier names a plan modifier by its Go type, so that a change of its
// description only is not a change. The description is for the messages.
type PlanModifier struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	// Replace is true for each RequiresReplace variant of the framework.
	Replace bool `json:"replace,omitempty"`
}

type flags interface {
	GetType() attr.Type
	IsRequired() bool
	IsOptional() bool
	IsComputed() bool
	IsSensitive() bool
	IsWriteOnly() bool
}

type describer interface {
	Description(context.Context) string
}

// Take reads the schema of the provider and of each resource and data source.
func Take(ctx context.Context, p provider.Provider) (Snapshot, error) {
	snapshot := Snapshot{}

	providerResp := &provider.SchemaResponse{}
	p.Schema(ctx, provider.SchemaRequest{}, providerResp)
	if providerResp.Diagnostics.HasError() {
		return nil, fmt.Errorf("provider schema: %v", providerResp.Diagnostics)
	}
	snapshot["provider"] = FromSchema(ctx, providerResp.Schema)

	metadata := &provider.MetadataResponse{}
	p.Metadata(ctx, provider.MetadataRequest{}, metadata)

	for _, factory := range p.Resources(ctx) {
		r := factory()
		meta := &resource.MetadataResponse{}
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: metadata.TypeName}, meta)
		resp := &resource.SchemaResponse{}
		r.Schema(ctx, resource.SchemaRequest{}, resp)
		if resp.Diagnostics.HasError() {
			return nil, fmt.Errorf("schema of %s: %v", meta.TypeName, resp.Diagnostics)
		}
		snapshot["resource "+meta.TypeName] = FromSchema(ctx, resp.Schema)
	}

	for _, factory := range p.DataSources(ctx) {
		d := factory()
		meta := &datasource.MetadataResponse{}
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: metadata.TypeName}, meta)
		resp := &datasource.SchemaResponse{}
		d.Schema(ctx, datasource.SchemaRequest{}, resp)
		if resp.Diagnostics.HasError() {
			return nil, fmt.Errorf("schema of %s: %v", meta.TypeName, resp.Diagnostics)
		}
		snapshot["data source "+meta.TypeName] = FromSchema(ctx, resp.Schema)
	}

	return snapshot, nil
}

// FromSchema reads a schema struct of the provider, resource or datasource
// schema package. The three packages have the same shape but no shared
// exported type, so FromSchema reads them through reflection.
func FromSchema(ctx context.Context, schema any) Schema {
	out := Schema{}
	v := reflect.ValueOf(schema)
	walk(ctx, "", v.FieldByName("Attributes"), v.FieldByName("Blocks"), out)
	return out
}

func walk(ctx context.Context, prefix string, attributes, blocks reflect.Value, out Schema) {
	for _, name := range sortedKeys(attributes) {
		value := attributes.MapIndex(reflect.ValueOf(name)).Elem()
		path := prefix + name
		nested, isNested := nestedObject(value)
		a := attribute(ctx, value, isNested)
		if isNested && nested != value {
			a.PlanModifiers = sortModifiers(append(a.PlanModifiers, planModifiers(ctx, nested)...))
		}
		out[path] = a
		if isNested {
			walk(ctx, path+".", nested.FieldByName("Attributes"), nested.FieldByName("Blocks"), out)
		}
	}
	for _, name := range sortedKeys(blocks) {
		value := blocks.MapIndex(reflect.ValueOf(name)).Elem()
		path := prefix + name
		a := Attribute{Kind: value.Type().Name(), PlanModifiers: planModifiers(ctx, value)}
		nested, ok := nestedObject(value)
		if ok && nested != value {
			a.PlanModifiers = sortModifiers(append(a.PlanModifiers, planModifiers(ctx, nested)...))
		}
		out[path] = a
		if ok {
			walk(ctx, path+".", nested.FieldByName("Attributes"), nested.FieldByName("Blocks"), out)
		}
	}
}

func attribute(ctx context.Context, value reflect.Value, isNested bool) Attribute {
	f := value.Interface().(flags)
	kind := f.GetType().TerraformType(ctx).String()
	if isNested {
		kind = value.Type().Name()
	}
	return Attribute{
		Kind:          kind,
		Required:      f.IsRequired(),
		Optional:      f.IsOptional(),
		Computed:      f.IsComputed(),
		Sensitive:     f.IsSensitive(),
		WriteOnly:     f.IsWriteOnly(),
		Default:       defaultValue(ctx, value),
		PlanModifiers: planModifiers(ctx, value),
	}
}

// nestedObject gives the struct that holds the nested attributes and blocks:
// the NestedObject field of a list, set or map, or the value itself for a
// single nested attribute or block.
func nestedObject(value reflect.Value) (reflect.Value, bool) {
	if value.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	if nested := value.FieldByName("NestedObject"); nested.IsValid() {
		return nested, true
	}
	if attributes := value.FieldByName("Attributes"); attributes.IsValid() && attributes.Kind() == reflect.Map {
		return value, true
	}
	return reflect.Value{}, false
}

// defaultValue gives the value of a static default, or the description of
// another default. A value does not change when the framework changes the
// text of its descriptions.
func defaultValue(ctx context.Context, value reflect.Value) string {
	for _, result := range callMethods(value, "DefaultValue") {
		if result.IsNil() {
			continue
		}
		if v, ok := defaultPlanValue(ctx, result.Elem()); ok {
			return v
		}
		if d, ok := result.Interface().(describer); ok {
			return d.Description(ctx)
		}
	}
	return ""
}

// defaultPlanValue calls the method of a default that sets the plan value,
// such as DefaultString, with an empty request.
func defaultPlanValue(ctx context.Context, d reflect.Value) (string, bool) {
	for i := range d.NumMethod() {
		method := d.Type().Method(i)
		// In(0) is the receiver.
		if !strings.HasPrefix(method.Name, "Default") || method.Type.NumIn() != 4 || method.Type.NumOut() != 0 {
			continue
		}
		req := reflect.New(method.Type.In(2)).Elem()
		resp := reflect.New(method.Type.In(3).Elem())
		d.Method(i).Call([]reflect.Value{reflect.ValueOf(ctx), req, resp})
		planValue := resp.Elem().FieldByName("PlanValue")
		if !planValue.IsValid() {
			continue
		}
		if v, ok := planValue.Interface().(attr.Value); ok {
			return v.String(), true
		}
	}
	return "", false
}

func planModifiers(ctx context.Context, value reflect.Value) []PlanModifier {
	var out []PlanModifier
	for _, result := range callMethods(value, "PlanModifiers") {
		for i := range result.Len() {
			if m, ok := planModifier(ctx, result.Index(i)); ok {
				out = append(out, m)
			}
		}
	}
	return sortModifiers(out)
}

func sortModifiers(m []PlanModifier) []PlanModifier {
	sort.Slice(m, func(i, j int) bool {
		if m[i].Type != m[j].Type {
			return m[i].Type < m[j].Type
		}
		return m[i].Description < m[j].Description
	})
	return m
}

func planModifier(ctx context.Context, modifier reflect.Value) (PlanModifier, bool) {
	if modifier.IsNil() {
		return PlanModifier{}, false
	}
	t := indirectType(modifier.Elem().Type())
	// UseStateForUnknown only keeps a known value in the plan.
	if strings.HasPrefix(t.Name(), "useStateForUnknown") || strings.HasPrefix(t.Name(), "useNonNullStateForUnknown") {
		return PlanModifier{}, false
	}
	m := PlanModifier{
		Type: t.PkgPath() + "." + t.Name(),
		// The framework gives each RequiresReplace variant the same type.
		// Only the description tells them apart, and its text can change in
		// a new framework version, so the check does not compare them.
		Replace: strings.HasPrefix(t.Name(), "requiresReplace") &&
			strings.HasPrefix(t.PkgPath(), "github.com/hashicorp/terraform-plugin-framework/"),
	}
	if d, ok := modifier.Interface().(describer); ok {
		m.Description = d.Description(ctx)
	}
	return m, true
}

// callMethods calls each method without arguments whose name ends with
// suffix, such as StringPlanModifiers, and gives the first result of each.
func callMethods(value reflect.Value, suffix string) []reflect.Value {
	var out []reflect.Value
	for i := range value.NumMethod() {
		method := value.Type().Method(i)
		if !strings.HasSuffix(method.Name, suffix) || method.Type.NumIn() != 1 || method.Type.NumOut() != 1 {
			continue
		}
		out = append(out, value.Method(i).Call(nil)[0])
	}
	return out
}

func indirectType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func sortedKeys(m reflect.Value) []string {
	if !m.IsValid() || m.Kind() != reflect.Map {
		return nil
	}
	keys := make([]string, 0, m.Len())
	for _, k := range m.MapKeys() {
		keys = append(keys, k.String())
	}
	sort.Strings(keys)
	return keys
}
