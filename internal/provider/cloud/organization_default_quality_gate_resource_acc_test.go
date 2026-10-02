package cloud_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
)

const (
	defaultQualityGateAddress = "sonarqube_cloud_organization_default_quality_gate.test"
	sonarWayDataAddress       = "data.sonarqube_cloud_quality_gate.sonar_way"
)

// A new organization is on the Free plan, which cannot change its default
// gate. The test therefore uses the organization of SONARQUBE_TEST_ORGANIZATION,
// which must be on the Team or the Enterprise plan.
//
// The test makes its gate outside of Terraform. The destroy does not change the
// default, and SonarQube Cloud refuses to delete the default gate, so a gate of
// the configuration could stay behind after a failed step. The cleanup first
// sets the original default again, then deletes the gate.
func TestAccOrganizationDefaultQualityGateResource(t *testing.T) {
	// The configuration of each step needs the gate, so the gate is made
	// before resource.Test, which would skip the test without TF_ACC.
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("%s is not set", resource.EnvTfAcc)
	}
	acctest.PreCheck(t)
	acctest.PreCheckDestructive(t)
	organization := os.Getenv(acctest.EnvTestOrganization)
	gateID := testAccPrepareDefaultQualityGate(t, organization)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccDefaultQualityGateConfig(organization, fmt.Sprintf("%q", gateID)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(defaultQualityGateAddress, "id", organization),
					resource.TestCheckResourceAttr(defaultQualityGateAddress, "quality_gate_id", gateID),
					testAccCheckDefaultQualityGate(t, organization, gateID),
				),
			},
			{
				// A change of the default outside of Terraform shows in the
				// plan, and the apply sets the configured gate again.
				PreConfig: func() { testAccSetSonarWayAsDefault(t, organization) },
				Config:    testAccDefaultQualityGateConfig(organization, fmt.Sprintf("%q", gateID)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(defaultQualityGateAddress, plancheck.ResourceActionUpdate)},
				},
				Check: testAccCheckDefaultQualityGate(t, organization, gateID),
			},
			{
				ResourceName:      defaultQualityGateAddress,
				ImportState:       true,
				ImportStateId:     organization,
				ImportStateVerify: true,
			},
			{
				// A built-in gate can be the default too.
				Config: testAccDefaultQualityGateConfig(organization, sonarWayDataAddress+".id"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(defaultQualityGateAddress, plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttrPair(defaultQualityGateAddress, "quality_gate_id", sonarWayDataAddress, "id"),
			},
		},
	})
}

// testAccPrepareDefaultQualityGate makes a gate for the test and registers the
// cleanup. It returns the UUID of the gate.
func testAccPrepareDefaultQualityGate(t *testing.T, organization string) string {
	c := acctest.Client()
	org, err := c.GetOrganization(t.Context(), organization)
	if err != nil {
		t.Fatalf("cannot read the organization: %v", err)
	}
	original := org.DefaultQualityGateUUID
	gate, err := c.CreateQualityGate(t.Context(), org.UUIDV4, fmt.Sprintf("Terraform default gate %d", rand.Uint32()))
	if err != nil {
		t.Fatalf("cannot make the quality gate: %v", err)
	}
	// The cleanups run in the opposite order, so the default changes before
	// the delete. The context of the test is canceled before the cleanups run.
	t.Cleanup(func() {
		if err := c.DeleteQualityGate(context.Background(), gate.ID); err != nil {
			t.Errorf("cannot delete the quality gate %s: %v", gate.ID, err)
		}
	})
	t.Cleanup(func() {
		if err := c.SetDefaultQualityGate(context.Background(), organization, original); err != nil {
			t.Errorf("cannot set the original default quality gate %s again: %v", original, err)
		}
	})
	return gate.ID
}

// testAccCheckDefaultQualityGate compares the default of the organization in
// the API with want.
func testAccCheckDefaultQualityGate(t *testing.T, organization, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		org, err := acctest.Client().GetOrganization(t.Context(), organization)
		if err != nil {
			return err
		}
		if org.DefaultQualityGateUUID != want {
			return fmt.Errorf("default quality gate = %q, want %q", org.DefaultQualityGateUUID, want)
		}
		return nil
	}
}

func testAccSetSonarWayAsDefault(t *testing.T, organization string) {
	c := acctest.Client()
	org, err := c.GetOrganization(t.Context(), organization)
	if err != nil {
		t.Fatalf("cannot read the organization: %v", err)
	}
	sonarWay, err := c.FindQualityGate(t.Context(), org.UUIDV4, "Sonar way")
	if err != nil {
		t.Fatalf("cannot find Sonar way: %v", err)
	}
	if err := c.SetDefaultQualityGate(t.Context(), organization, sonarWay.ID); err != nil {
		t.Fatalf("cannot change the default outside of Terraform: %v", err)
	}
}

func testAccDefaultQualityGateConfig(organization, gateReference string) string {
	return fmt.Sprintf(`
data "sonarqube_cloud_quality_gate" "sonar_way" {
  organization = %[1]q
  name         = "Sonar way"
}

resource "sonarqube_cloud_organization_default_quality_gate" "test" {
  organization    = %[1]q
  quality_gate_id = %[2]s
}
`, organization, gateReference)
}
