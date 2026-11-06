package cloud

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

const cannotReadTokenProjects = "Cannot read the token projects"

// tokenLinks holds the resource links of a token, split by resource type.
type tokenLinks struct {
	// projects maps the UUID of each linked project to the ID of its link.
	projects map[string]string
	others   []client.TokenResourceLink
}

func partitionLinks(links []client.TokenResourceLink) tokenLinks {
	result := tokenLinks{projects: map[string]string{}}
	for _, link := range links {
		if link.ResourceType == client.TokenResourceProject {
			result.projects[link.ResourceID] = link.ID
		} else {
			result.others = append(result.others, link)
		}
	}
	return result
}

// linkChanges returns the wanted projects without a link, and the sorted IDs
// of the project links to remove. These include links to deleted projects.
// Links to other resource types stay.
func linkChanges(wanted []client.Project, current tokenLinks) (add []client.Project, remove []string) {
	keep := make(map[string]bool, len(wanted))
	for _, project := range wanted {
		keep[project.ID] = true
		if _, linked := current.projects[project.ID]; !linked {
			add = append(add, project)
		}
	}
	for projectID, linkID := range current.projects {
		if !keep[projectID] {
			remove = append(remove, linkID)
		}
	}
	slices.Sort(remove)
	return add, remove
}

func (r *scopedOrganizationTokenResource) applyLinkChanges(ctx context.Context, tokenID string, add []client.Project, remove []string) error {
	for _, project := range add {
		if _, err := r.client.CreateTokenResourceLink(ctx, tokenID, client.TokenResourceProject, project.ID); err != nil {
			return fmt.Errorf("cannot link the token to the project %s: %w", project.Key, err)
		}
	}
	for _, linkID := range remove {
		if err := r.client.DeleteTokenResourceLink(ctx, linkID); err != nil {
			return fmt.Errorf("cannot remove the token resource link %s: %w", linkID, err)
		}
	}
	return nil
}

// revokeUnlinkedToken revokes a new token after a link failure. When the
// revocation fails too, the token stays in state, so that Terraform marks it
// as tainted and replaces it.
func (r *scopedOrganizationTokenResource) revokeUnlinkedToken(ctx context.Context, organizationID string, plan scopedOrganizationTokenModel, linkErr error, resp *resource.CreateResponse) {
	revokeErr := r.client.DeleteOrganizationToken(ctx, organizationID, plan.ID.ValueString())
	if revokeErr == nil {
		resp.Diagnostics.AddError("Cannot link the token to its projects", linkErr.Error()+". The provider revoked the new token.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.AddError("Cannot link the token to its projects", fmt.Sprintf("%s. The provider cannot revoke the new token: %s. The next apply replaces it.", linkErr, revokeErr))
}

// plannedProjects finds the organization and the projects of project_keys,
// sorted by key. A missing project is an error.
func (r *scopedOrganizationTokenResource) plannedProjects(ctx context.Context, plan scopedOrganizationTokenModel, diagnostics *diag.Diagnostics) (*client.Organization, []client.Project, bool) {
	keys, known := knownStrings(ctx, plan.ProjectKeys, diagnostics)
	if !known {
		if !diagnostics.HasError() {
			diagnostics.AddError("Unknown project keys", "Project keys must be known at apply time.")
		}
		return nil, nil, false
	}
	org, ok := lookupOrganization(ctx, r.client, plan.Organization.ValueString(), path.Root("organization"), diagnostics)
	if !ok || len(keys) == 0 {
		return org, nil, ok
	}
	slices.Sort(keys)
	projects, err := r.client.FindProjectsByKey(ctx, org.UUIDV4, keys)
	if err != nil {
		diagnostics.AddError(cannotReadTokenProjects, err.Error())
		return nil, nil, false
	}
	found, missing := matchProjects(keys, projects, func(p client.Project) string { return p.Key })
	if len(missing) > 0 {
		diagnostics.AddAttributeError(path.Root("project_keys"), "Project not found",
			"No project with the key "+strings.Join(missing, ", ")+" exists in the organization. Check the key, and check that the provider token can read the project.")
		return nil, nil, false
	}
	return org, found, true
}

// readProjectKeys returns the project_keys state, and false when the token is
// gone. project_keys is null for an organization-wide token, because the
// resource does not manage the links of such a token. On an error, the prior
// value stays.
func (r *scopedOrganizationTokenResource) readProjectKeys(ctx context.Context, organizationID string, state scopedOrganizationTokenModel, organizationWide bool, diagnostics *diag.Diagnostics) (types.Set, bool) {
	if organizationWide {
		return types.SetNull(types.StringType), true
	}
	links, err := r.client.ListTokenResourceLinks(ctx, state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		return state.ProjectKeys, false
	}
	if err != nil {
		diagnostics.AddError(cannotReadTokenProjects, err.Error())
		return state.ProjectKeys, true
	}
	keys, ok := linkedProjectKeys(ctx, r.client, organizationID, partitionLinks(links), diagnostics)
	if !ok {
		return state.ProjectKeys, true
	}
	return stringSetState(ctx, keys, state.ProjectKeys, diagnostics), true
}

// linkedProjectKeys returns the key of each linked project. The server keeps
// the link of a deleted project, so such a link gives a warning and no key.
// Links to other resource types give a warning, and the resource keeps them.
func linkedProjectKeys(ctx context.Context, c *client.Client, organizationID string, links tokenLinks, diagnostics *diag.Diagnostics) ([]string, bool) {
	if len(links.others) > 0 {
		others := make([]string, len(links.others))
		for i, link := range links.others {
			others[i] = link.ResourceType + " " + link.ResourceID
		}
		diagnostics.AddWarning("Unsupported token resource links", "The token has links to resources that are not projects: "+strings.Join(others, ", ")+". This resource does not manage them, and does not remove them.")
	}
	if len(links.projects) == 0 {
		return nil, true
	}
	ids := slices.Sorted(maps.Keys(links.projects))
	projects, err := c.FindProjectsByID(ctx, organizationID, ids)
	if err != nil {
		diagnostics.AddError(cannotReadTokenProjects, err.Error())
		return nil, false
	}
	found, missing := matchProjects(ids, projects, func(p client.Project) string { return p.ID })
	if len(missing) > 0 {
		diagnostics.AddWarning("Token linked to a missing project",
			"The token has links to projects that do not exist, or that the provider token cannot read: "+strings.Join(missing, ", ")+
				". These projects are not in project_keys. The next change of project_keys removes these links.")
	}
	keys := make([]string, len(found))
	for i, project := range found {
		keys[i] = project.Key
	}
	return keys, true
}

// matchProjects returns, in the order of values, the project for each value,
// and the values without a project. A project that matches no value is
// ignored, because a list can hold more than the values asked for.
func matchProjects(values []string, projects []client.Project, by func(client.Project) string) (found []client.Project, missing []string) {
	index := make(map[string]client.Project, len(projects))
	for _, project := range projects {
		index[by(project)] = project
	}
	for _, value := range values {
		if project, ok := index[value]; ok {
			found = append(found, project)
		} else {
			missing = append(missing, value)
		}
	}
	return found, missing
}
