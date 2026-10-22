# APIs consumed by the provider

Every SonarQube API endpoint that this provider calls, grouped by domain. All
calls go through this package (`internal/client`) — no other package makes an
HTTP call. When a new resource or data source needs a new domain, add a
section for it here. When an existing domain gains or loses an endpoint,
update its table in the same change.

Web API v2 answers at the `api.` host and takes a bearer token. Web API v1
answers at the instance host and takes the token as the basic-auth user name.

An endpoint is "Public" only when SonarSource's own docs link one as public.

## Permissions (`permissions.go`)

| Function | Endpoint | Surface | Public or internal |
| --- | --- | --- | --- |
| `CurrentUserLogin` | `GET {url}/api/users/current` | Web API v1 | Internal |
| `GetPermissions` | `GET {url}/api/permissions/groups?organization=&projectKey=&q=&p=&ps=` | Web API v1 | Internal |
| `GetPermissions` | `GET {url}/api/permissions/users?organization=&projectKey=&q=&p=&ps=` | Web API v1 | Internal |
| `AddPermission` | `POST {url}/api/permissions/add_group` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/add_group) |
| `RemovePermission` | `POST {url}/api/permissions/remove_group` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/remove_group) |
| `AddPermission` | `POST {url}/api/permissions/add_user` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/add_user) |
| `RemovePermission` | `POST {url}/api/permissions/remove_user` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/remove_user) |

The read endpoints list the groups or the organization members, also the ones
without permissions. `q` keeps the names and logins that hold the value (for
users, also the display names), so the client reads every page and matches
the exact name. The server refuses a `q` shorter than 3 characters, so the
client then omits `q`. It also refuses a `ps` above 100. A permission change
shows in the next read.

A scope names an organization, or a project with `projectKey`. The project
permissions use the same endpoints. A 404 means that the organization is
missing (`No organization with key '<key>'`), or with `projectKey`, that the
project is missing (`Project key '<key>' not found`). The client tells them
apart by the message. On a public project, the server answers 204 to a grant
of `user` or `codeviewer` but does not store it. The provider reads the
project visibility with `GetProject` before it sends these grants.

## Permission templates (`permission_templates.go`)

