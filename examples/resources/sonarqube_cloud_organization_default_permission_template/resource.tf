# The destroy of this resource does not change the default. Before you destroy
# a template that is the default, set a different template as the default.
resource "sonarqube_cloud_organization_default_permission_template" "example" {
  organization = "my-organization"
  template_id  = sonarqube_permission_template.example.id
}
