#!/usr/bin/env bash
# Signs the checksum file with a binary, detached signature. GoReleaser does
# not sign reliably with a key that has a passphrase.
#
# Usage: sign.sh <dist dir> <version>
# Environment:
#   GPG_KEY         the armored secret key
#   GPG_PASSPHRASE  the passphrase of the key
#   GPG_KEY_ID      the id of the signing (sub)key
set -euo pipefail

dist="$1"
version="$2"
: "${GPG_KEY:?}" "${GPG_PASSPHRASE:?}" "${GPG_KEY_ID:?}"

sums="${dist}/terraform-provider-sonarqube_${version}_SHA256SUMS"

GNUPGHOME="$(mktemp -d)"
export GNUPGHOME
chmod 700 "${GNUPGHOME}"
trap 'gpgconf --kill gpg-agent 2>/dev/null || true; rm -rf "${GNUPGHOME}"' EXIT

printf '%s\n' "${GPG_KEY}" | gpg --batch --quiet --import
# The "!" makes gpg use this exact subkey, not the newest signing subkey.
printf '%s\n' "${GPG_PASSPHRASE}" | gpg --batch --yes --pinentry-mode loopback \
  --passphrase-fd 0 --local-user "${GPG_KEY_ID}!" \
  --detach-sign --output "${sums}.sig" "${sums}"
