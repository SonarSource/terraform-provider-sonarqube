resource "sonarqube_cloud_enterprise_organization" "second" {
  enterprise_id = sonarqube_cloud_enterprise.main.id
  organization  = "my-second-organization"
}
