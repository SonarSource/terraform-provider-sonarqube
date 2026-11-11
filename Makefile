# The recipes use bash features: pipefail, and [[ ]]. GNU Make 3.81, the make
# of macOS, ignores .SHELLFLAGS, so a recipe that needs pipefail also sets it.
SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

ACCEPTANCE_LOG ?= acceptance-tests.log

# Go counts a skipped test as a pass. A missing variable would thus give a
# green run that tests nothing, so testacc fails when a test skips. These tests
# can skip: the first until it has a second staging user (TFP-36), the second
# when no enterprise license is set, because each run uses up one license.
ALLOWED_SKIP := TestAccOrganizationPermissionsUserResource TestAccEnterpriseResource

# The tests that bind to GitLab Self-Managed need an instance that
# scripts/gitlab-self-managed/start.sh starts. testacc leaves them out, and
# testacc-gitlab runs only them. No test of testacc-gitlab may skip.
GITLAB_TESTS := ^TestAccGitLabSelfManaged
GITLAB_ENV_FILE ?= .gitlab-self-managed.env

.PHONY: testacc testacc-gitlab gitlab-start gitlab-stop sweep release-check docs

# Run the acceptance tests. The environment variables that CONTRIBUTING.md
# lists must be set. The timeout applies to each package, not to the whole run.
testacc:
	set -o pipefail; TF_ACC=1 go test -v -count=1 -p 1 -timeout 40m -run '^TestAcc' -skip '$(GITLAB_TESTS)' ./internal/provider/... 2>&1 | tee $(ACCEPTANCE_LOG)
	@$(MAKE) --no-print-directory check-skips SKIPS_ALLOWED='$(ALLOWED_SKIP)'

# Run the acceptance tests of GitLab Self-Managed. With GITLAB_ENV_FILE
# present, its variables are used.
testacc-gitlab:
	set -o pipefail; if [[ -f '$(GITLAB_ENV_FILE)' ]]; then set -a; source '$(GITLAB_ENV_FILE)'; set +a; fi; \
		TF_ACC=1 go test -v -count=1 -p 1 -timeout 40m -run '$(GITLAB_TESTS)' ./internal/provider/... 2>&1 | tee $(ACCEPTANCE_LOG)
	@$(MAKE) --no-print-directory check-skips SKIPS_ALLOWED=
	@# A pattern that matches no test gives no SKIP line, so the run must
	@# show that a test passed.
	@grep -q '^--- PASS: TestAccGitLabSelfManaged' $(ACCEPTANCE_LOG) || { echo "No GitLab Self-Managed test ran."; exit 1; }

# Fail when a test in ACCEPTANCE_LOG skipped and SKIPS_ALLOWED does not name it.
# A test that a scoped organization token cannot run skips on purpose. The
# line before its SKIP line has the reason of acctest.SkipWithSOT.
.PHONY: check-skips
check-skips:
	@skipped=$$(awk '/^ *--- SKIP: / && prev !~ /SONARQUBE_TEST_SOT_TOKEN is set/ { print $$3 } { prev = $$0 }' $(ACCEPTANCE_LOG) | grep -vxF $(if $(SKIPS_ALLOWED),$(addprefix -e ,$(SKIPS_ALLOWED)),-e '') || true); \
	if [[ -n "$${skipped}" ]]; then \
		echo "These acceptance tests skipped:"; \
		echo "$${skipped}"; \
		exit 1; \
	fi

# Start and stop a temporary GitLab Self-Managed instance for testacc-gitlab.
# It needs Docker, and an ngrok auth token in NGROK_AUTHTOKEN or in the
# configuration of the ngrok agent.
gitlab-start:
	GITLAB_ENV_FILE='$(GITLAB_ENV_FILE)' scripts/gitlab-self-managed/start.sh

gitlab-stop:
	GITLAB_ENV_FILE='$(GITLAB_ENV_FILE)' scripts/gitlab-self-managed/stop.sh

# Delete what an earlier acceptance run left. Run it on one package only,
# because the sweepers are the same in each package that imports acctest.
sweep:
	TF_ACC=1 go test ./internal/provider/cloud -count=1 -v -sweep=all -sweep-allow-failures

# Build a snapshot release, sign it with a temporary key, and install it from a
# local mirror. It needs goreleaser, gpg, jq, shasum and terraform.
release-check:
	scripts/release/check.sh

# Generate docs/ from the schemas, examples/ and templates/, and check the pages
# for the Terraform Registry. Do not edit docs/ by hand. It needs terraform.
docs:
	go tool tfplugindocs generate --provider-name sonarqube --rendered-provider-name SonarQube
	go tool tfplugindocs validate --provider-name sonarqube
