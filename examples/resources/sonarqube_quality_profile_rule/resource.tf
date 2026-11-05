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

# Activates a rule that the parent profile does not activate, and sets one of
# its parameters. The other parameters keep their default values.
resource "sonarqube_quality_profile_rule" "too_many_parameters" {
  quality_profile_key = sonarqube_quality_profile.backend.key
  rule                = "java:S107"
  params = {
    max = "5"
  }
}

# Changes the severity of rules that the profile gets from its parent. A
# destroy gives the rules the values of the parent again.
resource "sonarqube_quality_profile_rule" "security" {
  for_each = toset(["java:S2077", "java:S3329"])

  quality_profile_key = sonarqube_quality_profile.backend.key
  rule                = each.value
  impacts = {
    SECURITY = "BLOCKER"
  }
}

# Deactivates a rule that the profile gets from its parent. A destroy
# activates the rule again, with the values of the parent.
resource "sonarqube_quality_profile_rule" "hardcoded_ip" {
  quality_profile_key = sonarqube_quality_profile.backend.key
  rule                = "java:S1313"
  active              = false
}
