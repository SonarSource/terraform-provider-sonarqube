data "sonarqube_quality_profile" "sonar_way_core" {
  organization = "my-organization"
  language     = "java"
  name         = "Sonar way core"
}

output "sonar_way_core_key" {
  value = data.sonarqube_quality_profile.sonar_way_core.key
}
