resource "sonarqube_quality_profile" "backend" {
  organization = "my-organization"
  language     = "java"
  name         = "Backend"
}

# The project uses the Backend profile for java. For the other languages, it
# uses the default profiles of the organization.
resource "sonarqube_project_quality_profile" "backend" {
  organization        = "my-organization"
  project_key         = "my-project"
  quality_profile_key = sonarqube_quality_profile.backend.key
}
