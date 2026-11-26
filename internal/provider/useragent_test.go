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

	cases := []struct {
		terraformVersion, providerVersion, want string
	}{
		{"1.9.8", "1.2.3", "Terraform/1.9.8 (+https://www.terraform.io) sonarqube-terraform/1.2.3"},
		{"", "1.2.3", "Terraform/unknown (+https://www.terraform.io) sonarqube-terraform/1.2.3"},
		{"1.9.8", "", "Terraform/1.9.8 (+https://www.terraform.io) sonarqube-terraform/unknown"},
	}
	for _, tc := range cases {
		if got := userAgent(tc.terraformVersion, tc.providerVersion); got != tc.want {
			t.Errorf("userAgent(%q, %q) = %q, want %q", tc.terraformVersion, tc.providerVersion, got, tc.want)
		}
	}
}

func TestSanitizeUserAgent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		appended string
		want     string
	}{
		{"empty", "", ""},
		{"product", "my-ci/2.0", "my-ci/2.0"},
		{"product and comment", "my-ci/2.0 (team-a)", "my-ci/2.0 (team-a)"},
		{"trims and collapses whitespace", "  my-ci/2.0 \t\t (team-a)  ", "my-ci/2.0 (team-a)"},
		{"spaces only", "   \t ", ""},
		{"line breaks", "my-ci/2.0\r\nX-Injected: yes", "my-ci/2.0 X-Injected: yes"},
		{"control characters", "my\x00-ci\x7f/2.0", "my-ci/2.0"},
		{"non-ASCII", "mój-ci/2.0 ✓", "mj-ci/2.0"},
		{"non-ASCII only", "✓✓", ""},
		{"Unicode whitespace", "my-ci/2.0\u00a0(team-a)\u2028x", "my-ci/2.0 (team-a) x"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := sanitizeUserAgent(tc.appended)
			if got != tc.want {
				t.Errorf("sanitizeUserAgent() = %q, want %q", got, tc.want)
			}
			if !printableASCII(got) {
				t.Errorf("sanitizeUserAgent() = %q is not a valid header field value", got)
			}
		})
	}
}

// A long value is cut, so that the header does not exceed the limits of a
// proxy and make each request fail.
func TestSanitizeUserAgentCutsALongValue(t *testing.T) {
	t.Parallel()

	got := sanitizeUserAgent(strings.Repeat("product/1.0 ", 500))

	if len(got) == 0 || len(got) > maxAppendedUserAgent {
		t.Errorf("appended text has %d characters, want 1 to %d", len(got), maxAppendedUserAgent)
	}
	if !strings.HasPrefix(got, "product/1.0 product/1.0") {
		t.Errorf("appended text = %q, want the start of the value", got)
	}
	if strings.HasSuffix(got, " ") {
		t.Errorf("appended text = %q ends with a space", got)
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
