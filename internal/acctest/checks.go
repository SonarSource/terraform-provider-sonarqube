package acctest

import (
	"errors"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
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

// MissingKey is a key that no object of a test has. The negative tests use it
// for an object that does not exist.
const MissingKey = KeyPrefix + "missing"

// MissingImport is the error of an import of an object that does not exist.
// Read removes the object from the state, and Terraform then refuses the
// import.
var MissingImport = regexp.MustCompile(`Cannot import non-existent remote object`)

// ImportError returns a step that imports id into address and expects an
// error that matches want.
func ImportError(address, id string, want *regexp.Regexp) resource.TestStep {
	return resource.TestStep{ResourceName: address, ImportState: true, ImportStateId: id, ExpectError: want}
}

// PlanAction expects that the plan of a step does action on the object of
// address.
func PlanAction(address string, action plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{
		PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, action)},
	}
}

// DeletedOutside returns a step that deletes the object of address outside of
// Terraform with del, waits until read does not find it, and applies config.
// The refresh removes the object from the state, so the plan must make it
// again. check runs after the apply.
func DeletedOutside[T any](t *testing.T, address, config string, check resource.TestCheckFunc,
	del func() error, read func() (T, error)) resource.TestStep {
	return resource.TestStep{
		PreConfig: func() {
			if err := deleteAndWait(address, del, read); err != nil {
				t.Fatal(err)
			}
		},
		Config:           config,
		ConfigPlanChecks: PlanAction(address, plancheck.ResourceActionCreate),
		Check:            check,
	}
}

func deleteAndWait[T any](address string, del func() error, read func() (T, error)) error {
	if err := del(); err != nil {
		return fmt.Errorf("cannot delete %s outside of Terraform: %w", address, err)
	}
	if err := WaitForGone(read); err != nil {
		return fmt.Errorf("%s is not deleted: %w", address, err)
	}
	return nil
}
