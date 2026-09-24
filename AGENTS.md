# terraform-provider-sonarqube

Shared instructions for all coding agents working in this repository.

## Purpose and scope

One official Terraform provider for SonarQube Cloud and SonarQube Server.
The alpha supports Cloud only and targets organizations bound to GitHub.


## Repository structure

- `main.go`: provider executable entry point.
- `internal/provider`: Terraform configuration, schemas, diagnostics,
  resources, data sources, and state.
- `internal/client`: SonarQube HTTP transport, authentication, API payloads,
  and API errors.
- `internal/client/APIS.md`: inventory of consumed API endpoints.
- `.github/workflows`: GitHub Actions workflows.

Use `terraform-plugin-framework` for provider implementations and
`terraform-plugin-testing` for Terraform acceptance tests. Follow the
existing organization data source and its tests when adding functionality.

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

## Adding or changing a resource or data source

1. Follow the existing implementation in the same package.
2. Add API operations to the appropriate domain file in `internal/client`.
3. Implement the Terraform schema, configuration, and state handling.
4. Register the constructor in the provider's `Resources` or `DataSources`.
5. Update `internal/client/APIS.md` in the same change.
6. Add tests for changed behavior, relevant errors, and state transitions.
   For resources, cover cleanup and a second apply with no changes;
   cover import when supported.

Use `httptest` for client behavior and direct provider tests for schemas,
configuration, diagnostics, and state. These tests must work without live
credentials.

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
