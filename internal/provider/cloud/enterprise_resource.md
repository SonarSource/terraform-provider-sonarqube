This resource creates a SonarQube Cloud enterprise with a license and its
first organization. The provider must use a user token. Scoped organization
tokens are not supported.

`key`, `license` and `organization` cannot change after create. A change to
one of them stops the plan. The provider never replaces the enterprise. A
change to `name` or `avatar` updates the enterprise in place.

## Destroy

~> Destroy deletes the enterprise and its portfolios.

- The license cannot be used again. A new enterprise needs a new license.
- The organizations stay, on a paid plan without enterprise features.
- `terraform apply -replace` and `terraform taint` also delete the enterprise.
  The create after that fails, because the license is used up.

We recommend `prevent_destroy`:

```terraform
resource "sonarqube_cloud_enterprise" "main" {
  # ...

  lifecycle {
    prevent_destroy = true
  }
}
```
