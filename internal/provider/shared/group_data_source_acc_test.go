package shared_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
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
		)}},
	})
}