| Function | Endpoint | Surface | Public or internal |
| --- | --- | --- | --- |
| `CreatePermissionTemplate` | `POST {url}/api/permissions/create_template` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/create_template) |
| `UpdatePermissionTemplate` | `POST {url}/api/permissions/update_template` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/update_template) |
| `DeletePermissionTemplate` | `POST {url}/api/permissions/delete_template` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/delete_template) |
| `GetPermissionTemplate`, `FindPermissionTemplateID`, `GetDefaultPermissionTemplateID`, `SetDefaultPermissionTemplate`, `ListPermissionTemplateNames` | `GET {url}/api/permissions/search_templates?organization=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/search_templates) |
| `SetDefaultPermissionTemplate` | `POST {url}/api/permissions/set_default_template` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/set_default_template) |
| `GetPermissionTemplate` | `GET {url}/api/permissions/template_groups?organization=&templateId=&p=&ps=` | Web API v1 | Internal |
| `GetPermissionTemplate` | `GET {url}/api/permissions/template_users?organization=&templateId=&p=&ps=` | Web API v1 | Internal |
| `AddPermissionTemplateGrant` | `POST {url}/api/permissions/add_group_to_template` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/add_group_to_template) |
| `AddPermissionTemplateGrant` | `POST {url}/api/permissions/add_user_to_template` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/add_user_to_template) |
| `AddPermissionTemplateGrant` | `POST {url}/api/permissions/add_project_creator_to_template` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/add_project_creator_to_template) |
| `RemovePermissionTemplateGrant` | `POST {url}/api/permissions/remove_group_from_template` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/remove_group_from_template) |
| `RemovePermissionTemplateGrant` | `POST {url}/api/permissions/remove_user_from_template` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/remove_user_from_template) |
| `RemovePermissionTemplateGrant` | `POST {url}/api/permissions/remove_project_creator_from_template` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/permissions/remove_project_creator_from_template) |

A template has a string id. `search_templates` has no pages and gives all
the templates of the organization. It shows the grants of groups and users
only as counts, and the project creator grants as `withProjectCreator` on
each permission. `template_groups` and `template_users` list all the groups,
the Anyone group (without an id) and the organization members, also the ones
without permissions, so the client keeps only the ones that have a
permission. They refuse a `ps` above 100.

`update_template` keeps a parameter that is not sent, so the client sends
each one, and an empty value removes the description or the pattern. A
template name is unique in an organization without regard to case. The
grant changes are idempotent. A user must be a member of the organization.
On an organization without the Team or the Enterprise plan, create and the
grant changes answer 403 `Organization '<key>' is not allowed to use custom
permission templates`. The server refuses to delete the default template
(400).

`search_templates` gives the default in `defaultTemplates`, with the
qualifier `TRK` for projects. SonarQube Cloud has no applications or
portfolios there. `set_default_template` takes `TRK` when no qualifier is
sent. No web service removes a default; only a different template can take
its place. `set_default_template` finds the template by its id in any
organization and changes the default of the organization of the template,
so `SetDefaultPermissionTemplate` first checks that the template is in the
organization.

## Groups (`groups.go`)

| Function | Endpoint | Surface | Public or internal |
| --- | --- | --- | --- |
| `CreateGroup` | `POST {api_url}/users/groups` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/createGroup) |
| `GetGroup` | `GET {api_url}/users/groups?organizationIds=&ids=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/searchGroups) |
| `FindGroupByName`, `SearchGroups` | `GET {api_url}/users/groups?organizationIds=&q=&pageIndex=&pageSize=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/searchGroups) |
| `UpdateGroup` | `PATCH {api_url}/users/groups/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/patchGroup) |
| `DeleteGroup` | `DELETE {api_url}/users/groups/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/deleteGroup) |

Each call except update and delete reads the organization first
(`GetOrganization`), because the group API takes the UUID of the organization
and not its key. The group API answers 403 for an organization that does not
exist, so that read is the one that finds a missing organization.

There is no read of one group by its id. The `ids` filter of the list gives
the same result. The `q` filter is a search on the name that ignores the
case, so the client finds the exact name. The page size is 500 at most. An update takes a
JSON merge patch, and an empty description removes the description. A delete
of a group that is already gone answers 204.

Group ids are UUIDs. The Server Web API v2 also uses UUIDs, at
`api/v2/authorizations/groups`. The provider supports only Cloud in the alpha.

The group list is eventually consistent. A new group shows in less than a
second, but an update or a delete can take about five seconds to show. The
`ids` filter and the `q` filter lag independently. The create and update
answers hold the new group, so the resource keeps the plan in the state and
does not read the group back after a write. When Read does not find the
group, or finds a name or a description that differs from the state, it reads
again for about six seconds before it takes the answer.

## Group members (`group_members.go`)

| Function | Endpoint | Surface | Public or internal |
| --- | --- | --- | --- |
| `AddGroupMember` | `POST {url}/api/user_groups/add_user` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/user_groups/add_user) |
| `RemoveGroupMember` | `POST {url}/api/user_groups/remove_user` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/user_groups/remove_user) |
| `IsGroupMember` | `GET {url}/api/user_groups/users?organization=&name=&selected=selected&q=&p=&ps=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/user_groups/users) |

The calls name the group by `organization` and `name`, which are case
sensitive, like `login`. A 404 names the missing part: `No organization with
key '<key>'`, `No group with name '<name>' in organization '<key>'`, or
`Could not find a user with login '<login>'`. The client tells them apart by
the message.

An add of a user that is already in the group, and a remove of a user that is
not in it, answer 204. An add of a user that is not a member of the
organization answers 400. Both writes refuse the default group `Members` with
400. A remove answers 400 `The last administrator user cannot be removed`
from any group when no user has a direct `admin` grant and no other group
that has `admin` has another member.

The read lists the members of the group. `q` keeps the users whose login or
name holds the value, without regard to case, so the client reads every page
and matches the exact login. `ps` is 500 at most: the server answers 500 to a
larger value. The list can show the old members for about two seconds after
a change.

The Web API v2 `group-memberships` endpoints are internal and require the
root role, so the client does not use them.

## Organizations (`organizations.go`)

| Function | Endpoint | Surface | Public or internal |
| --- | --- | --- | --- |
| `GetOrganization` | `GET {api_url}/organizations/organizations?organizationKey=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-organizations-external-1-0-2#/organizations/list-organizations) |
| `CreateOrganization` | `POST {url}/api/organizations/create` | Web API v1 | Internal |
| `UpdateOrganization` | `POST {url}/api/organizations/update` | Web API v1 | Internal |
| `UpdateOrganizationKey` | `POST {url}/api/organizations/update_key` | Web API v1 | Internal |
| `DeleteOrganization` | `POST {url}/api/organizations/delete` | Web API v1 | Internal |
| `GetCurrentPlan` | `GET {api_url}/billing/current-plan?organizationId=` | Web API v2 | Internal |
| `ListMemberOrganizationKeys` | `GET {url}/api/organizations/search?member=true&p=&ps=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/organizations/search) |

Web API v2 has no create, update, rename or delete for an organization today,
so every write goes through the internal v1 surface.

