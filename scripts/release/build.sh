#!/usr/bin/env bash
# Builds the release files in dist/ and prints the version.
#
# Usage: build.sh [--snapshot]
set -euo pipefail

goreleaser release --clean "$@" >&2

version="$(jq -r .version dist/metadata.json)"
project="$(jq -r .project_name dist/metadata.json)"
cp terraform-registry-manifest.json "dist/${project}_${version}_manifest.json"

echo "${version}"
