# Changelog

## 0.1.0-alpha.7 (Unreleased)

ENHANCEMENTS:

* The new provider attribute `disable_telemetry`, the `SONARQUBE_DISABLE_TELEMETRY` environment variable and
  `DO_NOT_TRACK=1` turn off the optional telemetry. This release sends no optional telemetry yet. The User-Agent
  header does not change. The provider docs list the data that the provider sends.

## 0.1.0-alpha.6 (November 25, 2026)

The tag `v0.1.0-alpha.5` exists, but `0.1.0-alpha.5` was not released. This release contains its changes.

BREAKING CHANGES:

* With a scoped organization token, each resource and data source that does not support these tokens now fails
  before it calls an API. The provider docs list the supported ones. For the others, use a user token, or remove
  them from the state with `terraform state rm` or a `removed` block with `destroy = false`.

FEATURES:

* **New Resource:** `sonarqube_cloud_project_automatic_analysis`
* **New Resource:** `sonarqube_cloud_organization_automatic_analysis`

ENHANCEMENTS:

* `sonarqube_project`: the new attribute `main_branch` sets the main branch. A change renames it in place.
* `sonarqube_project`: a new `key` changes the key in place, and the project keeps its analysis history.
* `sonarqube_project`: the plan refuses a new `name` or `organization`, because SonarQube Cloud cannot rename or
  move a project. Before, the project was replaced, and its analysis history was deleted.
* The provider sends its version and the Terraform version in the User-Agent header. Set `TF_APPEND_USER_AGENT`
  to add your own text.
* The new Go package `github.com/SonarSource/terraform-provider-sonarqube/sonarqube` gives the provider
  constructor `New(version)` to other Go programs. This API is experimental until version 1.0.0.
* Docs: an example for each resource and data source, an `import` example for each resource, and the token
  permissions that each resource needs.

BUG FIXES:

* `sonarqube_cloud_organization_permissions`, `sonarqube_cloud_project_permissions` and
  `sonarqube_quality_profile_rule`: the destroy succeeds when the same destroy deletes the group, the project or
  the quality profile.
* `sonarqube_project_quality_profile`: a quality profile key that is not found is a warning in the plan, not an error.
* `sonarqube_group_member` and the permission resources: the apply that creates a group with a recently used name
  can add members and permissions. Before, a second apply was necessary.
* A wrong, expired or revoked token gives one clear error, "Invalid token".
* A read is sent again after a temporary server error.

## 0.1.0-alpha.4 (November 13, 2026)

FEATURES:

* **New Resource:** `sonarqube_cloud_organization_gitlab_self_managed_binding`

ENHANCEMENTS:

* `sonarqube_cloud_project_binding` and its data source: bind a project of an organization that is bound to GitLab
  Self-Managed. The repository is the full path of the GitLab project, for example `group/subgroup/name`.

## 0.1.0-alpha.3 (November 9, 2026)

BREAKING CHANGES:

* **Removed Resource:** `sonarqube_permission_template`. A permission template applies only when SonarQube
  creates a project. After that, the template has no link to the project.
* **Removed Resource:** `sonarqube_cloud_organization_default_permission_template`.
* Before you upgrade, run `terraform state rm` for each of these resources, then remove their blocks from your
  configuration. If you remove the blocks and apply first, Terraform destroys the templates. With Terraform 1.7
  or later, you can instead replace each block with a `removed` block with `lifecycle { destroy = false }` and
  apply it before you upgrade.
* If you already upgraded, `terraform plan` fails with `no schema available for <resource> while reading state`.
  A `removed` block does not fix this. Run `terraform state rm` for each of these resources.
  These steps do not change the templates or the default template in SonarQube.
* `sonarqube_cloud_scoped_organization_token`: a token without `organization_permissions` now requires
  `project_keys`. Before, such a token had no projects. When you upgrade, add `project_keys` with the projects
  of the token in the same change. The next apply links the token to them in place. The provider now also reads
  the projects of such a token, and the next plan shows links that were made outside Terraform.

FEATURES:

* **New Resource:** `sonarqube_cloud_organization_default_quality_profile`
* **New Resource:** `sonarqube_quality_profile_rule`
* **New Resource:** `sonarqube_project_quality_profile`
* **New Resource:** `sonarqube_quality_profile_editor`

ENHANCEMENTS:

* `sonarqube_cloud_scoped_organization_token`: add `project_keys` to limit a token to specific projects. A change
  updates the token in place, and the secret stays the same.
* Provider: when an API refuses a scoped organization token, the error tells that the token is a possible cause.
  The provider docs list the resources and data sources that support these tokens.

## 0.1.0-alpha.2 (November 2, 2026)

FEATURES:

* **New Resource:** `sonarqube_quality_profile`
* **New Data Source:** `sonarqube_quality_profile`
* **New Resource:** `sonarqube_cloud_scoped_organization_token`
* **New Resource:** `sonarqube_cloud_enterprise`
* **New Data Source:** `sonarqube_cloud_enterprise`
* **New Resource:** `sonarqube_cloud_enterprise_organization`

## 0.1.0-alpha.1 (October 21, 2026)

FEATURES:

* **New Resource:** `sonarqube_cloud_organization`
* **New Resource:** `sonarqube_cloud_organization_binding`
* **New Resource:** `sonarqube_cloud_organization_default_permission_template`
* **New Resource:** `sonarqube_cloud_organization_default_quality_gate`
* **New Resource:** `sonarqube_cloud_organization_permissions`
* **New Resource:** `sonarqube_cloud_organization_quality_gate_settings`
* **New Resource:** `sonarqube_cloud_project_binding`
* **New Resource:** `sonarqube_cloud_project_permissions`
* **New Resource:** `sonarqube_cloud_project_quality_gate`
* **New Resource:** `sonarqube_cloud_quality_gate`
* **New Resource:** `sonarqube_group`
* **New Resource:** `sonarqube_group_member`
* **New Resource:** `sonarqube_permission_template`
* **New Resource:** `sonarqube_project`
* **New Data Source:** `sonarqube_cloud_dop_applications`
* **New Data Source:** `sonarqube_cloud_organization`
* **New Data Source:** `sonarqube_cloud_organization_binding`
* **New Data Source:** `sonarqube_cloud_project_binding`
* **New Data Source:** `sonarqube_cloud_quality_gate`
* **New Data Source:** `sonarqube_group`
