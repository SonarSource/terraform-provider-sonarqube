# The ID of a group is <organization>/<project_key>/group/<name>.
terraform import sonarqube_cloud_project_permissions.developers my-org/my-project/group/Developers

# The ID of a user is <organization>/<project_key>/user/<login>.
terraform import sonarqube_cloud_project_permissions.alice my-org/my-project/user/alice@github
