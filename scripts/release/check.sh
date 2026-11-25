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
# Until TFP-93, the registry key does not contain the signing subkey.
gpg --batch --armor --export "${primary}" >"${tmp}/registry-without-subkey.asc"
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
absent_fingerprint="0000000000000000000000000000000000000000"

# The release workflow pins the fingerprint of the signing subkey. When the
# key from Vault does not have that fingerprint, the signature must stop.
if GPG_KEY="${key}" GPG_PASSPHRASE="${passphrase}" GPG_KEY_ID="${absent_fingerprint}" \
  scripts/release/sign.sh dist "${version}" 2>/dev/null; then
  echo "sign.sh signed with a fingerprint that is not the signing subkey." >&2
  exit 1
fi

GPG_KEY="${key}" GPG_PASSPHRASE="${passphrase}" GPG_KEY_ID="${subkey}" \
  scripts/release/sign.sh dist "${version}" "${tmp}/signing-key.asc"

# As in the release workflow until TFP-93: the registry key alone cannot
# verify the signature, and the key that sign.sh writes makes it pass.
if scripts/release/verify.sh dist "${version}" "${tmp}/registry-without-subkey.asc" >/dev/null 2>&1; then
  echo "The registry key without the signing subkey verified the signature." >&2
  exit 1
fi
cat "${tmp}/registry-without-subkey.asc" "${tmp}/signing-key.asc" >"${tmp}/keys.asc"
scripts/release/verify.sh dist "${version}" "${tmp}/keys.asc"

# After TFP-93, the registry key contains the signing subkey.
gpg --batch --armor --export "${primary}" >"${tmp}/registry.asc"
scripts/release/verify.sh dist "${version}" "${tmp}/registry.asc"

scripts/release/check-mirror.sh dist "${version}"
