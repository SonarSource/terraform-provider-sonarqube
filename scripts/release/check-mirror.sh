#!/usr/bin/env bash
# Installs the provider from a local filesystem mirror, as re-service-config
# does.
#
# Usage: check-mirror.sh <dist dir> <version>
set -euo pipefail

dist="$1"
version="$2"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

mirror="${tmp}/mirror"
mkdir -p "${mirror}/registry.terraform.io/sonarsource/sonarqube"
cp "${dist}"/terraform-provider-sonarqube_"${version}"_*.zip \
  "${mirror}/registry.terraform.io/sonarsource/sonarqube/"

cat >"${tmp}/terraform.rc" <<EOF
provider_installation {
  filesystem_mirror {
    path = "${mirror}"
  }
}
EOF
export TF_CLI_CONFIG_FILE="${tmp}/terraform.rc"

mkdir "${tmp}/config"
cat >"${tmp}/config/main.tf" <<EOF
terraform {
  required_providers {
    sonarqube = {
      source  = "sonarsource/sonarqube"
      version = "${version}"
    }
  }
}
EOF

terraform -chdir="${tmp}/config" providers lock -fs-mirror="${mirror}" \
  -platform=linux_amd64 -platform=linux_arm64 \
  -platform=darwin_amd64 -platform=darwin_arm64 \
  -platform=windows_amd64
terraform -chdir="${tmp}/config" init -lockfile=readonly -input=false
