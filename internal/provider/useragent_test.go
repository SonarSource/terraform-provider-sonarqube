package provider

import (
	"strings"
	"testing"
)

// printableASCII reports whether value has only the characters that
// sanitizeUserAgent keeps, which are all valid in a header field value.
func printableASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < ' ' || value[i] > '~' {
			return false
		}
	}
	return true
}

func TestUserAgent(t *testing.T) {
	t.Parallel()

	const base = "Terraform/1.9.8 (+https://www.terraform.io) sonarqube-terraform/1.2.3"

	cases := []struct {
		name             string
		terraformVersion string
		appended         string
		want             string
	}{
		{"no appended value", "1.9.8", "", base},
		{"appended product", "1.9.8", "my-ci/2.0", base + " my-ci/2.0"},
		{"appended product and comment", "1.9.8", "my-ci/2.0 (team-a)", base + " my-ci/2.0 (team-a)"},
		{"trims and collapses whitespace", "1.9.8", "  my-ci/2.0 \t\t (team-a)  ", base + " my-ci/2.0 (team-a)"},
		{"spaces only", "1.9.8", "   \t ", base},
		{"line breaks", "1.9.8", "my-ci/2.0\r\nX-Injected: yes", base + " my-ci/2.0 X-Injected: yes"},
		{"control characters", "1.9.8", "my\x00-ci\x7f/2.0", base + " my-ci/2.0"},
		{"non-ASCII", "1.9.8", "mój-ci/2.0 ✓", base + " mj-ci/2.0"},
		{"non-ASCII only", "1.9.8", "✓✓", base},
		{"Unicode whitespace", "1.9.8", "my-ci/2.0\u00a0(team-a)\u2028x", base + " my-ci/2.0 (team-a) x"},
		{"unknown Terraform version", "", "", "Terraform/unknown (+https://www.terraform.io) sonarqube-terraform/1.2.3"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := userAgent(tc.terraformVersion, "1.2.3", tc.appended)
			if got != tc.want {
				t.Errorf("userAgent() = %q, want %q", got, tc.want)
			}
			if !printableASCII(got) {
				t.Errorf("userAgent() = %q is not a valid header field value", got)
			}
		})
	}
}

// A long value is cut, so that the header does not exceed the limits of a
// proxy and make each request fail.
func TestUserAgentCutsALongValue(t *testing.T) {
	t.Parallel()

	base := userAgent("1.9.8", "1.2.3", "")
	got := userAgent("1.9.8", "1.2.3", strings.Repeat("product/1.0 ", 500))

	appended := strings.TrimPrefix(got, base+" ")
	if len(appended) == 0 || len(appended) > maxAppendedUserAgent {
		t.Errorf("appended text has %d characters, want 1 to %d", len(appended), maxAppendedUserAgent)
	}
	if !strings.HasPrefix(appended, "product/1.0 product/1.0") {
		t.Errorf("appended text = %q, want the start of the value", appended)
	}
	if strings.HasSuffix(appended, " ") {
		t.Errorf("appended text = %q ends with a space", appended)
	}
}

func TestUserAgentOfAnUnknownProviderVersion(t *testing.T) {
	t.Parallel()

	want := "Terraform/1.9.8 (+https://www.terraform.io) sonarqube-terraform/unknown"
	if got := userAgent("1.9.8", "", ""); got != want {
		t.Errorf("userAgent() = %q, want %q", got, want)
	}
}

func TestConfigureSetsTheUserAgent(t *testing.T) {
	resp := configure(t, map[string]string{EnvToken: "a-token", envAppendUserAgent: "my-ci/2.0"}, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	c := configuredClient(t, resp)
	want := "Terraform/" + testTerraformVersion + " (+https://www.terraform.io) sonarqube-terraform/test my-ci/2.0"
	if got := c.UserAgent(); got != want {
		t.Errorf("UserAgent() = %q, want %q", got, want)
	}
}
