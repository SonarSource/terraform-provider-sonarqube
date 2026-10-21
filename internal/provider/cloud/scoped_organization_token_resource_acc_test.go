package cloud_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const scopedTokenAddress = "sonarqube_cloud_scoped_organization_token.test"

func TestAccScopedOrganizationTokenResource(t *testing.T) {
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("%s is not set", resource.EnvTfAcc)
	}
	organization := acctest.PreCheckSharedOrganization(t)
	org, err := acctest.Client().GetOrganization(t.Context(), organization)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("%stoken-%d", acctest.KeyPrefix, rand.Uint32())
	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			_, err := acctest.Client().FindOrganizationToken(t.Context(), org.UUIDV4, id)
			if errors.Is(err, client.ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("token %s still exists", id)
		},
		Steps: scopedTokenSteps(organization, name, "", &id),
	})
}

func scopedTokenSteps(organization, name, providerConfig string, id *string) []resource.TestStep {
	expiration := time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339)
	config := func(permissions string) string {
		return providerConfig + fmt.Sprintf(`
resource "sonarqube_cloud_scoped_organization_token" "test" {
 organization = %q
 name = %q
 organization_permissions = [%q]
 expires_at = %q
}
`, organization, name, permissions, expiration)
	}
	checks := func(s *terraform.State) error {
		attributes := s.RootModule().Resources[scopedTokenAddress].Primary.Attributes
		next := attributes["id"]
		if next == "" || !strings.HasPrefix(attributes["token"], "sqco_") || attributes["created_at"] == "" {
			return errors.New("missing token outputs")
		}
		if *id != "" && *id == next {
			return errors.New("replacement kept the old ID")
		}
		*id = next
		return nil
	}
	return []resource.TestStep{
		{Config: config("scan"), Check: checks},
		{ResourceName: scopedTokenAddress, ImportState: true, ImportStateIdFunc: func(*terraform.State) (string, error) { return organization + "/" + *id, nil }, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"token"}},
		{Config: config("admin"), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(scopedTokenAddress, plancheck.ResourceActionDestroyBeforeCreate)}}, Check: checks},
	}
}
