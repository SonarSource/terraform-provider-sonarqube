#!/usr/bin/env bash
# Compares the provider schemas of the working tree with the schemas at the
# merge base of a base ref, and lists the changes that can break a user.
#
#   scripts/schema-check.sh [base ref]
#
# The base ref defaults to origin/master. The exit status is 1 when a change
# can break a user, and 2 on an error. Only the last command can give 1, so
# that the breaking-change label cannot accept a check that did not run.
set -euo pipefail

base_ref="${1:-origin/master}"
root="$(git rev-parse --show-toplevel)" || exit 2
cd "${root}" || exit 2

merge_base="$(git merge-base "${base_ref}" HEAD)" || {
  echo "No merge base of ${base_ref} and HEAD. Fetch ${base_ref} with its history." >&2
  exit 2
}

tmp="$(mktemp -d)" || exit 2
trap 'git worktree remove --force "${tmp}/base" >/dev/null 2>&1 || true; rm -rf "${tmp}"' EXIT

git worktree add --quiet --detach "${tmp}/base" "${merge_base}" || exit 2

# The base can have an older checker or none, so the checker of the working
# tree reads the schemas of the base.
rm -rf "${tmp}/base/internal/schemacheck" || exit 2
cp -R internal/schemacheck "${tmp}/base/internal/" || exit 2

# go run gives 1 for each failure, so build the checker to keep its exit status.
(cd "${tmp}/base" && go build -o "${tmp}/base-schemacheck" ./internal/schemacheck/cmd/schemacheck) || {
  echo "The checker of the working tree does not build with the base ${merge_base}." >&2
  echo "Change internal/schemacheck so that it also builds with the base, for example" >&2
  echo "when the change uses a new provider.New or a new framework API." >&2
  exit 2
}
go build -o "${tmp}/schemacheck" ./internal/schemacheck/cmd/schemacheck || exit 2
"${tmp}/base-schemacheck" snapshot "${tmp}/base.json" || exit 2
"${tmp}/schemacheck" snapshot "${tmp}/head.json" || exit 2

echo "Base: ${base_ref} at ${merge_base}"
"${tmp}/schemacheck" compare "${tmp}/base.json" "${tmp}/head.json"