`GetOrganization` also reports the internal identifier of the organization,
an opaque string of 20 characters. The bindings API below names an organization
by that identifier and by nothing else, so a read of the organization is how
an organization key reaches it.

`DeleteOrganization` tries again after a 5xx, a 409, a 429, a timeout or a
connection that the server closed, with waits that add up to 30 seconds. The
server can refuse a delete for a short time after a change. A 404 is success.

`GetCurrentPlan` takes the UUID of the organization and answers
`{"planKey":"Enterprise"}`. The key can be in any case, so the client gives it
in lower case. The webapp reads the plan in the same way.

## DevOps platform bindings (`organization_bindings.go`)

| Function | Endpoint | Surface | Public or internal |
| --- | --- | --- | --- |
| `CreateOrganizationBinding` | `POST {api_url}/dop-translation/organization-bindings` | Web API v2 | Internal |
| `GetOrganizationBinding` | `GET {api_url}/dop-translation/organization-bindings/{id}` | Web API v2 | Internal |
| `FindOrganizationBinding` | `GET {api_url}/dop-translation/organization-bindings?organizationId=` | Web API v2 | Internal |
| `UpdateOrganizationBinding` | `PATCH {api_url}/dop-translation/organization-bindings/{id}` | Web API v2 | Internal |

There is no delete. A binding goes away only when its organization is
deleted, which removes the records that these endpoints write.

A search for an organization that is not bound answers 404 with an empty
body, not 200 with an empty collection.

Every endpoint of these two domains was read against a development instance on
2026-09-24. A bind that succeeds was not tested: it needs a GitHub
application installation that is bound to no organization.

## DevOps platform applications (`dop_applications.go`)

| Function | Endpoint | Surface | Public or internal |
| --- | --- | --- | --- |
| `ListDopApplications` | `GET {api_url}/dop-translation/dop-applications?devOpsPlatform=` | Web API v2 | Internal |

A binding to github.com accepts an installation of one of these applications
only, because the server looks an installation up in its own records instead
of asking GitHub.

## Organization quality gate settings (`quality_gate_settings.go`)

| Function | Endpoint | Surface | Public or internal |
| --- | --- | --- | --- |
| `GetOrganizationQualityGateSettings` | `GET {api_url}/quality-gates/settings?resourceId=&resourceType=ORGANIZATION` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0#/Quality%20Gate%20Settings) |
| `UpdateOrganizationQualityGateSettings` | `PATCH {api_url}/quality-gates/settings/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0#/Quality%20Gate%20Settings) |

The read names the organization by its UUID (`uuidV4`). The patch writes
`ignoreSmallChanges`, and the server also writes the value to all projects of
the organization. Thus the resource sends a patch only when the value changes.
There is no delete operation. Terraform destroy only
removes the setting from state.

## Projects (`projects.go`)

| Function | Endpoint | Surface | Public or internal |
| --- | --- | --- | --- |
| `GetProject` | `GET {api_url}/projects/projects?keys=&organizationIds=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-projectsexternal-0-0-1#/Projects/getProjects) |
| `CreateProject` | `POST {url}/api/projects/create` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/projects/create) |
| `DeleteProject` | `POST {url}/api/projects/delete` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/projects/delete) |
| `SearchProjectKeys` | `GET {url}/api/projects/search?organization=&q=&p=&ps=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/projects/search) |

`GetProject` first reads the organization to get its UUID, because the list call
filters by UUID. A project that is missing answers 200 with an empty list.

Project creation uses the plain project API and does not bind a repository.

The project bindings API below names a project by its legacy identifier and
by nothing else. `GetProject` reports it in `legacyId`. The `id` field holds a
UUID, which the bindings API refuses.

## Project bindings (`project_bindings.go`)

| Function | Endpoint | Surface | Public or internal |
| --- | --- | --- | --- |
| `CreateProjectBinding` | `POST {api_url}/dop-translation/project-bindings` | Web API v2 | Internal |
| `FindProjectBinding` | `GET {api_url}/dop-translation/project-bindings?projectId=` | Web API v2 | Internal |
| `UpdateProjectBinding` | `PATCH {api_url}/dop-translation/project-bindings/{id}` | Web API v2 | Internal |

There is no delete. A binding goes away only when its project is deleted.

For GitHub, a request takes the slug `owner/name` in `repositoryId`, and a read
gives the numeric identifier of the repository in the same field. The change
call accepts `application/merge-patch+json` only.

A search for a project that is not bound answers 200 with an empty collection,
unlike the search of organization bindings. A search for a project that does
not exist answers 404.

A bind, a read, a change of case and an import were read against a
development instance on 2026-09-30. On 2026-10-02, `legacyId` was checked to
be the identifier that the bindings API takes. A change to a different
repository was not tested.

## Quality gates (`quality_gates.go`)

| Function | Endpoint | Surface | Public or internal |
| --- | --- | --- | --- |
| `ListQualityGates` | `GET {api_url}/quality-gates/quality-gates?organizationId=&pageIndex=&pageSize=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) |
| `GetQualityGate` | `GET {api_url}/quality-gates/quality-gates/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) |
| `CreateQualityGate` | `POST {api_url}/quality-gates/quality-gates` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) |
| `UpdateQualityGate` | `PATCH {api_url}/quality-gates/quality-gates/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) |
| `DeleteQualityGate` | `DELETE {api_url}/quality-gates/quality-gates/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) |
| `SetDefaultQualityGate` | `PATCH {api_url}/quality-gates/quality-gates/defaults/{organizationId}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) |
| `ListQualityGateConditions` | `GET {api_url}/quality-gates/conditions?qualityGateId=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) |
| `CreateQualityGateCondition` | `POST {api_url}/quality-gates/conditions` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) |
| `UpdateQualityGateCondition` | `PATCH {api_url}/quality-gates/conditions/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) |
| `DeleteQualityGateCondition` | `DELETE {api_url}/quality-gates/conditions/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) |
| `FindQualityGateProjectAssociation` | `GET {api_url}/quality-gates/project-associations?projectIds=&organizationId=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) |
| `CreateQualityGateProjectAssociation` | `POST {api_url}/quality-gates/project-associations` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) |
| `DeleteQualityGateProjectAssociation` | `DELETE {api_url}/quality-gates/project-associations/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0) |
| `ListMetrics` | `GET {url}/api/metrics/search?p=&ps=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/metrics/search) |

