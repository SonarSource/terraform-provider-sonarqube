# The ID is <organization>/<quality_profile_key>/group/<group_name> for a group.
terraform import sonarqube_quality_profile_editor.backend_team "my-organization/my-profile-key/group/backend-team"

# The ID is <organization>/<quality_profile_key>/user/<login> for a user.
terraform import sonarqube_quality_profile_editor.lead "my-organization/my-profile-key/user/my-login@github"
