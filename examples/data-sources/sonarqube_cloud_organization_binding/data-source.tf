data "sonarqube_cloud_organization_binding" "example" {
  organization_key = "my-organization"
}

output "dev_ops_platform" {
  value = data.sonarqube_cloud_organization_binding.example.dev_ops_platform
}
