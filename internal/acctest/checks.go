package acctest

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// WaitForGone calls read until it reports client.ErrNotFound, and stops at
// any other error. Some reads of SonarQube Cloud still show a deleted object
// for some seconds, so one read is not sufficient.
func WaitForGone[T any](read func() (T, error)) error {
	return WaitFor(func() (bool, error) {
		_, err := read()
		if errors.Is(err, client.ErrNotFound) {
			return true, nil
		}
		return false, err
	})
}

// CheckGone is WaitForGone as a check, such as for CheckDestroy. name tells
// which object is still there.
func CheckGone[T any](name string, read func() (T, error)) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if err := WaitForGone(read); err != nil {
			return fmt.Errorf("%s is not deleted: %w", name, err)
		}
		return nil
	}
}

// CheckOrganizationsDeleted reports each organization of keys that the API
// still shows. A 404 can also mean that the token cannot see the
// organization, but the token user made the organizations of a test, so it
// can see them.
func CheckOrganizationsDeleted(t *testing.T, keys ...string) resource.TestCheckFunc {
	checks := make([]resource.TestCheckFunc, len(keys))
	for i, key := range keys {
		checks[i] = CheckGone("organization "+key, func() (*client.Organization, error) {
			return Client().GetOrganization(t.Context(), key)
		})
	}
	return resource.ComposeAggregateTestCheckFunc(checks...)
}

// CheckProjectDeleted reports a project that the API still shows.
func CheckProjectDeleted(t *testing.T, organization, key string) resource.TestCheckFunc {
	return CheckGone("project "+key, func() (*client.Project, error) {
		return Client().GetProject(t.Context(), organization, key)
	})
}
