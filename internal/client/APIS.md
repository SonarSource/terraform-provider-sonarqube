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

The Web API v1 flag `supportsScopedOrganizationTokens` of
`/api/webservices/list` is not reliable: `api/qualityprofiles/search` accepts
the token without the flag. Web API v2 does not document it. Thus, the column
comes from calls on a development instance (November 2026). Check it again
when SonarQube Cloud adds support for these tokens.

Web API v2 `projects/projects` does not show private projects to a scoped
organization token, also when the request names the project key. Web API v1
`api/projects/search` shows them, but not to a token with only
`provisioning`.

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

## Groups (`groups.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CreateGroup` | `POST {api_url}/users/groups` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/createGroup) | No (403) |
| `GetGroup` | `GET {api_url}/users/groups?organizationIds=&ids=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/searchGroups) | No (403) |
| `FindGroupByName`, `SearchGroups` | `GET {api_url}/users/groups?organizationIds=&q=&pageIndex=&pageSize=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/searchGroups) | No (403) |
| `UpdateGroup` | `PATCH {api_url}/users/groups/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/patchGroup) | No (403) |
| `DeleteGroup` | `DELETE {api_url}/users/groups/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalusers-0-0#/groups/deleteGroup) | No (403) |

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

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `AddGroupMember` | `POST {url}/api/user_groups/add_user` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/user_groups/add_user) | No (401) |
| `RemoveGroupMember` | `POST {url}/api/user_groups/remove_user` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/user_groups/remove_user) | Yes, `admin` |
| `IsGroupMember` | `GET {url}/api/user_groups/users?organization=&name=&selected=selected&q=&p=&ps=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/user_groups/users) | No (401) |

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

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `GetOrganization` | `GET {api_url}/organizations/organizations?organizationKey=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-organizations-external-1-0-2#/organizations/list-organizations) | Yes |
| `CreateOrganization` | `POST {url}/api/organizations/create` | Web API v1 | Internal | No (401) |
| `UpdateOrganization` | `POST {url}/api/organizations/update` | Web API v1 | Internal | No (401) |
| `UpdateOrganizationKey` | `POST {url}/api/organizations/update_key` | Web API v1 | Internal | No (401) |
| `DeleteOrganization` | `POST {url}/api/organizations/delete` | Web API v1 | Internal | No (401) |
| `GetCurrentPlan` | `GET {api_url}/billing/current-plan?organizationId=` | Web API v2 | Internal | Yes |
| `ListMemberOrganizationKeys` | `GET {url}/api/organizations/search?member=true&p=&ps=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/organizations/search) | Own organization only |

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

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CreateOrganizationBinding` | `POST {api_url}/dop-translation/organization-bindings` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `GetOrganizationBinding` | `GET {api_url}/dop-translation/organization-bindings/{id}` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `FindOrganizationBinding` | `GET {api_url}/dop-translation/organization-bindings?organizationId=` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `UpdateOrganizationBinding` | `PATCH {api_url}/dop-translation/organization-bindings/{id}` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `CreateGitLabSelfManagedBinding` | `POST {api_url}/dop-translation/organization-bindings` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `UpdateGitLabSelfManagedBinding` | `PATCH {api_url}/dop-translation/organization-bindings/{id}` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |

There is no delete. A binding goes away only when its organization is
deleted, which removes the records that these endpoints write.

A binding to GitLab Self-Managed sends `devOpsPlatform` `gitlab`, `dopFlavor`
`GITLAB_SELF_MANAGED`, `accessToken`, and the path of the group in
`devOpsPlatformOrganizationId`. A read reports the numeric identifier of the
group in that field, and the web address of the instance in `tenantId`. No
read returns the token. The change call for GitLab takes a JSON merge patch.
The organization must have a GitLab configuration first. These calls were
made against a development instance on 2026-11-10, with a GitLab CE instance
behind an ngrok tunnel.

A search for an organization that is not bound answers 404 with an empty
body, not 200 with an empty collection.

Every endpoint of these two domains was read against a development instance on
2026-09-24. A bind that succeeds was not tested: it needs a GitHub
application installation that is bound to no organization.

