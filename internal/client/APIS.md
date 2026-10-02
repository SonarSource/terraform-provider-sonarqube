# APIs consumed by the provider

Every SonarQube API endpoint that this provider calls, grouped by domain. All
calls go through this package (`internal/client`) — no other package makes an
HTTP call. When a new resource or data source needs a new domain, add a
section for it here. When an existing domain gains or loses an endpoint,
update its table in the same change.

Web API v2 answers at the `api.` host and takes a bearer token. Web API v1
answers at the instance host and takes the token as the basic-auth user name.

An endpoint is "Public" only when SonarSource's own docs link one as public.

## Organizations (`organizations.go`)

| Function | Endpoint | Surface | Public or internal |
|---|---|---|---|
| `GetOrganization` | `GET {api_url}/organizations/organizations?organizationKey=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-sonarcloud-organizations-organizations-external-1-0-2#/organizations/list-organizations) |
| `CreateOrganization` | `POST {url}/api/organizations/create` | Web API v1 | Internal |
| `UpdateOrganization` | `POST {url}/api/organizations/update` | Web API v1 | Internal |
| `UpdateOrganizationKey` | `POST {url}/api/organizations/update_key` | Web API v1 | Internal |
| `DeleteOrganization` | `POST {url}/api/organizations/delete` | Web API v1 | Internal |

Web API v2 has no create, update, rename or delete for an organization today,
so every write goes through the internal v1 surface.

`GetOrganization` also reports the internal identifier of the organization,
an opaque string of 20 characters. The bindings API below names an organization
by that identifier and by nothing else, so a read of the organization is how
an organization key reaches it.

## DevOps platform bindings (`organization_bindings.go`)

| Function | Endpoint | Surface | Public or internal |
|---|---|---|---|
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
|---|---|---|---|
| `ListDopApplications` | `GET {api_url}/dop-translation/dop-applications?devOpsPlatform=` | Web API v2 | Internal |

A binding to github.com accepts an installation of one of these applications
only, because the server looks an installation up in its own records instead
of asking GitHub.

## Projects (`projects.go`)

| Function | Endpoint | Surface | Public or internal |
|---|---|---|---|
| `GetProject` | `GET {api_url}/projects/projects?keys=&organizationIds=` | Web API v2 | [Public](https://api-docs.sonarsource.com/sonarqube-cloud/default/public-projectsexternal-0-0-1#/Projects/getProjects) |
| `CreateProject` | `POST {url}/api/projects/create` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/projects/create) |
| `DeleteProject` | `POST {url}/api/projects/delete` | Web API v1 | [Public](https://sonarcloud.io/web_api/api/projects/delete) |

`GetProject` first reads the organization to get its UUID, because the list call
filters by UUID. A project that is missing answers 200 with an empty list.

Project creation uses the plain project API and does not bind a repository.

The project bindings API below names a project by its legacy identifier and
by nothing else. `GetProject` reports it in `legacyId`. The `id` field holds a
UUID, which the bindings API refuses.

## Project bindings (`project_bindings.go`)

| Function | Endpoint | Surface | Public or internal |
|---|---|---|---|
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
|---|---|---|---|
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