The metrics web service maps a Terraform metric key to the condition API's
`legacyMetricId`. It also maps identifiers back to keys during refresh.

`SetDefaultQualityGate` names the organization by its UUID in the path, and
sends `{"qualityGateId": "<gate UUID>"}` as plain JSON. The gateway refuses
`application/merge-patch+json` on this path with 403. An unknown gate gives
404. No API removes a default: only a different gate can take its place.

The organization must be on the Team or the Enterprise plan. Otherwise the
call answers 403 "Your current plan does not allow you to perform this
operation".

The SonarQube Cloud web application still uses the older web service
`api/qualitygates/set_as_default`, which takes the numeric `legacyId` of the
gate. That web service is marked deprecated, so the provider does not use it.

## Quality profiles (`quality_profiles.go`)

| Function | Endpoint | Surface | Public or internal |
| --- | --- | --- | --- |
| `CreateQualityProfile` | `POST {url}/api/qualityprofiles/create` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/create) |
| `ChangeQualityProfileParent` | `POST {url}/api/qualityprofiles/change_parent` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/change_parent) |
| `RenameQualityProfile` | `POST {url}/api/qualityprofiles/rename` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/rename) |
| `DeleteQualityProfile` | `POST {url}/api/qualityprofiles/delete` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/delete) |
| `ListQualityProfiles`, `GetQualityProfile`, `FindQualityProfileByName` | `GET {url}/api/qualityprofiles/search?organization=&language=&qualityProfile=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/search) |

The write actions take the profile key as `key`. `change_parent` and
`delete` also take `qualityProfile`, but that is the profile name, not its
key. `change_parent` without `parentKey` removes the parent. `delete` also
deletes all the descendants of the profile, and answers 404 for a key that
does not exist. `search` has no pages, shows a change at once, and gives
`parentKey`, `isBuiltIn` and `isDefault`. Without `language`, it gives the
profiles of all the languages. Its `qualityProfile` filter matches the exact
name, with the same case. A missing organization is a 404 with the message
`No organization with key '<key>'`.

## Organization tokens (`organization_tokens.go`)

| Function | Endpoint | Surface | Public or internal |
| --- | --- | --- | --- |
| `CreateOrganizationToken` | `POST {api_url}/authentication/token-definitions` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) |
| `ListOrganizationTokens`, `FindOrganizationToken` | `GET {api_url}/authentication/token-definitions?organizationId=&pageIndex=&pageSize=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) |
| `DeleteOrganizationToken` | `DELETE {api_url}/authentication/token-definitions/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) |

Read uses the public list endpoint, with the organization UUID and pagination.
The endpoint that reads one token definition by ID is internal and is not used.
The secret is available only in the create response.

Delete returns 204 for an unknown ID, and can return 204 without deleting a
token when the caller lacks access. The client therefore checks the
organization list after deletion. A list error never means that a token is
absent.

Expired tokens can remain in the list and reserve their names. The resource
keeps them in state and plans replacement. These API behavior assumptions have
unit test coverage; the acceptance test checks the live lifecycle.
