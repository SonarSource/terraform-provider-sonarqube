# Releasing

A release is a GitHub release with the files that the Terraform Registry
needs: one zip file for each platform, the manifest, the `SHA256SUMS` file,
and its GPG signature. The `Release` workflow makes it when a `v*` tag is
pushed.

## Versions

The versions are semantic versions, and the tags have a `v` prefix.

* Alpha: `v0.1.0-alpha.1`, `v0.1.0-alpha.2`, and so on. Make an alpha
  release only when a user needs one, not for each change.
* After the first registry release, `v0.1.0`: increase the minor version for
  new features or breaking changes, and the patch version for fixes.

Terraform selects a pre-release version, such as `0.1.0-alpha.1`, only when
the version constraint gives that exact version. A constraint such as `>=` or
`~>` does not select it. The workflow marks a version with a `-` suffix as a
GitHub pre-release.

The registry keeps each version that it publishes. Nobody can remove a
version from the registry, also when the GitHub release is deleted.

## Procedure

1. In a pull request, change the header of the version in `CHANGELOG.md`
   from `(Unreleased)` to the release date, for example
   `## 0.1.0-alpha.1 (October 20, 2026)`. Change the version in
   `docs/index.md` and `examples/provider/provider.tf` to the new version.
   Merge the pull request.
2. Find the commit to release: a commit of `master` that has this change, and
   on which the `Build` and the `Acceptance tests` workflows passed. The
   `Release` workflow refuses a commit without these two successful runs. If
   the acceptance run of the commit was cancelled, start the workflow by hand
   on `master` while that commit is the newest one.
3. Tag that commit, and push the tag:

   ```bash
   git fetch origin
   git tag v0.1.0-alpha.1 <commit>
   git push origin v0.1.0-alpha.1
   ```

4. Approve the `release` environment in the run of the `Release` workflow.
5. After the repository is connected to the registry: run Resync on the
   provider in HCP Terraform. The registry does not get a new version
   automatically.

## When a release fails

The tag rulesets let only DOPEX make a `v*` tag, and nobody can move or
delete one. The workflow checks everything before it publishes, thus a run
that stops before the last step publishes nothing. But the tag stays.

* If the cause is outside the repository, for example Vault, the registry or
  the runner, run the failed job again.
* If the cause is in the repository, fix it on `master`, and release the
  next version, for example `v0.1.0-alpha.2`.
* `gh release create` makes a draft, uploads the files, and then publishes
  the draft. If the last step stops, a draft can stay. Delete the draft
  before you run the job again.

GitHub releases are immutable. A published release cannot change.

## Signing key

The workflow reads the signing key from Vault. The path is
`SIGNING_KEY_VAULT_PATH` in `.github/workflows/release.yml`. The registry
must have the public key of each key that signs a release.
