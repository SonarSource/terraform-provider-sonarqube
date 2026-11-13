# Changelog

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
