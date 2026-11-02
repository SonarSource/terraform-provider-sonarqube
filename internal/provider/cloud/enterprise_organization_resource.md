This resource attaches an organization to a SonarQube Cloud enterprise. The
provider must use a user token. Scoped organization tokens are not supported.

An organization can be in one enterprise only. If the organization is already
in the enterprise, create adopts it. If it is in a different enterprise,
create fails. An organization on the free plan moves to a paid plan when it is
attached.

~> Do not declare the first organization of `sonarqube_cloud_enterprise`
with this resource. `sonarqube_cloud_enterprise` attaches it already, and the
destroy of this resource fails when the organization is the last one in the
enterprise.

## Destroy

Destroy detaches the organization. The organization stays, on a paid plan
without enterprise features.

If the organization is the last one in the enterprise, destroy fails, because
the server deletes the enterprise when its last organization is detached.
Terraform destroys this resource before the enterprise, so a destroy of
`sonarqube_cloud_enterprise` in the same run stops at this resource too. Do
these steps in this order:

1. Remove this resource from the state with a `removed` block (Terraform 1.7
   or later) or with `terraform state rm`. The organization stays attached.
2. To delete the enterprise, destroy `sonarqube_cloud_enterprise`. The
   organizations stay, on a paid plan without enterprise features.

```terraform
removed {
  from = sonarqube_cloud_enterprise_organization.second

  lifecycle {
    destroy = false
  }
}
```
