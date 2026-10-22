data "sonarqube_quality_profile" "sonar_way_core" {
  organization = "my-organization"
  language     = "java"
  name         = "Sonar way core"
}

resource "sonarqube_quality_profile" "backend" {
  organization = "my-organization"
  language     = "java"
  name         = "Backend"
  parent_key   = data.sonarqube_quality_profile.sonar_way_core.key
}

# A replacement of the parent also deletes the child on the server. The
# lifecycle block makes Terraform replace the child together with the parent.
# It refers to key, which changes only when the parent is replaced, so a
# rename of the parent does not replace the child.
resource "sonarqube_quality_profile" "backend_payments" {
  organization = "my-organization"
  language     = "java"
  name         = "Backend payments"
  parent_key   = sonarqube_quality_profile.backend.key

  lifecycle {
    replace_triggered_by = [sonarqube_quality_profile.backend.key]
  }
}
