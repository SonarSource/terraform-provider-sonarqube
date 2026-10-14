---
page_title: "sonarqube_group_member Resource - SonarQube"
description: |-
  Adds one user to one group.
---

# sonarqube_group_member

This resource adds one user to one group. Destroy removes the user from the
group. It does not change the other members of the group. A change to any
attribute replaces the membership.

## Organization members only

The user must already be a member of the organization. This resource does not
add the user to the organization. If the user is not a member, the apply
fails.

## Groups that SCIM synchronizes

Do not use this resource for a group that an identity provider synchronizes
through SCIM. The next synchronization can remove the user again, so the plan
never converges. The provider refuses to add a member to such a group. Add
the user to the group in the identity provider.

## Groups that SonarQube Cloud refuses

SonarQube Cloud refuses the default group `Members`. Each member of the
organization is in this group.

SonarQube Cloud also refuses to remove a user from any group when no other
user is an administrator of the organization. This rule applies also to a
group that has no permissions. Then the destroy fails. To prevent this, give
the `admin` permission directly to a user, or add another user to a group
that has it.

## Delays

The member list of SonarQube Cloud can show the old members for some seconds
after a change. When a refresh does not find the user, it reads the list
again for about six seconds. A removal outside Terraform thus shows some
seconds later than other changes.

## Example Usage

```terraform
resource "sonarqube_group" "developers" {
  organization = "my-organization"
  name         = "Developers"
}

resource "sonarqube_group_member" "alice" {
  organization = "my-organization"
  group_name   = sonarqube_group.developers.name
  login        = "alice@github"
}
```

## Schema

### Required

- `organization` (String): Key of the organization that owns the group.
- `group_name` (String): Exact name of the group, with the same case.
- `login` (String): Login of the user, with the same case. It cannot contain `/`. The user must be a member of the organization.

### Read-Only

- `id` (String): `<organization>/<group_name>/<login>`.

## Import

Import a membership with `<organization>/<group_name>/<login>`. The group name
can contain `/`.
