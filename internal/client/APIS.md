# APIs consumed by the provider

Every SonarQube API endpoint that this provider calls, grouped by domain. All
calls go through this package (`internal/client`) — no other package makes an
HTTP call. When a new resource or data source needs a new domain, add a
section for it here. When an existing domain gains or loses an endpoint,
update its table in the same change.

Web API v2 answers at the `api.` host and takes a bearer token. Web API v1
answers at the instance host and takes the token as the basic-auth user name.

An endpoint is "Public" only when SonarSource's own docs link one as public.

The SOT column tells if the endpoint accepts a scoped organization token (a
token that starts with `sqco_`) of the organization, with the organization
permissions `admin`, `scan` and `provisioning`:

* `Yes`: a call with such a token worked. `Yes, admin` means that a token
  without the `admin` permission is refused. `Yes, not provisioning only`
  means that a token with only `provisioning` gets an empty answer.
* `Public projects only`: the endpoint accepts the token, but its answer does
  not contain private projects. `Own organization only`: the answer contains
  only the organization of the token. A resource that needs more than this
  does not work with the token.
* `No (status)`: the endpoint refused the token with this status. `No` alone
  means that the endpoint gives the token nothing useful, for example an
  empty list. `No (no user)`: the endpoint answers 200, but without a user,
  because the token has no user.

## Permissions (`permissions.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CurrentUserLogin` | `GET {url}/api/users/current` | Web API v1 | Internal | No (no user) |
| `GetPermissions` | `GET {url}/api/permissions/groups?organization=&projectKey=&q=&p=&ps=` | Web API v1 | Internal | No (401) |
| `GetPermissions` | `GET {url}/api/permissions/users?organization=&projectKey=&q=&p=&ps=` | Web API v1 | Internal | No (401) |
| `AddPermission` | `POST {url}/api/permissions/add_group` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/add_group) | No (401) |
| `RemovePermission` | `POST {url}/api/permissions/remove_group` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/remove_group) | No (401) |
| `AddPermission` | `POST {url}/api/permissions/add_user` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/add_user) | No (401) |
| `RemovePermission` | `POST {url}/api/permissions/remove_user` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/remove_user) | No (401) |

* The client reads every page and matches the exact name or login.
* The same endpoints serve the project permissions, with `projectKey`.

## Groups (`groups.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CreateGroup` | `POST {api_url}/users/groups` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/createGroup) | No (403) |
| `GetGroup` | `GET {api_url}/users/groups?organizationIds=&ids=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/searchGroups) | No (403) |
| `FindGroupByName`, `SearchGroups` | `GET {api_url}/users/groups?organizationIds=&q=&pageIndex=&pageSize=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/searchGroups) | No (403) |
| `UpdateGroup` | `PATCH {api_url}/users/groups/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/patchGroup) | No (403) |
| `DeleteGroup` | `DELETE {api_url}/users/groups/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/deleteGroup) | No (403) |

* The group API takes the UUID of the organization.
* There is no read of one group by ID, so the client filters the list by
  `ids`.
* The list can lag behind a write, so Read tries again for a few seconds.

## Group members (`group_members.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `AddGroupMember` | `POST {url}/api/user_groups/add_user` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/user_groups/add_user) | No (401) |
| `RemoveGroupMember` | `POST {url}/api/user_groups/remove_user` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/user_groups/remove_user) | Yes, `admin` |
| `IsGroupMember` | `GET {url}/api/user_groups/users?organization=&name=&selected=selected&q=&p=&ps=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/user_groups/users) | No (401) |

* The client reads every page and matches the exact login. Group names and
  logins are case-sensitive.
* The Web API v2 `group-memberships` endpoints are internal and need the root
  role, so the client uses Web API v1.

## Organizations (`organizations.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `GetOrganization` | `GET {api_url}/organizations/organizations?organizationKey=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-organizations-external-1-0-2#/organizations/list-organizations) | Yes |
| `CreateOrganization` | `POST {url}/api/organizations/create` | Web API v1 | Internal | No (401) |
| `UpdateOrganization` | `POST {url}/api/organizations/update` | Web API v1 | Internal | No (401) |
| `UpdateOrganizationKey` | `POST {url}/api/organizations/update_key` | Web API v1 | Internal | No (401) |
| `DeleteOrganization` | `POST {url}/api/organizations/delete` | Web API v1 | Internal | No (401) |
| `GetCurrentPlan` | `GET {api_url}/billing/current-plan?organizationId=` | Web API v2 | Internal | Yes |
| `ListMemberOrganizationKeys` | `GET {url}/api/organizations/search?member=true&p=&ps=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/organizations/search) | Own organization only |

