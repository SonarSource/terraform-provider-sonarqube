---
page_title: "sonarqube_cloud_organization_permissions Resource - SonarQube"
subcategory: "SonarQube Cloud"
description: |-
  Manages the organization permissions of one group or user.
---

# sonarqube_cloud_organization_permissions

This resource manages the complete set of organization permissions for one
group or user. Create removes existing grants that are not in `permissions`.
Destroy removes the selected principal's organization grants. It does not
change other principals.

The allowed permissions are `admin`, `gateadmin`, `profileadmin`,
`architectureadmin`, `provisioning`, and `scan`. The `Anyone` group is not
supported. A FREE organization accepts group permission changes.

The resource finds the group by its exact name. If somebody renames the group
outside Terraform, the next plan creates the resource again, and the apply
fails because no group has the configured name. A rename in a `sonarqube_group`
resource replaces this resource and keeps the permissions.

SonarQube Cloud refuses to remove the last organization administrator. If a
plan removes the last `admin` grant, the apply fails and reports the server
error. Keep another administrator before you remove that grant.

## Example Usage

```terraform
resource "sonarqube_cloud_organization_permissions" "developers" {
  organization = "my-org"
  group_name   = "Developers"
  permissions  = ["scan", "gateadmin"]
}
```

## Schema

### Required

- `organization` (String): Organization key.
- `permissions` (Set of String): Complete nonempty permission set.

### Optional

- `group_name` (String): Exact group name. Set this or `login`.
- `login` (String): Exact user login. Set this or `group_name`.

### Read-Only

- `id` (String): Resource identifier.

## Import

Import a group with `<organization>/group/<name>`. Import a user with
`<organization>/user/<login>`.
