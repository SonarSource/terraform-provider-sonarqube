package cloud_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// TestAccGeneratedConfigDestroy imports an existing setup with the
// configuration that -generate-config-out writes: hard-coded names and keys,
// so no dependencies. One destroy then deletes the groups and projects at the
// same time as their grants, and it must complete. Before the destroy, the
// quality profile is deleted, as an earlier destroy that failed can do. The
// plan of the destroy must not fail on the hard-coded key of that profile.
func TestAccGeneratedConfigDestroy(t *testing.T) {
	acctest.SkipWithSOT(t, acctest.SOTRefused)
	acctest.PreCheckAcceptance(t)
	organization := acctest.PreCheckSharedOrganization(t)
	api := acctest.Client()
	ctx := t.Context()
	suffix := rand.Uint32()
	groupName := fmt.Sprintf("%sgenerated-%d", acctest.KeyPrefix, suffix)
	projectKey := fmt.Sprintf("%sgenerated-%d", acctest.KeyPrefix, suffix)
	profileName := fmt.Sprintf("%sgenerated-%d", acctest.KeyPrefix, suffix)

	group, err := api.CreateGroup(ctx, organization, groupName, "")
	if err != nil {
		t.Fatal(err)
	}
	// The context of the test is canceled before the cleanups run.
	t.Cleanup(func() { _ = api.DeleteGroup(context.Background(), group.ID) })
	if err := api.CreateProject(ctx, organization, projectKey, "Terraform acceptance test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = api.DeleteProject(context.Background(), projectKey) })
	profile, err := api.CreateQualityProfile(ctx, organization, "java", profileName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = api.DeleteQualityProfile(context.Background(), profile.Key) })

	config := fmt.Sprintf(`resource "sonarqube_group" "developers" {
  organization = %[1]q
  name         = %[2]q
}
resource "sonarqube_project" "app" {
  organization = %[1]q
  key          = %[3]q
  name         = "Terraform acceptance test"
}
resource "sonarqube_quality_profile" "app_java" {
  organization = %[1]q
  language     = "java"
  name         = %[4]q
}
resource "sonarqube_cloud_organization_permissions" "developers" {
  organization = %[1]q
  group_name   = %[2]q
  permissions  = ["gateadmin"]
}
resource "sonarqube_cloud_project_permissions" "developers" {
  organization = %[1]q
  project_key  = %[3]q
  group_name   = %[2]q
  permissions  = ["issueadmin", "user"]
}
resource "sonarqube_project_quality_profile" "app_java" {
  organization        = %[1]q
  project_key         = %[3]q
  quality_profile_key = %[5]q
}
resource "sonarqube_quality_profile_rule" "app_java" {
  quality_profile_key = %[5]q
  rule                = "java:S107"
}
`, organization, groupName, projectKey, profileName, profile.Key)
	imports := fmt.Sprintf(`import {
  to = sonarqube_group.developers
  id = "%[1]s/%[2]s"
}
import {
  to = sonarqube_project.app
  id = "%[1]s/%[3]s"
}
import {
  to = sonarqube_quality_profile.app_java
  id = "%[1]s/java/%[4]s"
}
`, organization, groupName, projectKey, profileName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			acctest.CheckProjectDeleted(t, organization, projectKey),
			acctest.CheckGone("group "+groupName, func() (*client.Group, error) { return api.GetGroup(ctx, organization, group.ID) }),
			acctest.CheckGone("quality profile "+profileName, func() (*client.QualityProfile, error) {
				return api.GetQualityProfile(ctx, organization, "java", profile.Key)
			}),
		),
		Steps: []resource.TestStep{
			{Config: imports + config},
			{
				PreConfig: func() {
					if err := api.DeleteQualityProfile(ctx, profile.Key); err != nil {
						t.Fatal(err)
					}
				},
				Config:  config,
				Destroy: true,
			},
		},
	})
}
