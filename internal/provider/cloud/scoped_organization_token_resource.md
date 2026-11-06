This resource creates a scoped organization token on SonarQube Cloud. The
organization must have a plan that supports these tokens. The provider must
use a token whose user can administer the organization.

A change to `project_keys` updates the token in place, and the secret stays
the same. Each change to another configurable attribute replaces the token.
Destroy revokes it. The provider checks that the token is absent after
revocation.

The secret example also requires the GitHub provider and access to the
organization. Terraform marks `token` as sensitive, but stores its value in
state. Use a state backend with encryption and restricted access.

## Permissions

Set at least one organization or project permission. Organization permissions
are `admin`, `scan`, and `provisioning`. Project permissions are `user`,
`admin`, and `scan`.

If both sets have values, `organization_permissions` must include
`provisioning`. The provider then enables automatic project permission grants.

## Projects

A token with only project permissions gets them only on the projects in
`project_keys`. Set at least one project key. `project_keys` is not permitted
on a token with organization permissions, and the provider does not read or
change the project links of such a token.

Before the provider creates the token, it makes sure that each project exists.
If a project does not exist, `apply` stops and no token is created. If the
provider cannot link the token to a project, it revokes the new token.

When someone changes the projects of the token outside Terraform, the next
plan shows the difference. When someone deletes a project, the token keeps a
link to it, but the provider removes the key from the state and shows a
warning. Remove the key from the configuration, or the next apply stops
because the project does not exist.

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
