---
page_title: "SonarQube Provider"
description: |-
  Manage SonarQube Cloud resources with Terraform.
---

# SonarQube Provider

[Managing SonarQube Cloud with Terraform](https://docs.sonarsource.com/sonarqube-cloud/administering-sonarcloud/managing-with-terraform)
in the SonarQube Cloud documentation tells what the provider can manage, the
plan requirements and the known limitations. It also has an example.

This release supports SonarQube Cloud only.

## Requirements

Terraform 1.6.1 or later. Terraform 1.6.0 and earlier refuse a provider
version after its signing key expires, and the signing key of this provider
expires each year.

## Example Usage

```terraform
terraform {
  required_version = ">= 1.6.1"

  required_providers {
    sonarqube = {
      source  = "sonarsource/sonarqube"
      version = "0.1.0-alpha.6"
    }
  }
}

# The token can also come from the SONARQUBE_TOKEN environment variable.
provider "sonarqube" {
  token = var.sonarqube_token
}

variable "sonarqube_token" {
  type      = string
  sensitive = true
}
```

## Select the Instance

* `url` is the address of the SonarQube Cloud web application. The default is
  `https://sonarcloud.io` (the EU region). For the US region, set
  `https://sonarqube.us`.
* `api_url` is the address of Web API v2. The provider gets it from `url`, for
  example `https://api.sonarcloud.io` from `https://sonarcloud.io`. Set it only
  when your instance does not use this pattern.
* `product` is the SonarQube product. Only `cloud` (the default) is supported.

## Authenticate

Use a user token. To make one, open **My Account** > **Security** in SonarQube
Cloud. The provider has the permissions of the user of the token. Use a user
that is dedicated to Terraform, so that the configuration does not depend on
the account of one person.

Set the token in one of these places:

* the `token` argument, from a sensitive variable
* the `SONARQUBE_TOKEN` environment variable

The environment variables `SONARQUBE_URL` and `SONARQUBE_API_URL` set `url` and
`api_url`. A value in the configuration has priority over the environment
variable.

When the provider starts, it checks the token one time. A wrong, expired or
revoked token stops the plan with the error "Invalid token".

### Permissions of the Token

| To manage | The user needs |
| --- | --- |
| An organization | To create it: no permission, unless its enterprise limits who can create organizations. To change or delete it: **Administer Organization** (`admin`). |
| An enterprise | To create it: **Administer Organization** (`admin`) on its first organization. To delete it: enterprise administrator. |
| An organization in an enterprise | Enterprise administrator, and **Administer Organization** (`admin`) on the organization |
| Projects | **Create Projects** (`provisioning`) to create. **Administer** on the project to change or delete. |
| Groups, members and organization permissions | **Administer Organization** (`admin`) |
| Project permissions and project bindings | **Administer Organization** (`admin`), or **Administer** on the project |
| Organization bindings to a DevOps platform | **Administer Organization** (`admin`) |
| Quality gates and the default quality gate | **Administer Quality Gates** (`gateadmin`) |
| `sonarqube_cloud_organization_quality_gate_settings` | **Administer Organization** (`admin`) |
| Automatic Analysis | For a project: **Administer** on the project. For new projects of an organization: **Administer Organization** (`admin`). |
| Quality profiles, their rules and the default profiles | **Administer Quality Profiles** (`profileadmin`). To change a profile, the right to edit it is also enough. |
| Scoped organization tokens | **Administer Organization** (`admin`) |

The pages of the resources tell about other conditions, such as the plan of
the organization.

## Authenticate with a Scoped Organization Token

You can set a scoped organization token (a token that starts with `sqco_`) as
`token`. Then the configuration does not depend on the account of one user.
Many SonarQube Cloud APIs do not accept these tokens yet, so the provider
supports only a small set of resources and data sources with them.

| Resource or data source | With a scoped organization token |
| --- | --- |
| `sonarqube_cloud_organization` data source | Supported |
| `sonarqube_cloud_quality_gate` data source | Supported |
| `sonarqube_quality_profile` data source | Supported |
| `sonarqube_cloud_scoped_organization_token` | Supported. The token must have the `admin` organization permission. `project_keys` works only with public projects. |
| `sonarqube_cloud_organization_quality_gate_settings` | Supported. The token must have the `admin` organization permission. |
| `sonarqube_cloud_organization_automatic_analysis` | Supported. The token must have the `admin` organization permission. |
| All other resources and data sources | Not supported |

Give the token the organization permissions `admin`, `scan` and
`provisioning`. The data sources also work with `scan` or `provisioning` only.

Limits:

* A scoped organization token belongs to one organization. It cannot create,
  change or delete an organization.
* The projects API does not show private projects to a scoped organization
  token. A resource cannot find a private project, so do not manage projects
  with such a token.
* The token has no user. Resources that change groups, permissions, quality
  gates or quality profiles need a user token.

With a scoped organization token, each resource and data source that is not
supported fails with an error before it calls an API. A plan, a refresh, an
import and a destroy fail, and Terraform keeps the state. When an API refuses
a scoped organization token, the error tells that the token is a possible
cause. Use a user token for these resources and data sources.

To stop managing such a resource without a user token, use
`terraform state rm`, or (with Terraform 1.10 or later) a `removed` block with
`destroy = false`. Older Terraform versions refresh the removed resource first,
so run them with `-refresh=false`.

## Import an Existing Setup

`terraform plan -generate-config-out=<file>` writes the configuration of the
resources in `import` blocks. The "Import" section of each resource page gives
the format of the ID. This configuration has hard-coded names and keys,
so Terraform does not know the dependencies between the resources. Replace the
hard-coded values with references to the other resources, for example
`group_name = sonarqube_group.developers.name` instead of
`group_name = "developers"`. Then Terraform creates, changes and deletes the
resources in the correct sequence.

Without references, a destroy deletes a group or a project at the same time as
its permissions or settings. The provider accepts this: when the group, the
project or the quality profile is already deleted, the delete of the dependent
resource succeeds.

## Telemetry

The provider sends no separate telemetry requests. It sends usage data only in
the requests that it makes to manage your resources.

### User-Agent

The provider sends the Terraform version and the provider version in the
User-Agent header of each request, for example
`Terraform/1.9.8 (+https://www.terraform.io) sonarqube-terraform/1.0.0`. This
header is always sent, because support needs it. To add your own text at the
end of the header, set the `TF_APPEND_USER_AGENT` environment variable, for
example `TF_APPEND_USER_AGENT="my-pipeline/1.0"`. The provider removes control
characters and non-ASCII characters from this text, and keeps at most 256
characters.

### Optional Telemetry

The optional telemetry can contain only these values:

* the type name of a resource or data source of this provider, for example
  `sonarqube_project`
* the name of an operation: `create`, `read`, `update`, `delete` or `import`

It never contains attribute values, IDs, keys or other values from your
configuration. This release sends no optional telemetry yet.

To turn off the optional telemetry, do one of these steps:

* Set `disable_telemetry = true` in the provider configuration.
* Set the `SONARQUBE_DISABLE_TELEMETRY` environment variable to `true` or `1`.
* Set the `DO_NOT_TRACK` environment variable to `1`.

When one of these settings turns off the optional telemetry, it is off. The
User-Agent header above stays the same. A value of
`SONARQUBE_DISABLE_TELEMETRY` that is not a boolean stops the provider with an
error.

<!-- schema generated by tfplugindocs -->
## Schema

### Optional

- `api_url` (String) Base address of Web API v2, for example `https://api.sonarcloud.io`. Can also be given with the `SONARQUBE_API_URL` environment variable. Derived from `url` when absent, which is right for every standard deployment.
- `disable_telemetry` (Boolean) Set to `true` to turn off the optional telemetry. The `SONARQUBE_DISABLE_TELEMETRY` environment variable and `DO_NOT_TRACK=1` also turn it off. Defaults to `false`.
- `product` (String) SonarQube product to manage: `cloud` or `server`. Defaults to `cloud`. Only `cloud` works in this release.
- `token` (String, Sensitive) Token that authenticates against the instance. Can also be given with the `SONARQUBE_TOKEN` environment variable.
- `url` (String) Base address of the instance, for example `https://sonarcloud.io`. Can also be given with the `SONARQUBE_URL` environment variable. Defaults to `https://sonarcloud.io` when `product` is `cloud`.
