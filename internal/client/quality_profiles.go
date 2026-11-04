package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
)

const qualityProfilesPath = "/api/qualityprofiles"

// ErrQualityProfilePlanRefused reports that add_project or remove_project
// refused the call, because the plan of the organization does not permit
// custom quality profiles.
var ErrQualityProfilePlanRefused = errors.New("the plan of the organization does not permit to change the " +
	"quality profile of a project")

// QualityProfile is a quality profile of one language. An empty ParentKey
// means that the profile extends no other profile.
type QualityProfile struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Language  string `json:"language"`
	ParentKey string `json:"parentKey"`
	IsBuiltIn bool   `json:"isBuiltIn"`
	IsDefault bool   `json:"isDefault"`
}

// profileParameters names the organization. SonarQube Server has no
// organizations, so an empty organization sends no parameter.
func profileParameters(organization string) url.Values {
	values := url.Values{}
	if organization != "" {
		values.Set("organization", organization)
	}
	return values
}

// CreateQualityProfile creates a profile with no parent and no rule changes.
func (c *Client) CreateQualityProfile(ctx context.Context, organization, language, name string) (*QualityProfile, error) {
	params := profileParameters(organization)
	params.Set("language", language)
	params.Set("name", name)
	var answer struct {
		Profile QualityProfile `json:"profile"`
	}
	if err := c.postFor(ctx, qualityProfilesPath+"/create", params, &answer); err != nil {
		return nil, qualityProfileError(err, organization)
	}
	if answer.Profile.Key == "" {
		return nil, fmt.Errorf("the create answer for quality profile %q has no key", name)
	}
	return &answer.Profile, nil
}

// ChangeQualityProfileParent sets the parent of the profile with the key. An
// empty parentKey removes the parent. The key is unique on the instance, so
// the write actions take no organization. The web service also takes the
// parameter qualityProfile, but that is the name of the profile, not its key.
func (c *Client) ChangeQualityProfileParent(ctx context.Context, key, parentKey string) error {
	params := url.Values{"key": {key}}
	if parentKey != "" {
		params.Set("parentKey", parentKey)
	}
	return c.post(ctx, qualityProfilesPath+"/change_parent", params)
}

// GetQualityProfileParentKey returns the key of the parent of the profile, or
// an empty key when the profile has no parent. The web service takes the
// profile key without the organization, and lists the ancestors from the
// parent up.
func (c *Client) GetQualityProfileParentKey(ctx context.Context, key string) (string, error) {
	var answer struct {
		Ancestors []struct {
			Key string `json:"key"`
		} `json:"ancestors"`
	}
	if err := c.get(ctx, qualityProfilesPath+"/inheritance", url.Values{"key": {key}}, &answer); err != nil {
		return "", err
	}
	if len(answer.Ancestors) == 0 {
		return "", nil
	}
	return answer.Ancestors[0].Key, nil
}

// RenameQualityProfile sets the name of the profile with the key.
func (c *Client) RenameQualityProfile(ctx context.Context, key, name string) error {
	return c.post(ctx, qualityProfilesPath+"/rename", url.Values{"key": {key}, "name": {name}})
}

// DeleteQualityProfile deletes the profile with the key and all its
// descendants. A profile that is already gone is success. The server refuses
// to delete a profile when the profile or a descendant is a default profile.
func (c *Client) DeleteQualityProfile(ctx context.Context, key string) error {
	err := c.postIgnoringNotFound(ctx, qualityProfilesPath+"/delete", url.Values{"key": {key}})
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && hasMessageContaining(apiErr.Messages, "is marked as default") {
		return fmt.Errorf("the profile or one of its descendants is the default profile of the language. "+
			"Set a different default profile first: %w", err)
	}
	return err
}

// ListQualityProfiles returns the profiles of the organization for the
// language, or for all languages when language is empty. The web service has
// no pages.
func (c *Client) ListQualityProfiles(ctx context.Context, organization, language string) ([]QualityProfile, error) {
	return c.searchQualityProfiles(ctx, organization, language, "", false)
}

