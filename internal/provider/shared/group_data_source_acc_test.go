package shared_test

import (
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

func TestAccGroupDataSourceReadsBuiltInGroup(t *testing.T) {
	organization := os.Getenv(acctest.EnvTestOrganization)
	config := fmt.Sprintf(`data "sonarqube_group" "members" {
  organization = %q
  name         = "Members"
}`, organization)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{Config: config, Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("data.sonarqube_group.members", "name", "Members"),
			resource.TestCheckResourceAttrSet("data.sonarqube_group.members", "id"),
			resource.TestCheckResourceAttr("data.sonarqube_group.members", "built_in", "true"),
			resource.TestCheckResourceAttr("data.sonarqube_group.members", "managed", "false"),
		)}},
	})
}

func TestAccGroupDataSourceMissingGroup(t *testing.T) {
	organization := os.Getenv(acctest.EnvTestOrganization)
	config := fmt.Sprintf(`data "sonarqube_group" "missing" {
  organization = %q
  name         = %q
}`, organization, acctest.MissingKey)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      config,
			ExpectError: regexp.MustCompile(`Cannot find the group ` + acctest.MissingKey),
		}},
	})
}

func TestAccGroupDataSourceReadsGroupOfTest(t *testing.T) {
	organization := os.Getenv(acctest.EnvTestOrganization)
	name := fmt.Sprintf("%sdata-group-%d", acctest.KeyPrefix, rand.Uint32())
	group := fmt.Sprintf(`resource "sonarqube_group" "test" {
  organization = %q
  name         = %q
  description  = "Made by an acceptance test."
}`, organization, name)
	data := group + `

data "sonarqube_group" "test" {
  organization = sonarqube_group.test.organization
  name         = sonarqube_group.test.name
}`
	// The first step records the identifier of the group.
	var id string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheckSharedOrganization(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			return acctest.WaitFor(groupIs(t.Context(), organization, id, func(group *client.Group) bool { return group == nil }))
		},
		Steps: []resource.TestStep{
			{
				// The data source finds a group by name, and the name filter
				// shows a new group only after some seconds. So the data
				// source reads in the next step.
				Config: group,
				Check: func(state *terraform.State) error {
					id = state.RootModule().Resources["sonarqube_group.test"].Primary.ID
					return acctest.WaitForSteady(groupNamed(t.Context(), organization, name, id), 3)
				},
			},
			{
				Config: data,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.sonarqube_group.test", "id", "sonarqube_group.test", "id"),
					resource.TestCheckResourceAttr("data.sonarqube_group.test", "description", "Made by an acceptance test."),
					resource.TestCheckResourceAttr("data.sonarqube_group.test", "built_in", "false"),
					resource.TestCheckResourceAttr("data.sonarqube_group.test", "managed", "false"),
				),
			},
		},
	})
}
