data "sonarqube_cloud_enterprise" "example" {
  key = "my-enterprise"
}

output "enterprise_id" {
  value = data.sonarqube_cloud_enterprise.example.id
}
