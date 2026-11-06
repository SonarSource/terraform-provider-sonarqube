package cloud_test

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// TestAccBuiltInObjectDataSources reads objects that each organization has.
// It changes nothing, so it also runs with a scoped organization token.
func TestAccBuiltInObjectDataSources(t *testing.T) {
	acctest.PreCheckAcceptance(t)
	acctest.PreCheck(t)
	organization := os.Getenv(acctest.EnvTestOrganization)
	// The names of the built-in profiles and gates are not the same on each instance.
	profiles, err := acctest.Client().ListQualityProfiles(t.Context(), organization, "java")
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(profiles, func(p client.QualityProfile) bool { return p.IsBuiltIn })
	if i < 0 {
		t.Fatal("the organization has no built-in java quality profile")
	}
	org, err := acctest.Client().GetOrganization(t.Context(), organization)
	if err != nil {
		t.Fatal(err)
	}
	gates, err := acctest.Client().ListQualityGates(t.Context(), org.UUIDV4)
	if err != nil {
		t.Fatal(err)
	}
	g := slices.IndexFunc(gates, func(g client.QualityGate) bool { return g.BuiltIn })
	if g < 0 {
		t.Fatal("the organization has no built-in quality gate")
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
data "sonarqube_cloud_organization" "test" {
  key = %[1]q
}
data "sonarqube_cloud_quality_gate" "test" {
  organization = %[1]q
  name         = %[3]q
}
data "sonarqube_quality_profile" "test" {
  organization = %[1]q
  language     = "java"
  name         = %[2]q
}
`, organization, profiles[i].Name, gates[g].Name),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.sonarqube_cloud_organization.test", "key", organization),
				resource.TestCheckResourceAttr("data.sonarqube_cloud_quality_gate.test", "id", gates[g].ID),
				resource.TestCheckResourceAttr("data.sonarqube_quality_profile.test", "key", profiles[i].Key),
				resource.TestCheckResourceAttr("data.sonarqube_quality_profile.test", "is_built_in", "true"),
			),
		}},
	})
}
