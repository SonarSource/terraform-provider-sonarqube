package shared_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

func TestAccGroupResource(t *testing.T) {
	organization := os.Getenv(acctest.EnvTestOrganization)
	name := fmt.Sprintf("terraform-group-%d", rand.Uint32())
	renamed := name + "-renamed"
	outsideName := renamed + "-outside"
	config := func(groupName, description string) string {
		return fmt.Sprintf(`resource "sonarqube_group" "test" {
  organization = %q
  name         = %q
  description  = %q
}`, organization, groupName, description)
	}
	withoutDescription := fmt.Sprintf(`resource "sonarqube_group" "test" {
  organization = %q
  name         = %q
}`, organization, renamed)
	address := "sonarqube_group.test"
	// The group list is eventually consistent, so each step waits until a
	// read by id, as Read does, shows the change. Then Terraform refreshes.
	var id string
	shows := func(groupName, description string) resource.TestCheckFunc {
		return func(state *terraform.State) error {
			id = state.RootModule().Resources[address].Primary.ID
			if err := waitForGroup(t, organization, id, func(group *client.Group) bool {
				return group != nil && group.Name == groupName && group.Description == description
			}); err != nil {
				return err
			}
			// Import finds the group by name, and the name filter has its own lag.
			return waitFor(func() (bool, error) {
				group, err := acctest.Client().FindGroupByName(t.Context(), organization, groupName)
				if errors.Is(err, client.ErrNotFound) {
					return false, nil
				}
				return err == nil && group.ID == id, err
			})
		}
	}
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheckDestructive(t)
			if organization == "" {
				t.Skipf("%s is not set", acctest.EnvTestOrganization)
			}
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			return waitForGroup(t, organization, id, func(group *client.Group) bool { return group == nil })
		},
		Steps: []resource.TestStep{
			{Config: config(name, "Initial"), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "name", name),
				shows(name, "Initial"),
			)},
			{Config: config(name, "Initial")},
			{Config: config(renamed, "Changed"), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "name", renamed),
				resource.TestCheckResourceAttr(address, "description", "Changed"),
				shows(renamed, "Changed"),
			)},
			{ResourceName: address, ImportState: true, ImportStateId: organization + "/" + renamed, ImportStateVerify: true},
			{Config: withoutDescription, Check: shows(renamed, "")},
			{Config: withoutDescription, PreConfig: func() {
				if err := acctest.Client().UpdateGroup(t.Context(), id, outsideName, ""); err != nil {
					t.Fatal(err)
				}
				if err := waitForGroup(t, organization, id, func(group *client.Group) bool {
					return group != nil && group.Name == outsideName
				}); err != nil {
					t.Fatal(err)
				}
			}, PlanOnly: true, ExpectNonEmptyPlan: true},
			{Config: withoutDescription, Check: shows(renamed, "")},
			{Config: withoutDescription, PreConfig: func() {
				if err := acctest.Client().DeleteGroup(t.Context(), id); err != nil {
					t.Fatal(err)
				}
				if err := waitForGroup(t, organization, id, func(group *client.Group) bool { return group == nil }); err != nil {
					t.Fatal(err)
				}
			}, PlanOnly: true, ExpectNonEmptyPlan: true},
			{Config: withoutDescription, Check: shows(renamed, "")},
		},
	})
}

// waitForGroup reads the group by id until done accepts it, or nil when the
// organization does not hold it.
func waitForGroup(t *testing.T, organization, id string, done func(*client.Group) bool) error {
	t.Helper()
	return waitFor(func() (bool, error) {
		group, err := acctest.Client().GetGroup(t.Context(), organization, id)
		if errors.Is(err, client.ErrNotFound) {
			group, err = nil, nil
		}
		return err == nil && done(group), err
	})
}

// waitFor calls check each second until it reports true, an error, or 60
// seconds pass.
func waitFor(check func() (bool, error)) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		ok, err := check()
		if err != nil || ok {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the group list did not show the expected state in 60 seconds")
		}
		time.Sleep(time.Second)
	}
}
