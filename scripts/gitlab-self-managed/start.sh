#!/usr/bin/env bash
# Starts a temporary GitLab CE instance behind an ngrok HTTPS tunnel, for the
# acceptance tests that bind to GitLab Self-Managed. SonarQube Cloud must reach
# the GitLab API at a public HTTPS address, and the tunnel gives one.
#
# It needs Docker and NGROK_AUTHTOKEN. It writes the test variables to the file
# that GITLAB_ENV_FILE names (default: .gitlab-self-managed.env), as KEY=value
# lines that a shell can source and that GitHub Actions can add to GITHUB_ENV.
#
# Stop the instance with scripts/gitlab-self-managed/stop.sh.
set -Eeuo pipefail

# On a computer with the ngrok agent, the auth token of its configuration
# serves when NGROK_AUTHTOKEN is not set.
if [[ -z "${NGROK_AUTHTOKEN:-}" ]] && command -v ngrok >/dev/null; then
  ngrok_config=$(ngrok config check 2>/dev/null | sed -n 's/^Valid configuration file at //p')
  if [[ -n "${ngrok_config}" ]]; then
    NGROK_AUTHTOKEN=$(awk '/^ *authtoken:/ { print $2 }' "${ngrok_config}")
    export NGROK_AUTHTOKEN
  fi
fi
: "${NGROK_AUTHTOKEN:?Set NGROK_AUTHTOKEN to the auth token of an ngrok account.}"
env_file="${GITLAB_ENV_FILE:-.gitlab-self-managed.env}"

# The names of the containers, the network and the group. The group name
# starts with the prefix of the acceptance tests.
network=tf-acc-gitlab
gitlab=tf-acc-gitlab
ngrok=tf-acc-gitlab-ngrok
group=tf-acc-test-group
gitlab_image=gitlab/gitlab-ce:latest
ngrok_image=ngrok/ngrok:3-alpine
local_port=8929

diagnostics() {
  echo "::group::GitLab and ngrok diagnostics"
  docker ps -a --filter "name=${gitlab}" || true
  docker logs --tail 100 "${gitlab}" 2>&1 || true
  # The ngrok log can show the auth token in its arguments.
  docker logs --tail 50 "${ngrok}" 2>&1 | sed -E 's/(authtoken[^[:space:]]*[[:space:]]+)[^[:space:]]+/\1[REDACTED]/g' || true
  echo "::endgroup::"
}
trap diagnostics ERR

"$(dirname "$0")/stop.sh" >/dev/null 2>&1 || true
docker network create "${network}" >/dev/null

# The tunnel starts first, because GitLab needs its public address as the
# external URL. The agent sends the traffic to GitLab on the Docker network.
# The agent logs JSON, and the line that starts the tunnel holds its address.
docker run --detach --name "${ngrok}" --network "${network}" --env NGROK_AUTHTOKEN \
  "${ngrok_image}" http "${gitlab}:80" --log stdout --log-format json >/dev/null

url=""
for _ in $(seq 1 60); do
  url=$(docker logs "${ngrok}" 2>/dev/null |
    jq -rR 'fromjson? | .url? // empty | select(startswith("https://"))' | head -n 1 || true)
  [[ -n "${url}" ]] && break
  sleep 1
done
[[ -n "${url}" ]] || { echo "ngrok did not open an HTTPS tunnel. Check NGROK_AUTHTOKEN, and check that no other tunnel uses the account."; false; }
echo "Tunnel: ${url}"

# The configuration goes in an environment variable, so that nothing is
# mounted. Docker Desktop and Rancher Desktop do not share every host
# directory with their virtual machine.
# The image is the latest release. Docker does not pull a tag again when a copy
# exists, so --pull always keeps a local copy from going stale.
docker run --detach --pull always --name "${gitlab}" --hostname gitlab --network "${network}" \
  --shm-size 256m --publish "127.0.0.1:${local_port}:80" \
  --env GITLAB_OMNIBUS_CONFIG="external_url '${url}'
letsencrypt['enable'] = false
nginx['listen_port'] = 80
nginx['listen_https'] = false
nginx['proxy_set_headers'] = { 'X-Forwarded-Proto' => 'https', 'X-Forwarded-Ssl' => 'on' }
gitlab_rails['gitlab_shell_ssh_port'] = 2222" \
  "${gitlab_image}" >/dev/null

echo "Waiting for GitLab. The first start takes some minutes."
for _ in $(seq 1 120); do
  if curl --fail --silent --max-time 5 "http://127.0.0.1:${local_port}/help" >/dev/null 2>&1; then
    break
  fi
  if [[ "$(docker inspect --format '{{.State.Status}}' "${gitlab}")" != running ]]; then
    echo "The GitLab container stopped."
    false
  fi
  sleep 10
done
curl --fail --silent --show-error --max-time 10 "http://127.0.0.1:${local_port}/help" >/dev/null

# The root user owns the group and gives the two tokens. The second token lets
# a test change the token of a binding.
seed=$(docker exec "${gitlab}" gitlab-rails runner "
group = Group.find_or_create_by!(path: '${group}') do |g|
  g.name = '${group}'
  g.visibility_level = Gitlab::VisibilityLevel::PRIVATE
  g.organization = Organizations::Organization.default_organization
end
root = User.find_by_username!('root')
group.add_owner(root)
root.personal_access_tokens.where(name: ['tf-acc-first', 'tf-acc-second']).delete_all
first = root.personal_access_tokens.create!(name: 'tf-acc-first', scopes: [:api], expires_at: 30.days.from_now)
second = root.personal_access_tokens.create!(name: 'tf-acc-second', scopes: [:api], expires_at: 30.days.from_now)
puts('TF_ACC_SEED ' + { first: first.token, second: second.token }.to_json)
")
# The runner can print warnings before the result, so only the marked line
# counts.
seed=$(sed -n 's/^TF_ACC_SEED //p' <<<"${seed}" | tail -n 1)
first_token=$(jq -er '.first' <<<"${seed}")
second_token=$(jq -er '.second' <<<"${seed}")
unset seed
if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
  echo "::add-mask::${first_token}"
  echo "::add-mask::${second_token}"
fi

# Check the path that SonarQube Cloud takes: the API through the tunnel, with
# the token, on the group.
for _ in $(seq 1 30); do
  curl --fail --silent --max-time 10 --header "PRIVATE-TOKEN: ${first_token}" \
    "${url}/api/v4/groups/${group}" >/dev/null 2>&1 && break
  sleep 2
done
curl --fail --silent --show-error --max-time 10 --header "PRIVATE-TOKEN: ${first_token}" \
  "${url}/api/v4/groups/${group}" >/dev/null

umask 077
cat >"${env_file}" <<EOF
SONARQUBE_TEST_GITLAB_URL=${url}
SONARQUBE_TEST_GITLAB_GROUP=${group}
SONARQUBE_TEST_GITLAB_TOKEN=${first_token}
SONARQUBE_TEST_GITLAB_SECOND_TOKEN=${second_token}
EOF
# The image is the latest release, so a run says which one it used.
version=$(curl --silent --max-time 10 --header "PRIVATE-TOKEN: ${first_token}" \
  "${url}/api/v4/version" | jq -r '.version // "unknown"' 2>/dev/null || echo unknown)
echo "GitLab ${version} is ready at ${url}. The test variables are in ${env_file}."
