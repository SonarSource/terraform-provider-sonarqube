package shared_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

func TestAccGroupResource(t *testing.T) {
	organization := os.Getenv(acctest.EnvTestOrganization)
	name := fmt.Sprintf("%sgroup-%d", acctest.KeyPrefix, rand.Uint32())
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
			if err := acctest.WaitFor(groupIs(t.Context(), organization, id, func(group *client.Group) bool {
				return group != nil && group.Name == groupName && group.Description == description
			})); err != nil {
				return err
			}
			// Import finds the group by name, and the name filter has its own lag.
			return acctest.WaitFor(func() (bool, error) {
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
			acctest.PreCheckSharedOrganization(t)
		},
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			return acctest.WaitFor(groupIs(t.Context(), organization, id, func(group *client.Group) bool { return group == nil }))
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
				// The list can show the old name again after it showed the new
				// name, and then the plan finds no change. Read reads again
				// during the 6 seconds of readRetries while it sees another
				// name, so the wait must be longer than that.
				if err := acctest.WaitForSteady(groupIs(t.Context(), organization, id, func(group *client.Group) bool {
					return group != nil && group.Name == outsideName
				}), 10); err != nil {
					t.Fatal(err)
				}
			}, PlanOnly: true, ExpectNonEmptyPlan: true},
			{Config: withoutDescription, Check: shows(renamed, "")},
			{Config: withoutDescription, PreConfig: func() {
				if err := acctest.Client().DeleteGroup(t.Context(), id); err != nil {
					t.Fatal(err)
				}
				// The same wait as for the rename, because the list can show
				// the deleted group again.
				if err := acctest.WaitForSteady(groupIs(t.Context(), organization, id, func(group *client.Group) bool {
					return group == nil
				}), 10); err != nil {
					t.Fatal(err)
				}
			}, PlanOnly: true, ExpectNonEmptyPlan: true},
			{Config: withoutDescription, Check: shows(renamed, "")},
		},
	})
}

// groupIs is a check for acctest.WaitFor that reads the group by id and gives
// it to done, or nil when the organization does not hold it.
func groupIs(ctx context.Context, organization, id string, done func(*client.Group) bool) func() (bool, error) {
	return func() (bool, error) {
		group, err := acctest.Client().GetGroup(ctx, organization, id)
		if errors.Is(err, client.ErrNotFound) {
			group, err = nil, nil
		}
		return err == nil && done(group), err
	}
}
