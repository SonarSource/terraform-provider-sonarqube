package configure

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

const subject = "The sonarqube_test resource manages a test"

func newClient(token string, product client.Product) *client.Client {
	return client.New(client.Config{URL: "https://sonarcloud.example", Token: token, Product: product})
}

func TestCloudClient(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		providerData any
		accept       bool
		wantClient   bool
		wantError    string
	}{
		{name: "not configured yet", providerData: nil},
		{name: "unexpected data", providerData: "text", wantError: "Unexpected provider data"},
		{name: "SonarQube Server", providerData: newClient("test-token", client.ProductServer), wantError: "This needs SonarQube Cloud"},
		{name: "user token", providerData: newClient("test-token", client.ProductCloud), wantClient: true},
		{name: "scoped token refused", providerData: newClient("sqco_test-token", client.ProductCloud), wantError: "This needs a user token"},
		{name: "scoped token accepted", providerData: newClient("sqco_test-token", client.ProductCloud), accept: true, wantClient: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			get := CloudClient
			if tc.accept {
				get = CloudClientAcceptingScopedToken
			}
			c, diagnostics := get(tc.providerData, subject)
			if (c != nil) != tc.wantClient {
				t.Errorf("client = %v, want a client: %v", c, tc.wantClient)
			}
			checkError(t, diagnostics, tc.wantError)
		})
	}
}

// checkError expects no error when want is empty, and else one error with the
// summary want. The detail of a refusal starts with the subject.
func checkError(t *testing.T, diagnostics diag.Diagnostics, want string) {
	t.Helper()

	if want == "" {
		if diagnostics.HasError() {
			t.Fatalf("diagnostics = %v", diagnostics)
		}
		return
	}
	errs := diagnostics.Errors()
	if len(errs) != 1 || errs[0].Summary() != want {
		t.Fatalf("diagnostics = %v, want %q", diagnostics, want)
	}
	if want != "Unexpected provider data" && !strings.HasPrefix(errs[0].Detail(), subject) {
		t.Errorf("detail = %q, want it to start with the subject", errs[0].Detail())
	}
}
