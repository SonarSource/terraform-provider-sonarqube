---
page_title: "sonarqube_permission_template Resource - SonarQube"
description: |-
  Manages a permission template and all its grants.
---

# sonarqube_permission_template

This resource manages a permission template and all its grants. When a user
creates a project, the template whose project key pattern matches the key
gives its permissions to the new project. When no pattern matches, the
default template of the organization does. A change to a template does not
change the projects that exist.

## All the grants

The resource controls all the grants of the template: the grants of the
groups, the users and the project creator. A grant that is not in the
configuration, also one that a user added in the UI, shows as a change in
the next plan, and the apply removes it.

The resource refuses the `Anyone` group. If the template gives a permission
to `Anyone`, for example after a change in the UI, the next apply removes it.

## Plans

SonarQube Cloud needs the Team or the Enterprise plan to create a template
or to change its grants. On the Free plan, the apply fails. The token must
have the Administer permission on the organization.

## Users

A user must be a member of the organization. If the user is not a member,
the apply fails.

## The default template

SonarQube Cloud refuses to delete the default template. Before you destroy
a template that is the default, set a different template as the default.

## Example Usage

```terraform
resource "sonarqube_group" "developers" {
  organization = "my-organization"
  name         = "Developers"
}

resource "sonarqube_permission_template" "backend" {
  organization        = "my-organization"
  name                = "Backend projects"
  description         = "Grants of the backend projects."
  project_key_pattern = "my-organization_backend-.*"

  project_creator_permissions = ["admin"]

  group {
    name        = sonarqube_group.developers.name
    permissions = ["user", "codeviewer", "issueadmin"]
  }

  user {
    login       = "alice@github"
    permissions = ["admin"]
  }
}
```

## Schema

### Required

- `organization` (String): Key of the organization that owns the template. A change replaces the template.
- `name` (String): Name of the template. The names in an organization must be different, also when the case is ignored.

### Optional

- `description` (String): Description of the template.
- `project_key_pattern` (String): Java regular expression. The template applies to the new projects
  whose key matches it. Two templates of an organization cannot have the same pattern.
- `project_creator_permissions` (Set of String): Project permissions that the template gives to the user who creates the project.
- `group` (Block Set): Project permissions of a group. Each group can appear once.
  - `name` (String, Required): Exact group name, with the same case.
  - `permissions` (Set of String, Required): Project permissions of the group, such as `user` or `admin`.
- `user` (Block Set): Project permissions of a user. Each user can appear once.
  - `login` (String, Required): Exact user login, with the same case.
  - `permissions` (Set of String, Required): Project permissions of the user, such as `user` or `admin`.

The project permissions are `admin`, `codeviewer`, `issueadmin`,
`securityhotspotadmin`, `scan` and `user`. SonarQube Cloud also has
`architectureadmin`.

### Read-Only

- `id` (String): Identifier of the template.

## Import

Import a template with `<organization>/<name>`. The name can contain `/`.