* Web API v2 has no write for an organization, so the writes use the internal
  Web API v1.
* `GetOrganization` gives the internal identifier that the bindings API takes.

## DevOps platform bindings (`organization_bindings.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CreateOrganizationBinding` | `POST {api_url}/dop-translation/organization-bindings` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `GetOrganizationBinding` | `GET {api_url}/dop-translation/organization-bindings/{id}` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `FindOrganizationBinding` | `GET {api_url}/dop-translation/organization-bindings?organizationId=` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `UpdateOrganizationBinding` | `PATCH {api_url}/dop-translation/organization-bindings/{id}` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `CreateGitLabSelfManagedBinding` | `POST {api_url}/dop-translation/organization-bindings` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `UpdateGitLabSelfManagedBinding` | `PATCH {api_url}/dop-translation/organization-bindings/{id}` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |

* There is no delete. A binding goes away with its organization.
* A binding to GitLab Self-Managed uses the same endpoints, with `dopFlavor`.

## GitLab configurations (`gitlab_configurations.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CreateGitLabConfiguration` | `POST {api_url}/gitlab-configurations` | Web API v2 | Public | No (403) |
| `FindGitLabConfiguration` | `GET {api_url}/gitlab-configurations?organizationId=` | Web API v2 | Public | No (403) |
| `UpdateGitLabConfiguration` | `PATCH {api_url}/gitlab-configurations/{id}` | Web API v2 | Public | No (403) |

* The API takes the UUID of the organization.
* An organization has at most one configuration.

## DevOps platform applications (`dop_applications.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `ListDopApplications` | `GET {api_url}/dop-translation/dop-applications?devOpsPlatform=` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |

* A binding to github.com accepts an installation of one of these applications
  only.

