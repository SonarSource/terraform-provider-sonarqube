# The data source can also read the built-in "Sonar way" gate.
data "sonarqube_cloud_quality_gate" "example" {
  organization = "my-organization"
  name         = "Sonar way"
}

output "quality_gate_id" {
  value = data.sonarqube_cloud_quality_gate.example.id
}
