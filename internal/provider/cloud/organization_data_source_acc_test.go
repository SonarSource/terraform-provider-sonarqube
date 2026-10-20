package cloud_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
)

func TestAccOrganizationDataSource(t *testing.T) {
	organization := os.Getenv(acctest.EnvTestOrganization)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "sonarqube_cloud_organization" "test" {
  key = %[1]q
}
`, organization),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.sonarqube_cloud_organization.test", "key", organization),
					// The name of the shared organization is not in this
					// repository, so the check reads it from the API.
					func(s *terraform.State) error {
						org, err := acctest.Client().GetOrganization(t.Context(), organization)
						if err != nil {
							return err
						}
						return resource.TestCheckResourceAttr("data.sonarqube_cloud_organization.test", "name", org.Name)(s)
					},
				),
			},
		},
	})
}
