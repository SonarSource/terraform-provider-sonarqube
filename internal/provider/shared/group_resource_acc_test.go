package shared_test

import (
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
	// The group search is eventually consistent, so each step waits until
	// the search shows the change before Terraform refreshes.
	shows := func(groupName, description string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			return waitForGroup(t, organization, groupName, func(group *client.Group) bool {
				return group != nil && group.Description == description
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
				group, err := acctest.Client().FindGroupByName(t.Context(), organization, renamed)
				if err != nil {
					t.Fatal(err)
				}
				if err := acctest.Client().UpdateGroup(t.Context(), group.ID, &outsideName, nil); err != nil {
					t.Fatal(err)
				}
				if err := waitForGroup(t, organization, outsideName, func(group *client.Group) bool { return group != nil }); err != nil {
					t.Fatal(err)
				}
			}, PlanOnly: true, ExpectNonEmptyPlan: true},
			{Config: withoutDescription, Check: shows(renamed, "")},
			{Config: withoutDescription, PreConfig: func() {
				group, err := acctest.Client().FindGroupByName(t.Context(), organization, renamed)
				if err != nil {
					t.Fatal(err)
				}
				if err := acctest.Client().DeleteGroup(t.Context(), group.ID); err != nil {
					t.Fatal(err)
				}
				if err := waitForGroup(t, organization, renamed, func(group *client.Group) bool { return group == nil }); err != nil {
					t.Fatal(err)
				}
			}, PlanOnly: true, ExpectNonEmptyPlan: true},
			{Config: withoutDescription, Check: shows(renamed, "")},
		},
	})
}

// waitForGroup searches the organization until done accepts the group with the
// name, or nil when the search does not hold it.
func waitForGroup(t *testing.T, organization, name string, done func(*client.Group) bool) error {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		groups, err := acctest.Client().ListGroups(t.Context(), organization, "")
		if err != nil {
			return err
		}
		var found *client.Group
		for i := range groups {
			if groups[i].Name == name {
				found = &groups[i]
			}
		}
		if done(found) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the search did not show the expected state of group %q in 60 seconds; last seen: %+v", name, found)
		}
		time.Sleep(time.Second)
	}
}
