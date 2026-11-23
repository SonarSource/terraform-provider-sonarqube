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

.PHONY: testacc testacc-gitlab gitlab-start gitlab-stop sweep release-check docs lint vulncheck

# The checks of the Build and pre-commit workflows, without the acceptance
# tests and the release check. CI runs each of them, so a green make check
# gives a green CI. A check that a tool of .mise.toml needs fails when the tool
# is not installed. Run mise install.
CHECKS := fmt-check tidy-check vet build test lint vulncheck docs-check examples-fmt-check pre-commit-check
TEST_FLAGS ?= -race

.PHONY: check $(CHECKS)

# Run all checks, also after a failure, and list the checks that failed at the
# end. Thus one run reports all problems.
check:
	@failed=(); \
	for c in $(CHECKS); do \
		echo "=== $${c}"; \
		$(MAKE) --no-print-directory "$${c}" || failed+=("$${c}"); \
	done; \
	if (( $${#failed[@]} > 0 )); then echo "=== These checks failed: $${failed[*]}"; exit 1; fi; \
	echo "=== All checks passed."

fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [[ -n "$${unformatted}" ]]; then \
		echo "These files are not gofmt-formatted. Run gofmt -w on them:"; \
		echo "$${unformatted}"; \
		exit 1; \
	fi

# The check compares the files before and after go mod tidy, not with the last
# commit, so it also works with changes that are not committed.
tidy-check:
	@before=$$(cat go.mod go.sum | shasum); \
	go mod tidy; \
	if [[ "$$(cat go.mod go.sum | shasum)" != "$${before}" ]]; then \
		echo "go mod tidy changed go.mod or go.sum. Review the changes and keep them."; \
		exit 1; \
	fi

vet:
	go vet ./...

build:
	go build ./...

# env -u TF_ACC: the acceptance tests do not start when the shell sets TF_ACC.
test:
	env -u TF_ACC go test $(TEST_FLAGS) ./...

# The check compares docs/ before and after make docs, not with the last
# commit, so it also works with changes that are not committed. The output of
# make docs shows only when it fails, because tfplugindocs logs each file.
docs-check:
	@before=$$(find docs -type f -exec shasum {} + | sort | shasum); \
	log=$$(mktemp); \
	$(MAKE) --no-print-directory docs >"$${log}" 2>&1 || { cat "$${log}"; rm -f "$${log}"; exit 1; }; \
	rm -f "$${log}"; \
	if [[ "$$(find docs -type f -exec shasum {} + | sort | shasum)" != "$${before}" ]]; then \
		echo "make docs changed docs/. docs/ is generated: a schema description, an example or a template changed, or a page was edited by hand."; \
		echo "Review the changes and keep them. To change a page, change its source. CONTRIBUTING.md tells where the text of each page comes from."; \
		git status --short docs/; \
		exit 1; \
	fi

examples-fmt-check:
	@terraform fmt -check -recursive examples || { echo "Run terraform fmt -recursive examples."; exit 1; }

pre-commit-check:
	pre-commit run --all-files

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

# Run the linters of .golangci.yml. .mise.toml gives the version.
lint:
	golangci-lint run ./...

# Report the known vulnerabilities that the code can reach.
vulncheck:
	govulncheck ./...
