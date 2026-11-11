# Import with the organization key. The API never returns the token, so the
# next apply writes the configured access_token to the binding.
terraform import sonarqube_cloud_organization_gitlab_self_managed_binding.example my-organization
