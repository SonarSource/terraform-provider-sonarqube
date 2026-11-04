resource "sonarqube_quality_profile" "backend" {
  organization = "my-organization"
  language     = "java"
  name         = "Backend"
}

# The members of the group can edit the Backend profile.
resource "sonarqube_quality_profile_editor" "backend_team" {
  organization        = "my-organization"
  quality_profile_key = sonarqube_quality_profile.backend.key
  group_name          = "backend-team"
}

# The user can edit the Backend profile too.
resource "sonarqube_quality_profile_editor" "lead" {
  organization        = "my-organization"
  quality_profile_key = sonarqube_quality_profile.backend.key
  login               = "my-login@github"
}
