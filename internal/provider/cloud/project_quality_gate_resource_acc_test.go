package cloud_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

const projectQualityGateAddress = "sonarqube_cloud_project_quality_gate.test"

// A new organization is on the Free plan, which cannot assign a gate to a
// project. The test therefore uses the organization of
// SONARQUBE_TEST_ORGANIZATION, which must be on the Team or the Enterprise
// plan. The test makes its own project and gates, and does not change the
// default gate of the organization.
//
// The last step removes only the assignment from the configuration, and keeps
// the project. Its check reads from the API that the project uses the default
// gate again. The final destroy deletes the project too, so it cannot show this.
func TestAccProjectQualityGateResource(t *testing.T) {
	organization := os.Getenv(acctest.EnvTestOrganization)
	suffix := rand.Uint32()
	projectKey := fmt.Sprintf("%s_%s%d", organization, acctest.KeyPrefix, suffix)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckProjectDeleted(t, organization, projectKey),
		Steps: []resource.TestStep{
			{
				Config: testAccProjectQualityGateConfig(organization, projectKey, suffix, "first", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(projectQualityGateAddress, "id", organization+"/"+projectKey),
					resource.TestCheckResourceAttrPair(projectQualityGateAddress, "quality_gate_id",
						"sonarqube_cloud_quality_gate.first", "id"),
					resource.TestCheckResourceAttrSet(projectQualityGateAddress, "association_id"),
				),
			},
			{
				// A different gate changes the assignment in place.
				Config: testAccProjectQualityGateConfig(organization, projectKey, suffix, "second", true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(projectQualityGateAddress, plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttrPair(projectQualityGateAddress, "quality_gate_id",
					"sonarqube_cloud_quality_gate.second", "id"),
			},
			{
				// The gate of the project changes outside of Terraform, here to
				// the default. The plan shows it, and the apply assigns the
				// configured gate again.
				PreConfig: func() { testAccRemoveProjectGate(t, organization, projectKey) },
				Config:    testAccProjectQualityGateConfig(organization, projectKey, suffix, "second", true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(projectQualityGateAddress, plancheck.ResourceActionCreate)},
				},
			},
			{
				ResourceName:      projectQualityGateAddress,
				ImportState:       true,
				ImportStateId:     organization + "/" + projectKey,
				ImportStateVerify: true,
			},
			{
				// The configuration drops the assignment and keeps the project.
				// The destroy of the assignment must put the project back on
				// the default gate.
				Config: testAccProjectQualityGateConfig(organization, projectKey, suffix, "second", false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(projectQualityGateAddress, plancheck.ResourceActionDestroy)},
				},
				Check: testAccCheckProjectUsesDefaultGate(t, organization, projectKey),
			},
		},
	})
}

// testAccRemoveProjectGate puts the project back on the default gate, as a
// change in the UI does.
func testAccRemoveProjectGate(t *testing.T, organization, projectKey string) {
	c := acctest.Client()
	org, project := testAccReadProject(t, c, organization, projectKey)
	association, err := c.FindQualityGateProjectAssociation(t.Context(), org.UUIDV4, project)
	if err != nil {
		t.Fatalf("cannot read the quality gate of the project: %v", err)
	}
	if err := c.DeleteQualityGateProjectAssociation(t.Context(), association.ID); err != nil {
		t.Fatalf("cannot remove the quality gate outside of Terraform: %v", err)
	}
}

func testAccReadProject(t *testing.T, c *client.Client, organization, projectKey string) (*client.Organization, *client.Project) {
	t.Helper()
	org, err := c.GetOrganization(t.Context(), organization)
	if err != nil {
		t.Fatalf("cannot read the organization: %v", err)
	}
	project, err := c.GetProject(t.Context(), organization, projectKey)
	if err != nil {
		t.Fatalf("cannot read the project: %v", err)
	}
	return org, project
}

// testAccCheckProjectUsesDefaultGate reads from the API that the project has
// no gate of its own.
func testAccCheckProjectUsesDefaultGate(t *testing.T, organization, projectKey string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		c := acctest.Client()
		org, project := testAccReadProject(t, c, organization, projectKey)
		_, err := c.FindQualityGateProjectAssociation(t.Context(), org.UUIDV4, project)
		if errors.Is(err, client.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("project %s still has its own quality gate", projectKey)
	}
}

// testAccCheckProjectDeleted runs after the destroy. It accepts only a project
// that is not found, and not any other error.
func testAccCheckProjectDeleted(t *testing.T, organization, projectKey string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		_, err := acctest.Client().GetProject(t.Context(), organization, projectKey)
		if errors.Is(err, client.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("project %s still exists", projectKey)
	}
}

func testAccProjectQualityGateConfig(organization, projectKey string, suffix uint32, gate string, assign bool) string {
	config := fmt.Sprintf(`
resource "sonarqube_project" "test" {
  organization = %[1]q
  key          = %[2]q
  name         = "Terraform acceptance test"
}

resource "sonarqube_cloud_quality_gate" "first" {
  organization = %[1]q
  name         = "Terraform project gate first %[3]d"
}

resource "sonarqube_cloud_quality_gate" "second" {
  organization = %[1]q
  name         = "Terraform project gate second %[3]d"
}
`, organization, projectKey, suffix)
	if !assign {
		return config
	}
	return config + fmt.Sprintf(`
resource "sonarqube_cloud_project_quality_gate" "test" {
  organization    = %[1]q
  project_key     = sonarqube_project.test.key
  quality_gate_id = sonarqube_cloud_quality_gate.%[2]s.id
}
`, organization, gate)
}
