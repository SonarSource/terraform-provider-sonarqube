# Releasing

A release is a GitHub release with the files that the Terraform Registry
needs: one zip file for each platform, the manifest, the `SHA256SUMS` file,
and its GPG signature. The `Release` workflow makes it when a `v*` tag is
pushed.

## Versions

The versions are semantic versions, and the tags have a `v` prefix.

Increase the minor version for new features or breaking changes, and the
patch version for fixes.

The workflow marks a version with a `-` suffix, for example `1.0.0-rc.1`, as a
GitHub pre-release. Terraform selects a pre-release version only when the
version constraint gives that exact version.

The registry keeps each version that it publishes. Nobody can remove a
version from the registry, also when the GitHub release is deleted.

## Procedure

1. In a pull request, change the header of the version in `CHANGELOG.md`
   from `(Unreleased)` to the release date, for example
   `## 0.2.0 (January 15, 2027)`. Change the version in
   `examples/provider/provider.tf` to the new version, and run `make docs`
   to update `docs/index.md`. Merge the pull request.
2. Find the commit to release: a commit of `master` that has this change, and
   on which the `Build` and the `Acceptance tests` workflows passed. The
   `Release` workflow refuses a commit without these two successful runs. If
   the acceptance run of the commit was cancelled, start the workflow by hand
   on `master` while that commit is the newest one.
3. Tag that commit, and push the tag:

   ```bash
   git fetch origin
   git tag v0.2.0 <commit>
   git push origin v0.2.0
   ```

4. Approve the `release` environment in the run of the `Release` workflow.
5. Run Resync on the provider in HCP Terraform. The registry does not get a
   new version automatically. Until TFP-93, do not do this step (see
   "Signing key").

## When a release fails

The tag rulesets let only DOPEX make a `v*` tag, and nobody can move or
delete one. The workflow checks everything before it publishes, thus a run
that stops before the last step publishes nothing. But the tag stays.

* If the cause is outside the repository, for example Vault, the registry or
  the runner, run the failed job again.
* If the cause is in the repository, fix it on `master`, and release the
  next version, for example `v0.2.1`.
* `gh release create` makes a draft, uploads the files, and then publishes
  the draft. If the last step stops, a draft can stay. Delete the draft
  before you run the job again.

GitHub releases are immutable. A published release cannot change.

## Signing key

The workflow reads the signing key from Vault. The path is
`SIGNING_KEY_VAULT_PATH` in `.github/workflows/release.yml`. The registry
must have the public key of each key that signs a release.

When the key changes, update `SIGNING_KEY_VAULT_PATH` and `SIGNING_KEY_ID`
together. The registry must have the public key of the new key before a
release can pass the "Verify the release files" step. Until TFP-93, this is
not true: see the next paragraph.

Until the registry has the subkey `9582347C`, the "Verify the release files"
step also accepts the public key of the signing key. The release signs only
with the key that has the fingerprint `SIGNING_KEY_FPR`, so a different key
in Vault stops the release before it publishes. TFP-93 removes this
exception and `SIGNING_KEY_FPR`.

Thus a green run does not show that the registry accepts the release. The
step shows a warning when the registry keys cannot verify the signature.
Until the registry has the subkey, do not run Resync in HCP Terraform: it
fails for these releases. Installs from the GitHub release files do not
check the signature, so they work.
