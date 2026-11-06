package cloud_test

import (
	"strconv"
	"testing"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider/providertest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestScopedOrganizationTokenTerraformLifecycle(t *testing.T) {
	server := providertest.NewTokenServer(t)
	var id string
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories, Steps: scopedTokenSteps(providertest.TokenOrganization, "test-token", server.ProviderConfig(), &id)})
	server.AssertEmpty(t)
}

func TestScopedOrganizationTokenProjectLifecycle(t *testing.T) {
	server := providertest.NewTokenServer(t)
	hooks := scopedTokenProjectHooks{
		tokenNamed: func(name string) (bool, error) {
			_, exists := server.TokenNamed(name)
			return exists, nil
		},
		unlinkAll: func(tokenID string) error {
			server.Unlink(tokenID)
			return nil
		},
	}
	steps := scopedTokenProjectSteps(t, providertest.TokenOrganization, "test-token", server.ProviderConfig(),
		strconv.Quote(providertest.FirstProjectKey), strconv.Quote(providertest.SecondProjectKey), hooks)
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories, Steps: steps})
	server.AssertEmpty(t)
}
