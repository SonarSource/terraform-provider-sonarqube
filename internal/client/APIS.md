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
such as `AZcwYwExlol79EFABiuM`. The bindings API below names an organization
by that identifier and by nothing else, so a read of the organization is how
an organization key reaches it.

## DevOps platform bindings (`bindings.go`)

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

Every endpoint of these two domains was read against `dev11.sc-dev11.io` on
2026-09-24. A bind that succeeds was not tested: it needs a GitHub
application installation that is bound to no organization.

## DevOps platform applications (`dop_applications.go`)

| Function | Endpoint | Surface | Public or internal |
|---|---|---|---|
| `ListDopApplications` | `GET {api_url}/dop-translation/dop-applications?devOpsPlatform=` | Web API v2 | Internal |

A binding to github.com accepts an installation of one of these applications
only, because the server looks an installation up in its own records instead
of asking GitHub.
