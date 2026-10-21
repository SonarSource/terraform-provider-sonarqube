# Import with `<organization>/<token_id>`.
#
# Import refuses a token whose automatic project grants do not match the
# permission rules above. Use a token with matching grants, or revoke it before
# creating a replacement. Refresh also reports this mismatch and keeps the
# previous state.
#
# An imported resource has a null `token` value. Replace the resource if a
# consumer needs a new secret. Import does not recover a lost secret.
#
# If a create request times out or returns an incomplete response, check the
# organization for the requested name before retrying. The API might have
# created the token. Import its UUID for cleanup, or revoke it before creating
# a replacement. Do not assume that a failed request made no change.
terraform import sonarqube_cloud_scoped_organization_token.analysis 'my-org/00000000-0000-4000-8000-000000000002'
