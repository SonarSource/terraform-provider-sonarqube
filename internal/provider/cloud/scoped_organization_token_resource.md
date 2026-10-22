This resource creates a scoped organization token on SonarQube Cloud. The
organization must have a plan that supports these tokens. The provider must
use a token whose user can administer the organization.

Each change to a configurable attribute replaces the token. Destroy revokes
it. The provider checks that the token is absent after revocation.

The secret example also requires the GitHub provider and access to the
organization. Terraform marks `token` as sensitive, but stores its value in
state. Use a state backend with encryption and restricted access.

## Permissions

Set at least one organization or project permission. Organization permissions
are `admin`, `scan`, and `provisioning`. Project permissions are `user`,
`admin`, and `scan`.

If both sets have values, `organization_permissions` must include
`provisioning`. The provider then enables automatic project permission grants.
A token with only project permissions needs separate project assignments
before it can access those projects. This resource does not manage assignments.

## Expiration and rotation

`expires_at` is optional. If set, it must be an RFC 3339 timestamp in the
future, at most one year from creation. The API checks this limit. If omitted,
the token has no expiration date.

If the server removes a token, including one removed after 60 days without
use, the next plan creates a replacement.

An expired token can still exist and reserve its name. The next Terraform
plan replaces it. If the configuration has a fixed date in the past, change
that date before apply. Refresh alone does not revoke an expired token.

For scheduled rotation, use the Time provider and run Terraform regularly:

```terraform
resource "time_rotating" "analysis" {
  rotation_days = 30
}

resource "sonarqube_cloud_scoped_organization_token" "analysis" {
  organization             = "my-org"
  name                     = "ci-analysis"
  organization_permissions = ["scan"]

  lifecycle {
    replace_triggered_by = [time_rotating.analysis]
  }
}
```

The old token is revoked before its replacement is created. Consumers can
fail until they receive the new secret. Do not use `create_before_destroy`
with the same name: token names must be unique in the organization. To allow
both tokens to exist during rotation, give each replacement a unique name
and check how consumers receive the new secret.
