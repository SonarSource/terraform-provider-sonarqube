# The ID of a group is <organization>/group/<name>.
terraform import sonarqube_cloud_organization_permissions.developers my-org/group/Developers

# The ID of a user is <organization>/user/<login>.
terraform import sonarqube_cloud_organization_permissions.alice my-org/user/alice@github
