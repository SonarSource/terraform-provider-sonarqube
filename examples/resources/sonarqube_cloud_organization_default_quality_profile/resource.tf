# The destroy of this resource does not change the default. Before you destroy
# a profile that is the default, set a different profile as the default.
resource "sonarqube_cloud_organization_default_quality_profile" "java" {
  organization        = "my-organization"
  language            = "java"
  quality_profile_key = sonarqube_quality_profile.backend.key
}