## GitLab configurations (`gitlab_configurations.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CreateGitLabConfiguration` | `POST {api_url}/gitlab-configurations` | Web API v2 | Public | No (403) |
| `FindGitLabConfiguration` | `GET {api_url}/gitlab-configurations?organizationId=` | Web API v2 | Public | No (403) |
| `UpdateGitLabConfiguration` | `PATCH {api_url}/gitlab-configurations/{id}` | Web API v2 | Public | No (403) |

The OpenAPI definition of the service marks these operations public. The
public API docs site did not answer when this table was written (November
2026), so the table links no docs page. A scoped organization token with
`admin`, `scan` and `provisioning` gets 403 on each call ("User is not a
member of organization" for the search, "User does not have organization
admin role" for the create and the change), while a user token gets past the
check. These calls were made against a development instance on 2026-11-11.

A configuration names the GitLab Self-Managed instance of an organization,
and an organization has at most one. It takes the UUID of the organization,
not the internal identifier that the bindings take. The server checks the
connection to GitLab with `personalAccessToken` and `group`, and stores
neither. A create for an organization without the Enterprise plan answers
403, and a second create answers 409. A refusal of a field answers 400 with
`relatedField`. The change call takes a JSON merge patch only, and plain JSON
gets 415.

## DevOps platform applications (`dop_applications.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `ListDopApplications` | `GET {api_url}/dop-translation/dop-applications?devOpsPlatform=` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |

A binding to github.com accepts an installation of one of these applications
only, because the server looks an installation up in its own records instead
of asking GitHub.

## Organization quality gate settings (`quality_gate_settings.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `GetOrganizationQualityGateSettings` | `GET {api_url}/quality-gates/settings?resourceId=&resourceType=ORGANIZATION` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0#/Quality%20Gate%20Settings) | Yes, `admin` |
| `UpdateOrganizationQualityGateSettings` | `PATCH {api_url}/quality-gates/settings/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-qualitygates-1-0-0#/Quality%20Gate%20Settings) | Yes, `admin` |

The read names the organization by its UUID (`uuidV4`). The patch writes
`ignoreSmallChanges`, and the server also writes the value to all projects of
the organization. Thus the resource sends a patch only when the value changes.
There is no delete operation. Terraform destroy only
removes the setting from state.

## Projects (`projects.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `GetProject` | `GET {api_url}/projects/projects?keys=&organizationIds=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-projectsexternal-0-0-1#/Projects/getProjects) | Public projects only |
| `FindProjectsByKey`, `FindProjectsByID` | `GET {api_url}/projects/projects?keys=&organizationIds=&pageIndex=&pageSize=`, or with `ids=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-projectsexternal-0-0-1#/Projects/getProjects) | Public projects only |
| `CreateProject` | `POST {url}/api/projects/create` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/projects/create) | Yes |
| `DeleteProject` | `POST {url}/api/projects/delete` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/projects/delete) | No (401) |
| `SearchProjectKeys` | `GET {url}/api/projects/search?organization=&q=&p=&ps=` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/projects/search) | Yes, not `provisioning` only |

`GetProject` first reads the organization to get its UUID, because the list call
filters by UUID. A project that is missing answers 200 with an empty list.

`keys` and `ids` accept a list only as one comma-separated value. When the
parameter is repeated, the server uses only the last value. The client sends
at most 50 values, and at most 4,000 encoded bytes of values, in one request.
A project key can have 400 characters, and gateways commonly refuse a request
line of more than 8 KB.

Project creation uses the plain project API and does not bind a repository.

The project bindings API below names a project by its legacy identifier and
by nothing else. `GetProject` reports it in `legacyId`. The `id` field holds a
UUID, which the bindings API refuses.

## Project bindings (`project_bindings.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CreateProjectBinding` | `POST {api_url}/dop-translation/project-bindings` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `FindProjectBinding` | `GET {api_url}/dop-translation/project-bindings?projectId=` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |
| `UpdateProjectBinding` | `PATCH {api_url}/dop-translation/project-bindings/{id}` | Web API v2 | Internal | No (400 `AuthenticationType.SOT`) |

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