// GetQualityProfile returns the profile with the key, or ErrNotFound. The
// search gives the parent, which show does not.
func (c *Client) GetQualityProfile(ctx context.Context, organization, language, key string) (*QualityProfile, error) {
	profiles, err := c.ListQualityProfiles(ctx, organization, language)
	if err != nil {
		return nil, err
	}
	return findByKey(profiles, key, func(profile QualityProfile) string { return profile.Key })
}

// FindQualityProfileByName returns the profile with the exact name, also a
// built-in one, or ErrNotFound. Names are case-sensitive.
func (c *Client) FindQualityProfileByName(ctx context.Context, organization, language, name string) (*QualityProfile, error) {
	profiles, err := c.searchQualityProfiles(ctx, organization, language, name, false)
	if err != nil {
		return nil, err
	}
	return findByKey(profiles, name, func(profile QualityProfile) string { return profile.Name })
}

// GetDefaultQualityProfile returns the default profile of the language, or
// ErrNotFound when the search gives none.
func (c *Client) GetDefaultQualityProfile(ctx context.Context, organization, language string) (*QualityProfile, error) {
	profiles, err := c.searchQualityProfiles(ctx, organization, language, "", true)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(profiles, func(profile QualityProfile) bool { return profile.IsDefault })
	if i < 0 {
		return nil, ErrNotFound
	}
	return &profiles[i], nil
}

// SetDefaultQualityProfile makes the profile with the key the default of its
// own language. The web service does not check a language, so the caller must
// check that the profile is of the expected language.
func (c *Client) SetDefaultQualityProfile(ctx context.Context, key string) error {
	return setDefaultQualityProfileError(c.post(ctx, qualityProfilesPath+"/set_default", url.Values{"key": {key}}))
}

// setDefaultQualityProfileError explains the refusals that the server message
// does not make clear.
func setDefaultQualityProfileError(err error) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case isMissingMisraEntitlement(apiErr):
		return misraProfileError(err)
	case apiErr.StatusCode == http.StatusForbidden:
		return fmt.Errorf("SonarQube Cloud refused the change. Check that the token has the Administer Quality "+
			"Profiles permission of the organization: %w", err)
	case apiErr.StatusCode == http.StatusBadRequest && hasMessageContaining(apiErr.Messages, "is not allowed to perform this action"):
		return fmt.Errorf("the plan of the organization does not permit custom quality profiles. Use a built-in "+
			"profile, or move the organization to the Team or the Enterprise plan: %w", err)
	}
	return err
}

// searchQualityProfiles gives only the default profiles when defaults is true.
func (c *Client) searchQualityProfiles(ctx context.Context, organization, language, name string, defaults bool) ([]QualityProfile, error) {
	params := profileParameters(organization)
	if language != "" {
		params.Set("language", language)
	}
	if name != "" {
		params.Set("qualityProfile", name)
	}
	if defaults {
		params.Set("defaults", "true")
	}
	var answer struct {
		Profiles []QualityProfile `json:"profiles"`
	}
	if err := c.get(ctx, qualityProfilesPath+"/search", params, &answer); err != nil {
		return nil, qualityProfileError(err, organization)
	}
	return answer.Profiles, nil
}

// qualityProfileError makes a missing organization a ScopeNotFoundError. The
// other messages of the server are clear, for example for a parent of a
// different language or for a member of a combined quality profile group.
func qualityProfileError(err error, organization string) error {
	if isMissingOrganization(err) {
		return &ScopeNotFoundError{Kind: ScopeOrganization, Key: organization, Cause: err}
	}
	return err
}

// isMissingOrganization reports the 404 of an organization key that does not
// exist.
func isMissingOrganization(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound &&
		hasMessagePrefix(apiErr.Messages, "No organization with key ")
}

