# terraform-provider-sonarqube

Shared instructions for all coding agents working in this repository.

## Purpose and scope

One official Terraform provider for SonarQube Cloud and SonarQube Server.
The alpha supports Cloud only and targets organizations bound to GitHub.

## Data sensitivity

This repository is public. Never add any of the following to files,
comments, commit messages, or pull request and issue descriptions:

- **Restricted data**: secrets, API tokens, keys or credentials
- **Confidential data**: PII, customer source code, license keys, financial
  records, employee/candidate data, or customer data that could put a
  customer at risk.
- **Internal-only information**: non-public strategic decisions,
  architecture diagrams, specifications, company records/correspondence, or
  contact lists.

Use placeholder values in examples, comments, and tests (e.g. `test-token`,
`dev.example.io`) instead of real hosts, keys, or identifiers.

Never copy values from a live instance, a captured API response, or another
repository into this one. That covers identifiers, keys, and organization or
repository names. Keep the shape of a response, and replace each value with
an obvious placeholder, such as `project-legacy-id` or
`my-github-org/my-repo`.

If a change would introduce something that might fall into one of these
categories, or something already in the repository looks like it does, stop
and ask the user before adding, removing, redacting, or otherwise changing
it — do not decide unilaterally.

## Repository structure

- `main.go`: provider executable entry point.
- `internal/provider`: the provider: its settings, and the registration of
  every resource and data source.
  - `cloud`: resources and data sources for SonarQube Cloud only.
  - `server`: resources and data sources for SonarQube Server only. It does
    not exist yet.
  - `shared`: resources and data sources for both products.
  - `configure`: gives the resources and data sources the client that the
    provider built.
  - `validate`: the rules that the server applies to keys.
  - `providertest`: helpers for the unit tests of the packages above.
- `internal/acctest`: helpers for the acceptance tests, such as the provider
  factories and the check that refuses a production instance.
- `internal/client`: SonarQube HTTP transport, authentication, API payloads,
  and API errors.
- `internal/client/APIS.md`: inventory of consumed API endpoints.
- `internal/provider/PRODUCTS.md`: the products that each resource and data
  source supports, and why.
- `.github/workflows`: GitHub Actions workflows.

Use `terraform-plugin-framework` for provider implementations and
`terraform-plugin-testing` for Terraform acceptance tests. Follow the
existing `sonarqube_cloud_organization` data source and its tests when
adding functionality.

## Build and validation

Use the Go version declared in `go.mod`. Run commands from the repository
root. `.github/workflows/build.yml` defines the CI checks.

During development, target the affected package or test:

```bash
go test ./internal/client -run '^TestGetOrganization' -count=1
```

After Go changes, format the changed Go files and run:

```bash
gofmt -w <changed-go-files>
go vet ./...
go build ./...
env -u TF_ACC go test -race ./...
```

Unsetting `TF_ACC` keeps this validation independent of live acceptance
tests, even when the variable is set in the calling shell.

The acceptance tests need `TF_ACC=1`, `SONARQUBE_URL` and `SONARQUBE_TOKEN`,
and four more variables:

- `SONARQUBE_TEST_ALLOWED_HOSTS`: the hosts where a test may make and delete
  an organization, separated by commas. An entry allows its sub-domains as
  well. Empty allows nothing but a local instance, and a target that it does
  not name stops the run. A production instance is refused whatever this
  variable says. Set it to the instance you test against, never to
  production.
- `SONARQUBE_TEST_ORGANIZATION`: an organization that the token can read, for
  the tests that only read.
- `SONARQUBE_TEST_GITHUB_INSTALLATION_ID`: an installation of the GitHub
  application of the instance that no organization is bound to, for the tests
  that bind. Nothing can make one: install the application from
  `https://github.com/apps/<application_key>` on a GitHub organization that is
  not bound yet, and read the identifier from the address that GitHub shows.
  The `sonarqube_cloud_dop_applications` data source reports the application key.
