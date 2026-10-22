# The recipes use bash features: pipefail, and [[ ]]. GNU Make 3.81, the make
# of macOS, ignores .SHELLFLAGS, so a recipe that needs pipefail also sets it.
SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

ACCEPTANCE_LOG ?= acceptance-tests.log

# Go counts a skipped test as a pass. A missing variable would thus give a
# green run that tests nothing, so testacc fails when a test skips. This test
# skips until it has a second staging user (TFP-36).
ALLOWED_SKIP := TestAccOrganizationPermissionsUserResource

.PHONY: testacc sweep release-check docs

# Run the acceptance tests. The environment variables that AGENTS.md lists must
# be set. The timeout applies to each package, not to the whole run.
testacc:
	set -o pipefail; TF_ACC=1 go test -v -count=1 -p 1 -timeout 40m -run '^TestAcc' ./internal/provider/... 2>&1 | tee $(ACCEPTANCE_LOG)
	@skipped=$$(grep -oE -- '--- SKIP: [^ ]+' $(ACCEPTANCE_LOG) | cut -d' ' -f3 | grep -vxF $(ALLOWED_SKIP) || true); \
	if [[ -n "$${skipped}" ]]; then \
		echo "These acceptance tests skipped:"; \
		echo "$${skipped}"; \
		exit 1; \
	fi

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
