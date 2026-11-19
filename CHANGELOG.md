# Changelog

## 0.1.0-alpha.5 (Unreleased)

BREAKING CHANGES:

* With a scoped organization token, each resource and data source that does not support these tokens fails before
  it calls an API. A plan, a refresh, an import and a destroy fail, and Terraform keeps the state. Before, some of
  them worked, but `sonarqube_project` made a private project that the next refresh could not find, and Terraform
  removed it from the state with no warning. The provider docs list the supported resources and data sources. For
  the others, set a user token in the provider, or remove them from the state with `terraform state rm` or a
  `removed` block with `destroy = false` (Terraform 1.10 or later, or `-refresh=false` on older versions).

ENHANCEMENTS:

* `sonarqube_project`: the new attribute `main_branch` sets the main branch of a project. A change renames the
  main branch in place, and the project keeps its analysis history. When you do not set it, the project keeps
  the main branch that SonarQube Cloud chose.
* The provider now sends a User-Agent header with the Terraform version and the provider version, for example
  `Terraform/1.9.8 (+https://www.terraform.io) sonarqube-terraform/0.1.0-alpha.5`. To add your own text at the
  end, set the `TF_APPEND_USER_AGENT` environment variable. The provider removes control characters and
  non-ASCII characters from this text, and keeps at most 256 characters.
* The new public Go package `github.com/SonarSource/terraform-provider-sonarqube/sonarqube` gives the provider
  constructor `New(version)` to Go programs outside this module, for example Crossplane Upjet. This API is
  experimental and can change in any release before version 1.0.0. See the README.
* `sonarqube_project`: a new `key` changes the key in place, and the project keeps its analysis history. Before, it
  replaced the project, which deleted the analysis history, the issues and the settings.
* `sonarqube_project`: the plan refuses a new `name` or `organization`, because SonarQube Cloud cannot rename a
  project or move it to a different organization. Before, the change replaced the project, which deleted the
  analysis history, the issues and the settings. The error tells how to make the project again. A new key of the
  organization also changes `organization`: the docs of `sonarqube_cloud_organization` tell how to keep the
  projects then.
* `sonarqube_cloud_organization`: the docs tell what a new key does to the resources that refer to it, and how to
  keep them.
* Docs: each resource and data source has an example, and each resource has an `import` block example. The
  provider page tells which token permissions each resource needs, and links to the SonarQube Cloud documentation.
  The Registry shows the pages in groups.

BUG FIXES:

* `sonarqube_cloud_organization_permissions` and `sonarqube_cloud_project_permissions`: the destroy succeeds when
  the same destroy deletes the group or the project. Before, the remove failed, for example with the configuration
  from `terraform plan -generate-config-out`, which has no references between resources.
* `sonarqube_quality_profile_rule`: the destroy succeeds when the same destroy deletes the quality profile.
* `sonarqube_project_quality_profile`: a quality profile key that is not found is a warning in the plan, and an
  error in the apply. Before, the plan failed, and a destroy could not start after an earlier destroy that
  deleted the quality profile.
* `sonarqube_group_member`, `sonarqube_cloud_organization_permissions` and `sonarqube_cloud_project_permissions`:
  the apply that creates a group with a recently used name, or restores a group name that was changed outside
  Terraform, can now add members and permissions to the group. For some seconds after such a change, SonarQube
  Cloud can refuse these changes. The create of a member, and the create and update of permissions, then try
  each call again for about ten seconds. Before, the apply failed with status 404 or 500, and a second apply was necessary.
* A wrong, expired or revoked token now gives one clear error, "Invalid token", when the provider starts, for each
  resource and data source. Before, the error was a 403 that looked like a missing permission, or "unexpected status
  401", and it was different for each resource.
* Each read is sent again after a temporary server error, after 1, 2 and 4 seconds. Before, a plan or a destroy with
  a high `-parallelism` value, for example 30, could fail with "status 500: Internal server error".

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