## Organization quality gate settings (`quality_gate_settings.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `GetOrganizationQualityGateSettings` | `GET {api_url}/quality-gates/settings?resourceId=&resourceType=ORGANIZATION` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0#/Quality%20Gate%20Settings) | Yes, `admin` |
| `UpdateOrganizationQualityGateSettings` | `PATCH {api_url}/quality-gates/settings/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0#/Quality%20Gate%20Settings) | Yes, `admin` |

* There is no delete. A destroy removes the setting from the state only.
* A change applies to all projects of the organization, so the resource sends
  a patch only when the value changes.

## Projects (`projects.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `GetProject` | `GET {api_url}/projects/projects?keys=&organizationIds=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-projectsexternal-0-0-1#/Projects/getProjects) | Public projects only |
| `FindProjectsByKey`, `FindProjectsByID` | `GET {api_url}/projects/projects?keys=&organizationIds=&pageIndex=&pageSize=`, or with `ids=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-projectsexternal-0-0-1#/Projects/getProjects) | Public projects only |
| `CreateProject` | `POST {url}/api/projects/create` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/projects/create) | Yes |
| `DeleteProject` | `POST {url}/api/projects/delete` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/projects/delete) | No (401) |
| `SearchProjectKeys` | `GET {url}/api/projects/search?organization=&q=&p=&ps=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/projects/search) | Yes, not `provisioning` only |

* The project bindings API takes the legacy identifier (`legacyId`), not the
  UUID in `id`.
* `keys` and `ids` take one comma-separated value. The client sends at most 50
  values in one request.

## Project bindings (`project_bindings.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CreateProjectBinding` | `POST {api_url}/dop-translation/project-bindings` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `FindProjectBinding` | `GET {api_url}/dop-translation/project-bindings?projectId=` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `UpdateProjectBinding` | `PATCH {api_url}/dop-translation/project-bindings/{id}` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |

* There is no delete. A binding goes away with its project.
* For GitHub, the bind takes the slug `owner/name`, and a read gives the
  numeric identifier of the repository.

## Quality gates (`quality_gates.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `ListQualityGates` | `GET {api_url}/quality-gates/quality-gates?organizationId=&pageIndex=&pageSize=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) | Yes |
| `GetQualityGate` | `GET {api_url}/quality-gates/quality-gates/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) | Yes |
| `CreateQualityGate` | `POST {api_url}/quality-gates/quality-gates` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) | No (403) |
| `UpdateQualityGate` | `PATCH {api_url}/quality-gates/quality-gates/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) | No (403) |
| `DeleteQualityGate` | `DELETE {api_url}/quality-gates/quality-gates/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) | No (403) |
| `SetDefaultQualityGate` | `PATCH {api_url}/quality-gates/quality-gates/defaults/{organizationId}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) | No (403) |
| `ListQualityGateConditions` | `GET {api_url}/quality-gates/conditions?qualityGateId=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) | Yes |
| `CreateQualityGateCondition` | `POST {api_url}/quality-gates/conditions` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) | No (403) |
| `UpdateQualityGateCondition` | `PATCH {api_url}/quality-gates/conditions/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) | No (403) |
| `DeleteQualityGateCondition` | `DELETE {api_url}/quality-gates/conditions/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) | No (403) |
| `FindQualityGateProjectAssociation` | `GET {api_url}/quality-gates/project-associations?projectIds=&organizationId=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) | Yes |
| `CreateQualityGateProjectAssociation` | `POST {api_url}/quality-gates/project-associations` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) | No (403) |
| `DeleteQualityGateProjectAssociation` | `DELETE {api_url}/quality-gates/project-associations/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) | No (403) |
| `ListMetrics` | `GET {url}/api/metrics/search?p=&ps=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/metrics/search) | Yes |

* A condition names a metric by its `legacyMetricId`, which the metrics web
  service gives.
* No API removes a default gate.
* The provider does not use the deprecated `api/qualitygates/set_as_default`.

## Quality profiles (`quality_profiles.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CreateQualityProfile` | `POST {url}/api/qualityprofiles/create` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/create) | No (401) |
| `ChangeQualityProfileParent` | `POST {url}/api/qualityprofiles/change_parent` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/change_parent) | No (500) |
| `RenameQualityProfile` | `POST {url}/api/qualityprofiles/rename` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/rename) | No (401) |
| `DeleteQualityProfile` | `POST {url}/api/qualityprofiles/delete` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/delete) | No (401) |
| `ListQualityProfiles`, `GetQualityProfile`, `FindQualityProfileByName` | `GET {url}/api/qualityprofiles/search?organization=&language=&qualityProfile=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/search) | Yes |
| `GetQualityProfileParentKey` | `GET {url}/api/qualityprofiles/inheritance?key=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/inheritance) | Yes |
| `GetDefaultQualityProfile` | `GET {url}/api/qualityprofiles/search?organization=&language=&defaults=true` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/search) | Yes |
| `SetDefaultQualityProfile` | `POST {url}/api/qualityprofiles/set_default` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/set_default) | No (401) |
| `AddQualityProfileProject` | `POST {url}/api/qualityprofiles/add_project` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/add_project) | No (401) |
| `RemoveQualityProfileProject` | `POST {url}/api/qualityprofiles/remove_project` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/remove_project) | No (401) |
| `GetProjectQualityProfile` | `GET {url}/api/qualityprofiles/search?organization=&project=&language=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/search) | Yes |

* No Web API v2 endpoint sets a default profile or assigns a profile to a
  project.
* `change_parent` and `delete` take the profile name in `qualityProfile`, not
  its key.
