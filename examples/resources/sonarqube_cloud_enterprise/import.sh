# Import with the enterprise key. The API does not return `license` and
# `organization`, so they are null after import. The next apply writes the
# values of the configuration to the state, and does not change the enterprise.
terraform import sonarqube_cloud_enterprise.main my-enterprise
