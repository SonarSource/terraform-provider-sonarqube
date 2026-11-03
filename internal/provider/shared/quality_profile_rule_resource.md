This resource activates a rule in a custom quality profile, and sets the severity of its impacts and the values of its parameters. It can also change a rule that the profile gets from its parent profile.

Use one resource for each rule. To manage many rules, use `for_each`. The provider does not read the rules that no resource manages, so a change to such a rule in the UI shows no drift.

The built-in profiles are read-only. Create a `sonarqube_quality_profile` that extends a built-in profile, and change the rules of that profile.

## Values that the configuration does not set

The provider manages only the software qualities in `impacts` and the parameters in `params` that the configuration sets. The other ones keep the value of the parent profile, or the default value of the rule, and a change to them in the UI shows no drift. When you remove a key from `impacts` or `params`, the apply resets the rule first. The reset applies to all the values of the rule, also to the ones that the configuration does not set, such as a change in the UI. They then get the value of the parent profile, or the default value of the rule, again.

The server ignores a software quality that the rule does not have. The apply then fails, and the error gives the software qualities of the rule.

## Create and destroy

When the profile already activates the rule, the create takes it over and does not fail. When the profile activates the rule itself, a destroy deactivates the rule, also when the rule was active before the create. When the profile gets the rule from its parent, a destroy does not deactivate the rule: the rule gets the values of the parent profile again.

## MISRA rules

The MISRA rules of C and C++ need a MISRA entitlement of the organization. Without it, the apply fails.

## Import

Import a rule with `<quality_profile_key>/<rule>`, for example `my-profile-key/java:S107`. The import keeps the impacts and the parameters that differ from the values of the parent profile, for an inherited rule, or from the default values of the rule.
