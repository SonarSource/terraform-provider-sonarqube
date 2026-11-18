# terraform-provider-sonarqube

Terraform provider for SonarQube. The alpha release supports SonarQube Cloud.

## Requirements

Terraform 1.6.1 or later. Terraform 1.6.0 and earlier refuse a provider
version after its signing key expires, and the signing key of this provider
expires each year.

## Embed the provider in a Go program

The [`sonarqube`](sonarqube/sonarqube.go) package gives the constructor of the
provider to Go programs outside this module, for example Crossplane Upjet:

```go
import "github.com/SonarSource/terraform-provider-sonarqube/sonarqube"

factory := sonarqube.New(version)
```

This API is experimental. It can change in any release before version 1.0.0.

* Set `url`, `api_url` and `token` in the provider configuration. Do not use
  the `SONARQUBE_*` environment variables: all of the process shares them.
* The provider makes its own User-Agent header. Do not replace it. To add text
  at the end, set the `TF_APPEND_USER_AGENT` environment variable.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) to build the provider, use a local
build, run the tests and generate the documentation.

## Releases

See [RELEASING.md](RELEASING.md) for the version scheme and the release
procedure, and [CHANGELOG.md](CHANGELOG.md) for the changes in each version.
