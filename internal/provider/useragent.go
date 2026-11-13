package provider

import (
	"cmp"
	"strings"
	"unicode"
)

// maxAppendedUserAgent limits the appended text, so that the header stays well
// below the header size limits of proxies and load balancers.
const maxAppendedUserAgent = 256

// userAgent returns the User-Agent header of the requests of the provider.
func userAgent(terraformVersion, providerVersion, appended string) string {
	ua := "Terraform/" + cmp.Or(terraformVersion, "unknown") +
		" (+https://www.terraform.io) sonarqube-terraform/" + cmp.Or(providerVersion, "unknown")
	if extra := sanitizeUserAgent(appended); extra != "" {
		ua += " " + extra
	}
	return ua
}

// sanitizeUserAgent keeps only printable ASCII, so that the value is always a
// valid header field value. Each group of whitespace becomes one space. The
// result has at most maxAppendedUserAgent characters.
func sanitizeUserAgent(value string) string {
	printable := strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case r > ' ' && r < 0x7f:
			return r
		}
		return -1
	}, value)
	clean := strings.Join(strings.Fields(printable), " ")
	if len(clean) > maxAppendedUserAgent {
		clean = strings.TrimSpace(clean[:maxAppendedUserAgent])
	}
	return clean
}
