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
		return fmt.Errorf("the profile is a MISRA profile, and the organization does not have the MISRA "+
			"entitlement that it needs. Use a different profile, or get the entitlement for the organization: %w", err)
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
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound &&
		hasMessagePrefix(apiErr.Messages, "No organization with key ") {
		return &ScopeNotFoundError{Kind: ScopeOrganization, Key: organization, Cause: err}
	}
	return err
}
