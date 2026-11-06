package acctest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
)

// setSOTMode sets the package state that init sets from the environment.
func setSOTMode(t *testing.T, sot, user string) {
	t.Helper()
	oldSOT, oldUser := sotToken, userToken
	sotToken, userToken = sot, user
	t.Cleanup(func() { sotToken, userToken = oldSOT, oldUser })
}

// In SOT mode, Client sends the user token, and the provider token is the SOT.
func TestSOTModeClientKeepsUserToken(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		fmt.Fprint(w, `[{"key":"my-org","uuidV4":"org"}]`)
	}))
	defer srv.Close()
	t.Setenv(provider.EnvURL, srv.URL)
	t.Setenv(provider.EnvAPIURL, srv.URL)
	t.Setenv(provider.EnvToken, "sqco_test-token")
	setSOTMode(t, "sqco_test-token", "test-user-token")
	if _, err := Client().GetOrganization(t.Context(), "my-org"); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer test-user-token" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestSkipWithSOT(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sot, user string
		skip      bool
	}{
		{"user token only", "", "", false},
		{"scoped organization token", "sqco_test-token", "test-user-token", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setSOTMode(t, tc.sot, tc.user)
			var skipped bool
			t.Run("test", func(t *testing.T) {
				defer func() { skipped = t.Skipped() }()
				SkipWithSOT(t, SOTRefused)
			})
			if skipped != tc.skip {
				t.Fatalf("skipped = %v", skipped)
			}
		})
	}
}