- `SONARQUBE_TEST_GITHUB_REPOSITORY`: a public repository, as `owner/name`,
  that the installation above can see, for the tests that bind a project.
  Write the slug with the case that GitHub shows. The test deletes its
  organization at the end, which removes the binding, so one repository
  serves every run.

When imports or dependencies change, run `go mod tidy` and review both
`go.mod` and `go.sum`.

For documentation or workflow changes, run the applicable checks from
`.pre-commit-config.yaml`:

```bash
pre-commit run --files <changed-files>
```

Report which checks ran and distinguish failures, skipped checks, and
checks that could not run.

## Implementation rules

- Route all SonarQube HTTP requests through `internal/client`. Keep
  Terraform schemas, state, and diagnostics in `internal/provider`.
- Reuse the client's transport and authentication helpers. Web API v2
  uses the API host with bearer authentication; the older web services
  use the instance host with the token as the basic-auth username.
- Preserve `SONARQUBE_URL`, `SONARQUBE_API_URL`, and `SONARQUBE_TOKEN`.
  Do not substitute `SONAR_TOKEN`: CI commonly uses it for analysis.
- Preserve Terraform's distinction between null, unknown, and known
  values. Unknown provider settings must produce diagnostics rather than
  silently falling back to environment variables.
- Keep tokens sensitive and exclude credentials from logs and diagnostics.
- Propagate request contexts and return errors through Terraform diagnostics.
- An organization read returning 404 can indicate either a missing
  organization or insufficient access. Diagnostics must explain both.
- Explain non-obvious API behavior in comments. Avoid comments that merely
  restate the code.
- Use a public Web API v2 endpoint when one gives the data. Do not use
  `/api/navigation/*`: these endpoints serve the web interface, and they can
  change without notice. Mark every internal endpoint in `APIS.md`.

## SonarQube Cloud and SonarQube Server

The provider will support SonarQube Cloud and SonarQube Server. The alpha
supports SonarQube Cloud only. Do not add SonarQube Server code now, but do
not make a choice that prevents it later.

### Shared or product-specific

A resource or data source is shared when the same configuration means the
same thing on both products. A shared resource can have these differences:

- Scope: the attribute that names the container of the object, such as
  `organization` or `enterprise` on SonarQube Cloud. It is required on the
  product that has the container and refused on the other product. Thus,
  when both products are supported, the schema makes it optional, and the
  provider checks it for the product. Only on the product that has the
  container does the import ID start with the scope.
- API: endpoints, authentication and internal identifiers. Keep them in
  `internal/client`.
- Permitted values that the server checks, such as permission keys or
  metric keys. Let the server refuse a value that is not valid.

Each of these differences makes the resource product-specific:

- A required attribute, other than scope, that one product does not have.
- An attribute that has the same name but a different meaning.
- A different lifecycle. For example, delete removes the object on one
  product but only removes it from state on the other, or a change updates
  the object on one product but replaces it on the other.
- A different import ID, other than the scope prefix.

An optional attribute that only one product supports is permitted when the
provider refuses it on the other product with a clear diagnostic. When a
resource has more than a small number of such attributes, make it
product-specific.

### Names

- `sonarqube_<name>`: shared.
- `sonarqube_cloud_<name>`: SonarQube Cloud only.
- `sonarqube_server_<name>`: SonarQube Server only.

Put a resource or data source in the package of its products: `cloud`,
`server` or `shared`. The package shows the product, so the names of the Go
files, types and constructors do not repeat it. For example,
`sonarqube_cloud_organization` is in `cloud/organization_resource.go`, with
`organizationResource`, `organizationResourceModel` and
`cloud.NewOrganizationResource`. The names in `internal/client` describe API
calls, so they do not follow this rule.

Put a helper in the package that uses it. When packages of more than one
product use it, put it in a package below `internal/provider` that has the
name of its task, such as `configure` or `validate`. Do not use a package
name that tells nothing, such as `common` or `util`. A product package must
not import another product package.

A change from shared to product-specific, or the opposite, is a rename. A
rename after a release breaks the configurations of users.

### Before you add a resource or data source

1. Read the APIs of both products, also when the change is for SonarQube
   Cloud only.
