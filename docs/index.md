---
page_title: "SonarQube Provider"
description: |-
  Manage SonarQube Cloud resources with Terraform.
---

# SonarQube Provider

The SonarQube provider manages the resources of SonarQube Cloud. The alpha
release supports SonarQube Cloud only.

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
      version = "0.1.0-alpha.1"
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

An alpha version is a pre-release. Terraform selects it only when the
constraint gives the exact version.

## Schema

### Optional

- `url` (String) Base address of the instance, for example
  `https://sonarcloud.io`. Can also be given with the `SONARQUBE_URL`
  environment variable. Defaults to `https://sonarcloud.io` when `product` is
  `cloud`.
- `api_url` (String) Base address of Web API v2, for example
  `https://api.sonarcloud.io`. Can also be given with the `SONARQUBE_API_URL`
  environment variable. Derived from `url` when absent, which is right for
  every standard deployment.
- `token` (String, Sensitive) Token that authenticates against the instance.
  Can also be given with the `SONARQUBE_TOKEN` environment variable.
- `product` (String) SonarQube product to manage: `cloud` or `server`.
  Defaults to `cloud`. Only `cloud` works in this release.
