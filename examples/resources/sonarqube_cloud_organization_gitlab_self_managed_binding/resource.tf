variable "gitlab_token" {
  description = "GitLab personal access token with the api scope, of an account that owns the group."
  type        = string
  sensitive   = true
}

resource "sonarqube_cloud_organization" "example" {
  key  = "my-organization"
  name = "My organization"
}

# A binding to GitLab Self-Managed needs the Enterprise plan, so the
# organization must belong to an enterprise.
data "sonarqube_cloud_enterprise" "example" {
  key = "my-enterprise"
}

resource "sonarqube_cloud_enterprise_organization" "example" {
  enterprise_id = data.sonarqube_cloud_enterprise.example.id
  organization  = sonarqube_cloud_organization.example.key
}

resource "sonarqube_cloud_organization_gitlab_self_managed_binding" "example" {
  organization_key = sonarqube_cloud_enterprise_organization.example.organization
  server_url       = "https://gitlab.example.com"
  group            = "my-group"
  access_token     = var.gitlab_token
}

# A destroy removes the organization and the binding together. The API cannot
# remove a binding on its own: the server drops it when it deletes the
# organization.
