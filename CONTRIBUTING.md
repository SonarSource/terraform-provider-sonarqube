# Contributing

## Requirements

* Go, at the version that `go.mod` gives.
* Terraform 1.6.1 or later.

`.mise.toml` gives the versions of the development tools. With
[mise](https://mise.jdx.dev), `mise install` installs them.

## Build the provider

```bash
go build ./...
```

To install the provider binary in `$(go env GOBIN)`, or in
`$(go env GOPATH)/bin` when `GOBIN` is not set:

```bash
go install .
```

## Use a local build

Terraform can use your local build instead of the version from the
registry. Add a `dev_overrides` block to `~/.terraformrc`. Replace the path
with the directory where `go install` puts the binary: the output of
`go env GOBIN`, or the output of `go env GOPATH` followed by `/bin` when
`GOBIN` is not set:

```hcl
provider_installation {
  dev_overrides {
    "sonarsource/sonarqube" = "/home/<user>/go/bin"
  }

  direct {}
}
```

Run `go install .` after each change. Then run `terraform plan` or
`terraform apply` in your configuration. Do not run `terraform init` for
this provider: Terraform uses the override directly and shows a warning
about it. Remove the block when you want to use the registry version again.

## Run the unit tests

```bash
env -u TF_ACC go test -race ./...
```

To run one package or one test:

```bash
go test ./internal/client -run '^TestGetOrganization$' -count=1
```

The unit tests do not connect to SonarQube. `env -u TF_ACC` makes sure
that the acceptance tests do not run, also when your shell sets `TF_ACC`.

## Run the acceptance tests

The acceptance tests make, change and delete real objects on a SonarQube
Cloud instance. Do not run them against production.

Run the acceptance tests with `make testacc`. It sets `TF_ACC=1`, runs the
tests, and fails when a test skips, except the one that the `Makefile` names.
It needs `SONARQUBE_URL` and `SONARQUBE_TOKEN`, and four more variables:

* `SONARQUBE_TEST_ALLOWED_HOSTS`: the hosts where a test may make and delete
  an organization, separated by commas. An entry allows its sub-domains as
  well. Empty allows nothing but a local instance, and a target that it does
  not name stops the run. A production instance is refused whatever this
  variable says. Set it to the instance you test against, never to
  production.
* `SONARQUBE_TEST_ORGANIZATION`: an organization that the tests share. The
  tests that only read use it, and so do the tests that need a paid plan, such
  as the tests of the default gate, the default template and the permission
  templates. An organization that a test makes is on the Free plan, and no API
  can change its plan. The tests that change the shared organization call
  `acctest.PreCheckSharedOrganization`, which stops the run when the
  organization is not on the Team or the Enterprise plan, when the token user
  has no direct `admin` grant on it, or when its key starts with
  `acctest.KeyPrefix`. Admin through a group is not sufficient. Each test
  deletes what it makes there, and the sweepers delete what a test leaves.
* `SONARQUBE_TEST_GITHUB_INSTALLATION_ID`: an installation of the GitHub
  application of the instance that no organization is bound to, for the tests
  that bind. Nothing can make one: install the application from
  `https://github.com/apps/<application_key>` on a GitHub organization that is
  not bound yet, and read the identifier from the address that GitHub shows.
  The `sonarqube_cloud_dop_applications` data source reports the application key.
* `SONARQUBE_TEST_GITHUB_REPOSITORY`: a public repository, as `owner/name`,
  that the installation above can see, for the tests that bind a project.
  Write the slug with the case that GitHub shows. The test deletes its
  organization at the end, which removes the binding, so one repository
  serves every run.

Each test gives the prefix `acctest.KeyPrefix` (`tf-acc-test-`) to what it
makes. When a run stops before its cleanup, the sweepers in
`internal/acctest/sweep.go` delete what starts with the prefix: the
organizations of the token user, and the projects, groups, permission
templates and quality gates of `SONARQUBE_TEST_ORGANIZATION`. They never
delete `SONARQUBE_TEST_ORGANIZATION` itself. Run them with the same variables:

```bash
make sweep
```

`-sweep-allow-failures` lets each sweeper run when another one fails. A
sweeper keeps a default template or a default gate, because the server cannot
delete it, and logs a warning. Run them on one package only, because the
sweepers are the same in each package that imports `acctest`. `-sweep` acts
only where `TestMain` calls `resource.TestMain`. A package without that
`TestMain` ignores the flag, and `go test` refuses it in a package that does
not import `helper/resource`.

`.github/workflows/acceptance-tests.yml` runs the acceptance tests against
SonarQube Cloud staging after each merge to `master` and each night at 03:17
UTC. A failed run on `master` sends a Slack message to the squad. GitHub
disables the schedule of a public repository after 60 days without activity
in the repository. Then the nightly runs stop without a message. To see the
state of the workflow and to enable it again, run:

```bash
gh workflow list --all
gh workflow enable acceptance-tests.yml
```

A disabled workflow shows the state `disabled_inactivity`.

To run one acceptance test:

```bash
TF_ACC=1 go test ./internal/provider/cloud -run '^TestAccQualityGateResource$' -count=1 -v
```

## Documentation

`tfplugindocs` generates the Terraform Registry pages in `docs/`. Do not edit
`docs/` by hand. The page of a resource or data source comes from:

* its schema: the `MarkdownDescription` (or `Description`) of the schema gives
  the text at the top of the page, and the descriptions of the attributes give
  the schema section;
* `examples/resources/<name>/resource.tf` or
  `examples/data-sources/<name>/data-source.tf`: the example;
* `examples/resources/<name>/import.sh`: the import section.

`templates/resources.md.tmpl` and `templates/data-sources.md.tmpl` give the
layout of these pages. The provider page, `docs/index.md`, comes from
`templates/index.md.tmpl`, the example `examples/provider/provider.tf`, and
the descriptions of the provider schema. After a change to one of these
sources, run:

```bash
make docs
```

Commit the changes in `docs/` with your change. The `Docs` job of the build
workflow fails when `docs/` is different from the output of `make docs`.

## Checks before a pull request

The `Build` and `pre-commit` workflows run these checks on each pull request.
To run them locally:

```bash
gofmt -l .
go mod tidy
go vet ./...
go build ./...
env -u TF_ACC go test -race ./...
make docs
pre-commit run --all-files
```

After `go mod tidy` and `make docs`, `git status` must show no unexpected
changes.