The write actions take the profile key as `key`. `change_parent` and
`delete` also take `qualityProfile`, but that is the profile name, not its
key. `change_parent` without `parentKey` removes the parent. `delete` also
deletes all the descendants of the profile, and answers 404 for a key that
does not exist. `search` has no pages, shows a change at once, and gives
`parentKey`, `isBuiltIn` and `isDefault`. Without `language`, it gives the
profiles of all the languages. Its `qualityProfile` filter matches the exact
name, with the same case. A missing organization is a 404 with the message
`No organization with key '<key>'`.

No Web API v2 endpoint reads or sets the default profile of a language.
`set_default` takes the profile key and makes the profile the default of its
own language. It does not check a language, so
`sonarqube_cloud_organization_default_quality_profile` first finds the profile
in `search` and refuses a profile of a different language. The API has no
operation to remove a default. `set_default` refuses a custom profile with 400
`Organization '<uuid>' is not allowed to perform this action` when the plan of
the organization does not permit custom profiles. It refuses a MISRA built-in
profile with 403 `Enterprise MISRA rules require a MISRA entitlement` when the
organization does not have the MISRA entitlement, and refuses a token without
the Administer Quality Profiles permission with 403.

`inheritance` takes the profile key without `organization`, and `ancestors`
lists the ancestors from the parent up. The import of
`sonarqube_quality_profile_rule` uses it to compare an inherited rule with the
rule of the parent profile.

No Web API v2 endpoint assigns a profile to a project. `add_project` and
`remove_project` take the profile key and the project key, without
`organization`. `add_project` replaces the profile of the same language and
does not change the other languages. With the default profile, it makes an
explicit assignment. `remove_project` of a profile that the project does not
use answers 204 and does nothing. Both answer 404 `Component key '<key>' not
found` for a missing project and 404 `Quality Profile with key '<key>' does
not exist` for a missing profile. A token without one of the permissions
Administer Quality Profiles, edit of the profile, or Administer of the project
also gets a 404, with a different message. Both refuse an organization whose
plan does not permit custom profiles with 400 `Organization '<uuid>' is not
allowed to perform this action`.

`search` with `project` and `language` gives the profile that the project
uses for the language: the assigned profile, or else the default profile.
`sonarqube_project_quality_profile` reads it, and not
`projects?selected=selected`, so that a change outside of Terraform shows as
drift and not as a deleted assignment. With `project`, `search` needs
`organization`. It does not check a permission on the project. It answers
404 `Component key '<key>' not found` for a missing project, and a different
404 for a project of a different organization.

## Quality profile editors (`quality_profile_editors.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `AddQualityProfileEditor` | `POST {url}/api/qualityprofiles/add_group`, `POST {url}/api/qualityprofiles/add_user` | Web API v1 | Internal | No (500) |
| `RemoveQualityProfileEditor` | `POST {url}/api/qualityprofiles/remove_group`, `POST {url}/api/qualityprofiles/remove_user` | Web API v1 | Internal | No (500) |
| `HasQualityProfileEditor` | `GET {url}/api/qualityprofiles/search_groups`, `GET {url}/api/qualityprofiles/search_users` | Web API v1 | Internal | No (500) |

No public endpoint and no Web API v2 endpoint lets a group or a user edit a
profile. The SonarQube Cloud web application uses these internal web
services. They name the profile by `qualityProfile` (its name), `language`
and `organization`, not by its key, so the caller reads the profile from its
key first. A rename keeps the editors.

`add_*` and `remove_*` succeed when nothing changes. A missing group answers
404 `No group with name '<name>' in organization '<key>'`, and a missing user
answers 404 `User with login '<login>' is not found'`. A missing profile
answers 404 `Quality Profile for language '<language>' and name '<name>' does
not exist in organization '<key>'`. A built-in profile answers 400
`Operation forbidden for built-in Quality Profile ...`, and a user who is not
a member of the organization answers 400. On a plan without custom profiles,
the writes answer 403 `This action is not allowed for your current
subscription plan`, and the searches answer an empty list.

`search_groups` and `search_users` with `selected=selected` give the editors,
with `paging.total`. Their `q` filter keeps the groups whose name holds the
value, and the users whose login or name holds it, without regard to case.
The searches show a change at once. On SonarQube Cloud, `add_group` and
`remove_group` find the group without regard to case (SonarQube Server is not
checked yet), so `HasQualityProfileEditor` matches a group
name without regard to case and a login exactly. `remove_user` refuses a
user who is not a member of the organization any more with 400, and
`RemoveQualityProfileEditor` takes this as success.

