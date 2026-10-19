#!/usr/bin/env bash
# Prints the section of a version in CHANGELOG.md, for the release notes.
# Fails when the section is missing, empty, or still marked "Unreleased".
#
# Usage: notes.sh <version> [changelog file]
set -euo pipefail

version="$1"
changelog="${2:-CHANGELOG.md}"

header="$(awk -v prefix="## ${version} (" 'index($0, prefix) == 1 { print; exit }' "${changelog}")"
if [[ -z "${header}" ]]; then
  echo "${changelog} has no section \"## ${version} (<date>)\"." >&2
  exit 1
fi
if [[ "${header}" == *"(Unreleased)"* ]]; then
  echo "The section of ${version} in ${changelog} is marked \"Unreleased\". Write the release date." >&2
  exit 1
fi

notes="$(awk -v header="${header}" '
  $0 == header { found = 1; next }
  found && /^## / { exit }
  found { print }
' "${changelog}")"
if [[ -z "${notes//[[:space:]]/}" ]]; then
  echo "The section of ${version} in ${changelog} is empty." >&2
  exit 1
fi

printf '%s\n' "${notes}"
