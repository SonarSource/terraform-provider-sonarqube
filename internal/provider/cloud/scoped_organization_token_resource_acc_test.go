package cloud_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"slices"
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

// scopedTokenProjectHooks connects the project steps to the server under test.
type scopedTokenProjectHooks struct {
	// tokenNamed tells if a token with the name exists.
	tokenNamed func(name string) (bool, error)
	// unlinkAll removes all links of the token, as an edit in the UI does.
	unlinkAll func(tokenID string) error
}

// scopedTokenProjectSteps creates a token for the first project, adds the
// second project, then removes the first one. setup declares the projects, and
// first and second are expressions that give their keys.
func scopedTokenProjectSteps(t *testing.T, organization, name, setup, first, second string, hooks scopedTokenProjectHooks) []resource.TestStep {
	config := func(keys string) string {
		return setup + fmt.Sprintf(`
resource "sonarqube_cloud_scoped_organization_token" "test" {
 organization        = %q
 name                = %q
 project_permissions = ["scan"]
 project_keys        = [%s]
}
`, organization, name, keys)
	}
	var id, secret string
	same := func(s *terraform.State) error {
		attributes := s.RootModule().Resources[scopedTokenAddress].Primary.Attributes
		if id == "" {
			id, secret = attributes["id"], attributes["token"]
			if id == "" || secret == "" {
				return errors.New("missing token outputs")
			}
			return nil
		}
		if attributes["id"] != id || attributes["token"] != secret {
			return errors.New("the project change replaced the token")
		}
		return nil
	}
	update := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(scopedTokenAddress, plancheck.ResourceActionUpdate)}}
	return []resource.TestStep{
		{Config: config(`"` + acctest.KeyPrefix + `missing-project"`), ExpectError: regexp.MustCompile(`Project not found`)},
		{
			PreConfig: func() {
				if exists, err := hooks.tokenNamed(name); err != nil || exists {
					t.Fatalf("a missing project left a token: exists=%v, err=%v", exists, err)
				}
			},
			Config: config(first),
			Check:  resource.ComposeAggregateTestCheckFunc(same, resource.TestCheckResourceAttr(scopedTokenAddress, "project_keys.#", "1")),
		},
		{Config: config(first + ", " + second), ConfigPlanChecks: update, Check: resource.ComposeAggregateTestCheckFunc(same, resource.TestCheckResourceAttr(scopedTokenAddress, "project_keys.#", "2"))},
		{Config: config(second), ConfigPlanChecks: update, Check: resource.ComposeAggregateTestCheckFunc(same, resource.TestCheckResourceAttr(scopedTokenAddress, "project_keys.#", "1"))},
		{ResourceName: scopedTokenAddress, ImportState: true, ImportStateIdFunc: func(*terraform.State) (string, error) { return organization + "/" + id, nil }, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"token"}},
		{
			PreConfig: func() {
				if err := hooks.unlinkAll(id); err != nil {
					t.Fatal(err)
				}
			},
			Config: config(second), ConfigPlanChecks: update, Check: same,
		},
	}
}

func TestAccScopedOrganizationTokenProjects(t *testing.T) {
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("%s is not set", resource.EnvTfAcc)
	}
	organization := acctest.PreCheckSharedOrganization(t)
	org, err := acctest.Client().GetOrganization(t.Context(), organization)
	if err != nil {
		t.Fatal(err)
	}
	suffix := rand.Uint32()
	name := fmt.Sprintf("%stoken-%d", acctest.KeyPrefix, suffix)
	setup := fmt.Sprintf(`
resource "sonarqube_project" "first" {
 organization = %[1]q
 key          = "%[2]stoken-first-%[3]d"
 name         = "Terraform acceptance test"
}
resource "sonarqube_project" "second" {
 organization = %[1]q
 key          = "%[2]stoken-second-%[3]d"
 name         = "Terraform acceptance test"
}
`, organization, acctest.KeyPrefix, suffix)
	hooks := scopedTokenProjectHooks{
		tokenNamed: func(name string) (bool, error) {
			tokens, err := acctest.Client().ListOrganizationTokens(t.Context(), org.UUIDV4)
			return slices.ContainsFunc(tokens, func(token client.OrganizationToken) bool { return token.Name == name }), err
		},
		unlinkAll: func(tokenID string) error {
			links, err := acctest.Client().ListTokenResourceLinks(t.Context(), tokenID)
			for _, link := range links {
				err = errors.Join(err, acctest.Client().DeleteTokenResourceLink(t.Context(), link.ID))
			}
			return err
		},
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			exists, err := hooks.tokenNamed(name)
			if err == nil && exists {
				return fmt.Errorf("token %s still exists", name)
			}
			return err
		},
		Steps: scopedTokenProjectSteps(t, organization, name, setup, "sonarqube_project.first.key", "sonarqube_project.second.key", hooks),
	})
}