## Quality profile rules (`quality_profile_rules.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `ActivateQualityProfileRule`, `ResetQualityProfileRule` | `POST {url}/api/qualityprofiles/activate_rule` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/activate_rule) | No (401) |
| `DeactivateQualityProfileRule` | `POST {url}/api/qualityprofiles/deactivate_rule` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/qualityprofiles/deactivate_rule) | No (500) |
| `GetActiveRule` | `GET {url}/api/rules/search?qprofile=&activation=true&rule_key=&f=actives,params,impacts` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/rules/search) | Yes |

No Web API v2 endpoint manages the rules of a quality profile.

`activate_rule` takes `params` and `impacts` as `key1="value1";key2="value2"`.
The quotes let a value contain `;`, but the server does not read a doubled
quote as one quote, so a value cannot contain `"`. The provider does not send
`severity`, which is deprecated and replaced by `impacts`. `reset=true` sets
the values of the parent profile on an inherited rule, and the default values
of the rule on a rule that the profile activates itself.

On SonarQube Cloud, the call keeps the current value of each parameter and
impact that it does not get. It ignores an impact for a software quality that
the rule does not have, and still answers 204. SonarQube Server is different
([`ActivateRuleAction`](https://github.com/SonarSource/sonarqube/blob/master/server/sonar-webserver-webapi/src/main/java/org/sonar/server/qualityprofile/ws/ActivateRuleAction.java)):
an impact for a software quality that the rule does not have gives 400 `Only
impacts defined on the rule can be overridden`, and `impacts` probably sets
the whole set of impacts. Check this before the provider supports SonarQube
Server.

`deactivate_rule` answers 204 for a rule that is not active. It also
deactivates an inherited rule, and the deactivation applies to the
descendants of the profile too, also to an `OVERRIDES` rule there. An
activation again gives the descendants new activations with the values of
the profile, so their overrides do not come back. The setting
`sonar.qualityProfiles.allowDisableInheritedRules` (default `true`) permits
this on SonarQube Cloud and on SonarQube Server. On SonarQube Server, an
administrator can set it to `false`, and then `deactivate_rule` answers 400
`Cannot deactivate inherited rule '<rule>'`.

The server deletes the activation of a deactivated inherited rule. Thus
`activate_rule` with `reset=true` does nothing for it: the server ignores a
reset for a rule that is not active. `activate_rule` without values
activates it again as `INHERITED`, with the values of the parent. With
values, the server activates it as `NONE`, the profile's own rule, so the
provider first activates it without values and then sends the values, which
gives `OVERRIDES`. A change of the rule in the parent profile activates the
rule again in the profile.

`rules/search` finds the profile by `qprofile`, so it needs no
`organization`. With `f=actives`, `actives` gives the activation of the rule
in this profile only: `inherit` (`NONE`, `INHERITED` or `OVERRIDES`), all the
parameters with their values, also the default ones, and the impacts.
`rules[].params[].defaultValue` and `rules[].impacts` give the defaults of the
rule. A rule that is not active gives `total` 0, and an unknown profile gives
404. The search index can lag behind a write by some seconds. `rules/show`
needs `organization` and gives the activations of all the profiles, so the
provider does not use it.

A built-in profile refuses each change with 400 `Operation forbidden for
built-in Quality Profile`. A MISRA rule without the MISRA entitlement of the
organization gives 403 `Enterprise MISRA rules require a MISRA entitlement`.

## Organization tokens (`organization_tokens.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CreateOrganizationToken` | `POST {api_url}/authentication/token-definitions` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) | Yes, `admin` |
| `ListOrganizationTokens`, `FindOrganizationToken` | `GET {api_url}/authentication/token-definitions?organizationId=&pageIndex=&pageSize=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) | Yes, `admin` |
| `DeleteOrganizationToken` | `DELETE {api_url}/authentication/token-definitions/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) | Yes, `admin` |
| `CreateTokenResourceLink` | `POST {api_url}/authentication/token-resource-links` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) | Yes, `admin` |
| `ListTokenResourceLinks` | `GET {api_url}/authentication/token-resource-links?organizationTokenId=&pageIndex=&pageSize=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) | Yes, `admin` |
| `DeleteTokenResourceLink` | `DELETE {api_url}/authentication/token-resource-links/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-externalauthentication-0-0) | Yes, `admin` |

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

