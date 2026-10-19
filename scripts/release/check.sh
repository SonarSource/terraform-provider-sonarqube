#!/usr/bin/env bash
# Runs the release steps on a snapshot, with a temporary key.
#
# The temporary key has the shape of the real one: a primary key that only
# certifies, a signing subkey with a passphrase, and an export of the secret
# subkey only.
set -euo pipefail

tmp="$(mktemp -d)"
trap 'gpgconf --kill gpg-agent 2>/dev/null || true; rm -rf "${tmp}"' EXIT
GNUPGHOME="${tmp}/gnupg"
export GNUPGHOME
mkdir -m 700 "${GNUPGHOME}"

version="$(scripts/release/build.sh --snapshot)"

passphrase="release-check"
gpg --batch --quiet --pinentry-mode loopback --passphrase "${passphrase}" \
  --quick-generate-key "Release check <release-check@example.invalid>" rsa3072 cert 1d
primary="$(gpg --batch --with-colons --list-keys | awk -F: '$1 == "fpr" { print $10; exit }')"
gpg --batch --quiet --pinentry-mode loopback --passphrase "${passphrase}" \
  --quick-add-key "${primary}" rsa3072 sign 1d
subkey="$(gpg --batch --with-colons --list-keys |
  awk -F: '$1 == "sub" && $12 ~ /s/ { found = 1; next } found && $1 == "fpr" { print $10; exit }')"
if [[ -z "${subkey}" ]]; then
  echo "The temporary signing subkey was not found." >&2
  exit 1
fi

key="$(gpg --batch --pinentry-mode loopback --passphrase "${passphrase}" \
  --armor --export-secret-subkeys "${subkey}!")"
GPG_KEY="${key}" GPG_PASSPHRASE="${passphrase}" GPG_KEY_ID="${subkey}" \
  scripts/release/sign.sh dist "${version}"

gpg --batch --armor --export "${primary}" >"${tmp}/public.asc"
scripts/release/verify.sh dist "${version}" "${tmp}/public.asc"

scripts/release/check-mirror.sh dist "${version}"
