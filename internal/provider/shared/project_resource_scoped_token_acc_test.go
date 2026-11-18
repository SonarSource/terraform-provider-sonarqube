package shared_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// The test makes its own scoped organization token, so it runs with a user
// token and with SONARQUBE_TEST_SOT_TOKEN.
func TestAccProjectResourceRefusesScopedToken(t *testing.T) {
	acctest.PreCheckAcceptance(t)
	organization := acctest.PreCheckSharedOrganization(t)
	user := acctest.Client()
	org, err := user.GetOrganization(t.Context(), organization)
	if err != nil {
		t.Fatal(err)
	}

	suffix := rand.Uint32()
	token, err := user.CreateOrganizationToken(t.Context(), client.OrganizationTokenRequest{
		OrganizationID: org.UUIDV4,
		Name:           fmt.Sprintf("%stoken-%d", acctest.KeyPrefix, suffix),
		RoleAssignments: []client.TokenRoleAssignment{{
			ResourceType: client.TokenResourceOrganization,
			Roles:        []string{"admin", "scan", "provisioning"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := user.DeleteOrganizationToken(context.Background(), org.UUIDV4, token.ID); err != nil {
			t.Errorf("cannot delete the scoped organization token: %v", err)
		}
	})

	// The removed block takes the private project out of the state only, so
	// the test deletes it. The cleanup covers a run that stops early.
	privateKey := fmt.Sprintf("%sprivate-%d", acctest.KeyPrefix, suffix)
	deletePrivate := func() error { return user.DeleteProject(context.Background(), privateKey) }
	t.Cleanup(func() {
		if err := deletePrivate(); err != nil {
			t.Errorf("cannot delete the project %s: %v", privateKey, err)
		}
	})
	newKey := fmt.Sprintf("%s%d", acctest.KeyPrefix, suffix)

	provider := func(token string) string {
		return fmt.Sprintf("provider \"sonarqube\" {\n  token = %q\n}\n", token)
	}
	project := func(key string) string {
		return fmt.Sprintf(`
resource "sonarqube_project" "test" {
  organization = %q
  key          = %q
  name         = "Project"
}
`, organization, key)
	}
	removed := `
removed {
  from = sonarqube_project.test
  lifecycle {
    destroy = false
  }
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Older versions refresh a resource of a removed block before they
		// forget it, so that step fails.
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{tfversion.RequireAbove(tfversion.Version1_10_0)},
		CheckDestroy: func(state *terraform.State) error {
			if err := deletePrivate(); err != nil {
				return err
			}
			return resource.ComposeAggregateTestCheckFunc(
				acctest.CheckProjectDeleted(t, organization, privateKey),
				acctest.CheckProjectDeleted(t, organization, newKey),
			)(state)
		},
		Steps: []resource.TestStep{
			{
				Config: provider(acctest.UserToken()) + project(privateKey),
				Check:  testAccCheckProjectPrivate(t, organization, privateKey),
			},
			{
				// The refresh fails and keeps the state.
				Config:      provider(token.Token) + project(privateKey),
				ExpectError: regexp.MustCompile(`This needs a user token`),
			},
			{
				Config: provider(token.Token) + removed,
			},
			{
				Config:        provider(token.Token) + project(privateKey),
				ResourceName:  projectAddress,
				ImportState:   true,
				ImportStateId: organization + "/" + privateKey,
				ExpectError:   regexp.MustCompile(`This needs a user token`),
			},
			{
				Config:      provider(token.Token) + project(newKey),
				ExpectError: regexp.MustCompile(`This needs a user token`),
			},
		},
	})
}

func testAccCheckProjectPrivate(t *testing.T, organization, key string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		project, err := acctest.Client().GetProject(t.Context(), organization, key)
		if err != nil {
			return err
		}
		if project.Visibility != "private" {
			return fmt.Errorf("%s must make new projects private, but the project %s is %s",
				acctest.EnvTestOrganization, key, project.Visibility)
		}
		return nil
	}
}
