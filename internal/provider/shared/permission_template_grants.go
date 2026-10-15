package shared

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// The model of sonarqube_permission_template holds the grants as blocks per
// group and per user and as a set for the project creator. The client and
// the diff use a flat list of client.TemplateGrant. This file converts
// between the two.

// templateBlock is a group or a user block: a key and its permissions.
type templateBlock interface {
	templateGroupModel | templateUserModel
	key() (types.String, types.Set)
}

func (m templateGroupModel) key() (types.String, types.Set) { return m.Name, m.Permissions }
func (m templateUserModel) key() (types.String, types.Set)  { return m.Login, m.Permissions }

func newGroupBlock(name string, permissions types.Set) templateGroupModel {
	return templateGroupModel{Name: types.StringValue(name), Permissions: permissions}
}

func newUserBlock(login string, permissions types.Set) templateUserModel {
	return templateUserModel{Login: types.StringValue(login), Permissions: permissions}
}

// blocks reads the blocks of a set. A null or unknown set gives none.
func blocks[B templateBlock](ctx context.Context, set types.Set, diagnostics *diag.Diagnostics) []B {
	var models []B
	if !set.IsNull() && !set.IsUnknown() {
		diagnostics.Append(set.ElementsAs(ctx, &models, false)...)
	}
	return models
}

func (m permissionTemplateResourceModel) template() client.PermissionTemplate {
	return client.PermissionTemplate{
		Name:              m.Name.ValueString(),
		Description:       m.Description.ValueString(),
		ProjectKeyPattern: m.ProjectKeyPattern.ValueString(),
	}
}

// grants gives the grants of the model as a flat list.
func (m permissionTemplateResourceModel) grants(ctx context.Context, diagnostics *diag.Diagnostics) []client.TemplateGrant {
	grants := setGrants(ctx, client.GranteeProjectCreator, "", m.ProjectCreatorPermissions, diagnostics)
	grants = append(grants, blockGrants[templateGroupModel](ctx, client.GranteeGroup, m.Groups, diagnostics)...)
	return append(grants, blockGrants[templateUserModel](ctx, client.GranteeUser, m.Users, diagnostics)...)
}

func blockGrants[B templateBlock](ctx context.Context, kind string, set types.Set, diagnostics *diag.Diagnostics) []client.TemplateGrant {
	var grants []client.TemplateGrant
	for _, block := range blocks[B](ctx, set, diagnostics) {
		key, permissions := block.key()
		grants = append(grants, setGrants(ctx, kind, key.ValueString(), permissions, diagnostics)...)
	}
	return grants
}

func setGrants(ctx context.Context, kind, name string, permissions types.Set, diagnostics *diag.Diagnostics) []client.TemplateGrant {
	var keys []string
	if !permissions.IsNull() {
		diagnostics.Append(permissions.ElementsAs(ctx, &keys, false)...)
	}
	grants := make([]client.TemplateGrant, 0, len(keys))
	for _, key := range keys {
		grants = append(grants, client.TemplateGrant{Kind: kind, Name: name, Permission: key})
	}
	return grants
}

// setTemplate writes the template and its grants to the model. An empty
// value or set becomes null, which is the value of an omitted attribute.
func (m *permissionTemplateResourceModel) setTemplate(ctx context.Context, template *client.PermissionTemplate, diagnostics *diag.Diagnostics) {
	m.ID = types.StringValue(template.ID)
	m.Name = types.StringValue(template.Name)
	m.Description = optionalString(template.Description)
	m.ProjectKeyPattern = optionalString(template.ProjectKeyPattern)
	m.setGrants(ctx, template.Grants, diagnostics)
}

// setGrants groups the grants by grantee into the attribute and the blocks.
// An omitted block is an empty set.
func (m *permissionTemplateResourceModel) setGrants(ctx context.Context, grants []client.TemplateGrant, diagnostics *diag.Diagnostics) {
	byKind := map[string]map[string][]string{}
	for _, grant := range grants {
		if byKind[grant.Kind] == nil {
			byKind[grant.Kind] = map[string][]string{}
		}
		byKind[grant.Kind][grant.Name] = append(byKind[grant.Kind][grant.Name], grant.Permission)
	}
	m.ProjectCreatorPermissions = types.SetNull(types.StringType)
	if creator := byKind[client.GranteeProjectCreator][""]; len(creator) > 0 {
		m.ProjectCreatorPermissions = stringSet(ctx, creator, diagnostics)
	}
	m.Groups = blockSet(ctx, templateGroupType, byKind[client.GranteeGroup], newGroupBlock, diagnostics)
	m.Users = blockSet(ctx, templateUserType, byKind[client.GranteeUser], newUserBlock, diagnostics)
}

func blockSet[B templateBlock](ctx context.Context, elementType attr.Type, permissionsByKey map[string][]string,
	newBlock func(string, types.Set) B, diagnostics *diag.Diagnostics) types.Set {
	models := make([]B, 0, len(permissionsByKey))
	for key, permissions := range permissionsByKey {
		models = append(models, newBlock(key, stringSet(ctx, permissions, diagnostics)))
	}
	set, d := types.SetValueFrom(ctx, elementType, models)
	diagnostics.Append(d...)
	return set
}

func stringSet(ctx context.Context, values []string, diagnostics *diag.Diagnostics) types.Set {
	set, d := types.SetValueFrom(ctx, types.StringType, values)
	diagnostics.Append(d...)
	return set
}

func optionalString(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

// grantDiff gives the desired grants that are missing and the current grants
// that are not desired, in a fixed order, so that the requests and the errors
// are the same in each run.
func grantDiff(current, desired []client.TemplateGrant) (adds, removes []client.TemplateGrant) {
	missing := func(from, in []client.TemplateGrant) []client.TemplateGrant {
		var out []client.TemplateGrant
		for _, grant := range from {
			if !slices.Contains(in, grant) {
				out = append(out, grant)
			}
		}
		slices.SortFunc(out, compareGrants)
		return out
	}
	return missing(desired, current), missing(current, desired)
}

func compareGrants(a, b client.TemplateGrant) int {
	return cmp.Or(strings.Compare(a.Kind, b.Kind), strings.Compare(a.Name, b.Name), strings.Compare(a.Permission, b.Permission))
}

func describeGrant(grant client.TemplateGrant) string {
	if grant.Kind == client.GranteeProjectCreator {
		return grant.Permission + " to the project creator"
	}
	return fmt.Sprintf("%s to %s %q", grant.Permission, grant.Kind, grant.Name)
}

// refuseDuplicateBlocks refuses a group or a user that is in two blocks.
func refuseDuplicateBlocks[B templateBlock](ctx context.Context, config tfsdk.Config, block string, diagnostics *diag.Diagnostics) {
	var set types.Set
	diagnostics.Append(config.GetAttribute(ctx, path.Root(block), &set)...)
	seen := map[string]bool{}
	for _, model := range blocks[B](ctx, set, diagnostics) {
		key, _ := model.key()
		if key.IsNull() || key.IsUnknown() {
			continue
		}
		if seen[key.ValueString()] {
			diagnostics.AddAttributeError(path.Root(block), "Duplicate "+block,
				fmt.Sprintf("%q is in more than one %s block. Put all its permissions in one block.", key.ValueString(), block))
			return
		}
		seen[key.ValueString()] = true
	}
}