A token resource link gives a token its project permissions on one project.
`resourceId` is the project `id` that `FindProjectsByKey` returns. The
projects API docs call it the main branch UUID. The server refuses the legacy
ID. Links to other resource types give a warning, and the resource keeps them. The list page size is at most 500.

The server does not check that a token has only project permissions, and it
accepts links on an organization-wide token. The resource does this check,
and it does not read or change the links of an organization-wide token. A
repeated `POST` returns the existing link. A `DELETE` of a link that is gone
answers 204. A `POST` with an unknown project UUID answers 404. The resource
finds each project before it creates the token, so that a missing project
creates no token.

When a project is deleted, its link stays in the list. The resource removes
the key of such a link from the state and shows a warning. Revocation of a
token removes its links, and then the list answers 404 `Token not found`.

## Enterprises (`enterprises.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `CreateEnterprise` | `POST {api_url}/enterprises/enterprises` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `GetEnterprise` | `GET {api_url}/enterprises/enterprises/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `GetEnterpriseByKey` | `GET {api_url}/enterprises/enterprises?enterpriseKey=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `ListEnterprises` | `GET {api_url}/enterprises/enterprises` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `UpdateEnterprise` | `PATCH {api_url}/enterprises/enterprises/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `DeleteEnterprise` | `DELETE {api_url}/enterprises/enterprises/{id}`, then `GET {api_url}/enterprises/enterprises/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |

The API needs a user token. With a scoped organization token, the list is
empty, and the other calls give 403 or 404.

The gateway refuses a create without `avatar`, `license` or `organizationId`,
although the docs show them as optional. `avatar` must be a string: `""` is
accepted, and `null` gives 400. `organizationId` accepts the legacy ID or the
UUID. A license is single-use: a successful create uses it up, also when the
enterprise is deleted later. A create that fails validation does not use it.
A key that another enterprise uses gives 400 `Enterprise key is not valid`,
which does not tell the cause, so the resource checks the key first.

`?enterpriseKey=` answers a list of one item, and 404 for an unknown key. A
read leaves out `avatar` when the enterprise never had one. `PATCH` takes
`application/merge-patch+json`. It also changes `key`, but the docs do not
list `key`, so the client does not send it.

`DELETE` gives 204, also for an enterprise that is already gone, so
`DeleteEnterprise` reads the enterprise after the delete and expects 404. It
deletes the portfolios of the enterprise. The organizations stay, on a paid plan
without enterprise features.

## Enterprise organizations (`enterprise_organizations.go`)

| Function | Endpoint | Surface | Public or internal | SOT |
| --- | --- | --- | --- | --- |
| `ListEnterpriseOrganizationsOfEnterprise` | `GET {api_url}/enterprises/enterprise-organizations?enterpriseId=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `ListEnterpriseOrganizationsOfOrganization` | `GET {api_url}/enterprises/enterprise-organizations?organizationId=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `AttachEnterpriseOrganization` | `POST {api_url}/enterprises/enterprise-organizations` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |
| `DetachEnterpriseOrganization` | `DELETE {api_url}/enterprises/enterprise-organizations/{id}` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-enterprises-external-1-2-0) | No |

The lists have no pages. `organizationId` accepts the legacy ID or the UUID,
in the filter and in the body of `POST`. An unknown organization, an
enterprise that the token cannot read, and a scoped organization token all
give 200 with an empty list. Thus, when the relation is missing, the resource
reads the enterprise to find a missing access. A deleted enterprise gives 404.
A user token without access possibly gives 404 too (not checked), so a read
warns before it removes the attachment, and a delete reads the relation of
the organization before it skips the detach.

An organization on the free plan can be attached, and it moves to a paid plan.
An organization that is already in an enterprise gives 400 `Organization is
already part of an enterprise`.

`DELETE` takes the ID of the relation, not of the organization. A relation
that does not exist gives 404. A detached organization stays on a paid plan
without enterprise features. A detach of the last organization also deletes
the enterprise, so the resource refuses it.
