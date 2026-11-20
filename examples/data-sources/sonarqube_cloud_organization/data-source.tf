data "sonarqube_cloud_organization" "example" {
  key = "my-organization"
}

output "organization_name" {
  value = data.sonarqube_cloud_organization.example.name
}