* `delete` also deletes the descendants of the profile.
* `add_project` replaces the profile of the same language.

## Quality profile editors (`quality_profile_editors.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `AddQualityProfileEditor` | `POST {url}/api/qualityprofiles/add_group`, `POST {url}/api/qualityprofiles/add_user` | Web API v1 | Internal | No (500) |
| `RemoveQualityProfileEditor` | `POST {url}/api/qualityprofiles/remove_group`, `POST {url}/api/qualityprofiles/remove_user` | Web API v1 | Internal | No (500) |
| `HasQualityProfileEditor` | `GET {url}/api/qualityprofiles/search_groups`, `GET {url}/api/qualityprofiles/search_users` | Web API v1 | Internal | No (500) |

* No public endpoint manages the editors.
* These internal web services name a profile by its name, language and
  organization, so the client reads the profile from its key first.

## Quality profile rules (`quality_profile_rules.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `ActivateQualityProfileRule`, `ResetQualityProfileRule` | `POST {url}/api/qualityprofiles/activate_rule` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/activate_rule) | No (401) |
| `DeactivateQualityProfileRule` | `POST {url}/api/qualityprofiles/deactivate_rule` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/deactivate_rule) | No (500) |
| `GetActiveRule` | `GET {url}/api/rules/search?qprofile=&activation=true&rule_key=&f=actives,params,impacts` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/rules/search) | Yes |

* No Web API v2 endpoint manages the rules of a profile.
* A deactivation of an inherited rule also deactivates it in the descendants
  of the profile.
* On SonarQube Server, `activate_rule` refuses an impact that the rule does
  not have, and `impacts` can replace the whole set: check this before the
  provider supports it.

## Organization tokens (`organization_tokens.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CreateOrganizationToken` | `POST {api_url}/authentication/token-definitions` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) | Yes, `admin` |
| `ListOrganizationTokens`, `FindOrganizationToken` | `GET {api_url}/authentication/token-definitions?organizationId=&pageIndex=&pageSize=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) | Yes, `admin` |
| `DeleteOrganizationToken` | `DELETE {api_url}/authentication/token-definitions/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) | Yes, `admin` |
| `CreateTokenResourceLink` | `POST {api_url}/authentication/token-resource-links` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) | Yes, `admin` |
| `ListTokenResourceLinks` | `GET {api_url}/authentication/token-resource-links?organizationTokenId=&pageIndex=&pageSize=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) | Yes, `admin` |
| `DeleteTokenResourceLink` | `DELETE {api_url}/authentication/token-resource-links/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) | Yes, `admin` |

* The token secret is only in the create answer.
* The read of one token by ID is internal, so Read uses the public list.

## Enterprises (`enterprises.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CreateEnterprise` | `POST {api_url}/enterprises/enterprises` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `GetEnterprise` | `GET {api_url}/enterprises/enterprises/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `GetEnterpriseByKey` | `GET {api_url}/enterprises/enterprises?enterpriseKey=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `ListEnterprises` | `GET {api_url}/enterprises/enterprises` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `UpdateEnterprise` | `PATCH {api_url}/enterprises/enterprises/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `DeleteEnterprise` | `DELETE {api_url}/enterprises/enterprises/{id}`, then `GET {api_url}/enterprises/enterprises/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |

* The API needs a user token.
* A license works only once.
* `PATCH` can change `key`, but the docs do not list it, so the client does
  not send it.

## Enterprise organizations (`enterprise_organizations.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `ListEnterpriseOrganizationsOfEnterprise` | `GET {api_url}/enterprises/enterprise-organizations?enterpriseId=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `ListEnterpriseOrganizationsOfOrganization` | `GET {api_url}/enterprises/enterprise-organizations?organizationId=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `AttachEnterpriseOrganization` | `POST {api_url}/enterprises/enterprise-organizations` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `DetachEnterpriseOrganization` | `DELETE {api_url}/enterprises/enterprise-organizations/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |

* A detach of the last organization also deletes the enterprise, so the
  resource refuses it.
