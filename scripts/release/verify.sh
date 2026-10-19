#!/usr/bin/env bash
# Checks the checksum file, the checksums and the signature.
#
# Usage: verify.sh <dist dir> <version> <armored public key file>
set -euo pipefail

dist="$1"
version="$2"
public_key="$3"

prefix="terraform-provider-sonarqube_${version}"
sums="${dist}/${prefix}_SHA256SUMS"

if ! diff -u <(printf '%s\n' \
  "${prefix}_darwin_amd64.zip" \
  "${prefix}_darwin_arm64.zip" \
  "${prefix}_linux_amd64.zip" \
  "${prefix}_linux_arm64.zip" \
  "${prefix}_manifest.json" \
  "${prefix}_windows_amd64.zip") <(awk '{ print $2 }' "${sums}" | LC_ALL=C sort); then
  echo "${sums} does not list the expected files." >&2
  exit 1
fi
(cd "${dist}" && shasum -a 256 --check --strict "${sums##*/}")

GNUPGHOME="$(mktemp -d)"
export GNUPGHOME
chmod 700 "${GNUPGHOME}"
trap 'gpgconf --kill gpg-agent 2>/dev/null || true; rm -rf "${GNUPGHOME}"' EXIT

gpg --batch --quiet --import "${public_key}"
gpg --batch --verify "${sums}.sig" "${sums}"
