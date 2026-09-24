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