2. Use the rules above to decide if it is shared or product-specific.
3. Add the decision and the reason to `internal/provider/PRODUCTS.md`, and
   get agreement before you write code.

## Adding or changing a resource or data source

1. Follow the existing implementation in the same package. For a new
   resource or data source, first do the steps in "Before you add a
   resource or data source".
2. Add API operations to the appropriate domain file in `internal/client`.
3. Implement the Terraform schema, configuration, and state handling.
4. Register the constructor in the provider's `Resources` or `DataSources`.
5. Update `internal/client/APIS.md` in the same change.
6. Add tests for changed behavior, relevant errors, and state transitions.
   For resources, cover cleanup and a second apply with no changes;
   cover import when supported.

Use `httptest` for client behavior and direct provider tests for schemas,
configuration, diagnostics, and state. These tests must work without live
credentials. Put a helper that the unit tests of more than one package use
in `internal/provider/providertest`.

Write the acceptance tests in an external test package, such as
`cloud_test`. They start the provider, and the provider imports the product
packages, so a test in the product package itself makes an import cycle.
Their helpers are in `internal/acctest`.

## Keep the API inventory current

`internal/client/APIS.md` lists every SonarQube API endpoint the provider
calls, grouped by domain, with each endpoint marked either public (with a
link to its docs page) or internal. Whenever a change to `internal/client`
adds, removes, or changes an endpoint — including adding a new domain for a
new resource or data source — update `APIS.md` in the same change.

## Documentation

Update these instructions when commands, package boundaries, or development
rules change.

<!-- sonar:begin:sonarqube-agentic-analysis-protocol -->
## SonarQube Agentic Analysis protocol

SonarQube Agentic Analysis is the final confirmation layer at the end of every
turn in which you wrote to one or more files in the workspace (create, edit,
patch, format — any tool call that changed file contents on disk).

**Trigger:** the moment before you send your final reply for the turn.

When the trigger fires, you **MUST** run SonarQube Agentic Analysis on the
full set of files you modified this turn — **one command, not a per-file
loop**.

**Preferred:** pass every path you modified this turn in a single invocation
(repeat `--file` for each path). Use paths **relative to the project root**
(e.g. `src/foo.ts`). For end-of-turn analysis, **always** pass `--depth DEEP`
— even when you changed only one file — so cross-file findings are included.

```bash
sonar analyze agentic --project SonarSource_terraform-provider-sonarqube --depth DEEP --file <path/to/file1> --file <path/to/file2>
```

**Fallback:** when you cannot reliably list every modified path (e.g. you did
not track them, or git state is the only source of truth), use the git
change set with DEEP analysis:

```bash
sonar analyze agentic --project SonarSource_terraform-provider-sonarqube --depth DEEP
```

Per-edit hooks run faster STANDARD analysis. End-of-turn analysis must always
use `--depth DEEP` (including a single `--file`). Use `--depth STANDARD` only
for optional faster passes — never for the mandatory end-of-turn run.

Non-negotiable rules:

1. Mid-turn edits do **not** need SonarQube Agentic Analysis — only the final
   on-disk state matters. Skipping SonarQube Agentic Analysis at end-of-turn
   is a protocol violation.
2. Run SonarQube Agentic Analysis **before** sending your final reply,
   marking the task done, or handing control back to the user.
3. If SonarQube Agentic Analysis reports issues on lines you touched in this
   turn, fix them, then re-run SonarQube Agentic Analysis on the same scope
   (change set or explicit file list). Repeat until clean (or only
   pre-existing findings on lines you did not touch remain). Pre-existing
   findings on untouched lines are out of scope — do not "fix" them unless
   the user asked.
4. If SonarQube Agentic Analysis is skipped (no SonarQube Cloud connection,
   or no project configured), state the skip reason to the user once and
   continue — do not retry.
5. Do not suppress, summarize away, or omit SonarQube Agentic Analysis
   findings from your reply. Surface them verbatim.
<!-- sonar:end:sonarqube-agentic-analysis-protocol -->