// AddQualityProfileProject makes the project use the profile for the language
// of the profile. It replaces the profile that the project used for that
// language, and it does not change the other languages. The key is unique on
// the instance, so the call takes no organization.
func (c *Client) AddQualityProfileProject(ctx context.Context, key, project string) error {
	err := c.post(ctx, qualityProfilesPath+"/add_project", url.Values{"key": {key}, "project": {project}})
	return projectQualityProfileError(err, project)
}

// RemoveQualityProfileProject makes the project use the default profile of the
// language again. The call succeeds when the project does not use the
// profile. A missing project or profile is success too, but a missing
// permission is not.
func (c *Client) RemoveQualityProfileProject(ctx context.Context, key, project string) error {
	err := projectQualityProfileError(c.post(ctx, qualityProfilesPath+"/remove_project",
		url.Values{"key": {key}, "project": {project}}), project)
	if errors.Is(err, ErrProjectNotFound) || errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// GetProjectQualityProfile returns the profile that the project uses for the
// language: the profile that add_project gave, or else the default profile.
// It returns a ScopeNotFoundError for a missing organization, and for a
// project that is missing or is in a different organization. search does not
// check a permission on the project, so it has no 404 for a permission.
func (c *Client) GetProjectQualityProfile(ctx context.Context, organization, project, language string) (*QualityProfile, error) {
	params := profileParameters(organization)
	params.Set("project", project)
	params.Set("language", language)
	var answer struct {
		Profiles []QualityProfile `json:"profiles"`
	}
	if err := c.get(ctx, qualityProfilesPath+"/search", params, &answer); err != nil {
		switch {
		case isMissingOrganization(err):
			return nil, &ScopeNotFoundError{Kind: ScopeOrganization, Key: organization, Cause: err}
		case errors.Is(err, ErrNotFound):
			return nil, &ScopeNotFoundError{Kind: ScopeProject, Key: project, Cause: err}
		}
		return nil, err
	}
	if len(answer.Profiles) == 0 {
		return nil, ErrNotFound
	}
	return &answer.Profiles[0], nil
}

// isMissingComponent reports the 404 of a project key that does not exist.
func isMissingComponent(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound && hasMessagePrefix(apiErr.Messages, "Component key ")
}

// projectQualityProfileError explains the refusals of add_project and
// remove_project. A missing project becomes a ScopeNotFoundError, and a
// missing profile stays a 404 that matches ErrNotFound. The server also
// answers 404 when the token has no permission, so that it does not show that
// the project exists. That 404 has a different message and becomes an error
// that does not match ErrNotFound.
func projectQualityProfileError(err error, project string) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case isMissingComponent(err):
		return &ScopeNotFoundError{Kind: ScopeProject, Key: project, Cause: err}
	case apiErr.StatusCode == http.StatusNotFound && hasMessagePrefix(apiErr.Messages, "Quality Profile with key "):
		return err
	case apiErr.StatusCode == http.StatusNotFound:
		return fmt.Errorf("the server refused the call. Check that the token has the Administer Quality "+
			"Profiles permission, the permission to edit the profile, or the Administer permission of the "+
			"project: %s", apiErr.Error())
	case isMissingMisraEntitlement(apiErr):
		return misraProfileError(err)
	case apiErr.StatusCode == http.StatusBadRequest && hasMessageContaining(apiErr.Messages, "is not allowed to perform this action"):
		return fmt.Errorf("%w. On SonarQube Cloud, move the organization to the Team or the Enterprise plan: %w",
			ErrQualityProfilePlanRefused, err)
	}
	return err
}

// misraProfileError explains the refusal of a MISRA profile in an
// organization without the MISRA entitlement.
func misraProfileError(err error) error {
	return fmt.Errorf("the profile is a MISRA profile, and the organization does not have the MISRA "+
		"entitlement that it needs. Use a different profile, or get the entitlement for the organization: %w", err)
}
