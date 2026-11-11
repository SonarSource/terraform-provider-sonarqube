#!/usr/bin/env bash
# Stops the instance and the tunnel of start.sh, and removes their data.
set -uo pipefail

docker rm --force --volumes tf-acc-gitlab tf-acc-gitlab-ngrok >/dev/null 2>&1
docker network rm tf-acc-gitlab >/dev/null 2>&1
rm -f "${GITLAB_ENV_FILE:-.gitlab-self-managed.env}"
exit 0
