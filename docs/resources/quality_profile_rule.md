---
page_title: "sonarqube_quality_profile_rule Resource - SonarQube"
subcategory: ""
description: |-
  This resource activates a rule in a custom quality profile, and sets the severity of its impacts and the values of its parameters. It can also change a rule that the profile gets from its parent profile.
---

# sonarqube_quality_profile_rule (Resource)

This resource activates a rule in a custom quality profile, and sets the severity of its impacts and the values of its parameters. It can also change a rule that the profile gets from its parent profile.

Use one resource for each rule. To manage many rules, use `for_each`. The provider does not read the rules that no resource manages, so a change to such a rule in the UI shows no drift.

The built-in profiles are read-only. Create a `sonarqube_quality_profile` that extends a built-in profile, and change the rules of that profile.

## Values that the configuration does not set

The provider manages only the software qualities in `impacts` and the parameters in `params` that the configuration sets. The other ones keep the value of the parent profile, or the default value of the rule, and a change to them in the UI shows no drift. When you remove a key from `impacts` or `params`, the apply resets the rule first. The reset applies to all the values of the rule, also to the ones that the configuration does not set, such as a change in the UI. They then get the value of the parent profile, or the default value of the rule, again.

The server ignores a software quality that the rule does not have. The apply then fails, and the error gives the software qualities of the rule.

## Create and destroy

When the profile already activates the rule, the create takes it over and does not fail. When the profile activates the rule itself, a destroy deactivates the rule, also when the rule was active before the create. When the profile gets the rule from its parent, a destroy does not deactivate the rule: the rule gets the values of the parent profile again.

## Deactivate an inherited rule

Set `active = false` to deactivate a rule that the profile gets from its parent profile. Do not set `impacts` and `params` then. The apply fails when the parent does not activate the rule. The deactivation also applies to the profiles that extend this profile, and it removes the changes that they make to the rule. A destroy, or a change to `active = true`, activates the rule again in those profiles with the values of this profile, so their changes do not come back. Set them again, for example with a `sonarqube_quality_profile_rule` for each such profile.

A change of the rule in the parent profile activates the rule again in this profile. The next plan shows this, and the apply deactivates the rule again. When the parent does not activate the rule any more, the refresh removes the resource, and the next apply fails, because there is no inherited rule to deactivate. A destroy activates the rule again, with the values of the parent profile. A change to `active = true` also activates the rule again, with the values of the parent profile and the configured values.

The setting `sonar.qualityProfiles.allowDisableInheritedRules` must be `true`, which is the default. On SonarQube Server, an administrator can change it.

## MISRA rules

The MISRA rules of C and C++ need a MISRA entitlement of the organization. Without it, the apply fails.

## Import

Import a rule with `<quality_profile_key>/<rule>`, for example `my-profile-key/java:S107`. The import keeps the impacts and the parameters that differ from the values of the parent profile, for an inherited rule, or from the default values of the rule. The import of a rule that the parent activates and the profile deactivates gives `active = false`.

## Example Usage

```terraform
data "sonarqube_quality_profile" "sonar_way_core" {
  organization = "my-organization"
  language     = "java"
  name         = "Sonar way core"
}

resource "sonarqube_quality_profile" "backend" {
  organization = "my-organization"
  language     = "java"
  name         = "Backend"
  parent_key   = data.sonarqube_quality_profile.sonar_way_core.key
}

# Activates a rule that the parent profile does not activate, and sets one of
# its parameters. The other parameters keep their default values.
resource "sonarqube_quality_profile_rule" "too_many_parameters" {
  quality_profile_key = sonarqube_quality_profile.backend.key
  rule                = "java:S107"
  params = {
    max = "5"
  }
}

# Changes the severity of rules that the profile gets from its parent. A
# destroy gives the rules the values of the parent again.
resource "sonarqube_quality_profile_rule" "security" {
  for_each = toset(["java:S2077", "java:S3329"])

  quality_profile_key = sonarqube_quality_profile.backend.key
  rule                = each.value
  impacts = {
    SECURITY = "BLOCKER"
  }
}

# Deactivates a rule that the profile gets from its parent. A destroy
# activates the rule again, with the values of the parent.
resource "sonarqube_quality_profile_rule" "hardcoded_ip" {
  quality_profile_key = sonarqube_quality_profile.backend.key
  rule                = "java:S1313"
  active              = false
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `quality_profile_key` (String) Key of a custom quality profile, for example the `key` of a `sonarqube_quality_profile`. Built-in profiles are read-only.
- `rule` (String) Key of the rule, as `<repository>:<key>`, for example `java:S107`. The rule must have the language of the profile.

### Optional

- `active` (Boolean) When `false`, the profile deactivates a rule that it gets from its parent profile. Then do not set `impacts` and `params`. The default is `true`. A change of `active` changes the rule in place.
- `impacts` (Map of String) Severity for each software quality of the rule. The keys are `SECURITY`, `RELIABILITY` and `MAINTAINABILITY`. The values are `INFO`, `LOW`, `MEDIUM`, `HIGH` and `BLOCKER`. Set only the software qualities that the rule has. The software qualities that you do not set keep the severity of the parent profile or of the rule.
- `params` (Map of String) Values of the parameters of the rule, by parameter key. The parameters that you do not set keep the value of the parent profile or the default value of the rule. The server checks the keys and the values.

### Read-Only

- `id` (String) `<quality_profile_key>/<rule>`.

## Import

Import is supported using the following syntax:

The [`terraform import` command](https://developer.hashicorp.com/terraform/cli/commands/import) can be used, for example:

```shell
# The ID is <quality_profile_key>/<rule>.
terraform import sonarqube_quality_profile_rule.too_many_parameters "my-profile-key/java:S107"
```
