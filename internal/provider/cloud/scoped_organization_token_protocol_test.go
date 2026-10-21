package cloud_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/acctest"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestScopedOrganizationTokenTerraformLifecycle(t *testing.T) {
	var mu sync.Mutex
	tokens := map[string]client.OrganizationToken{}
	sequence := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		const collection = "/authentication/token-definitions"
		switch {
		case r.URL.Path == "/organizations/organizations":
			fmt.Fprint(w, `[{"key":"my-org","uuidV4":"00000000-0000-4000-8000-000000000001"}]`)
		case r.URL.Path == collection && r.Method == http.MethodPost:
			var request client.OrganizationTokenRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if tokenNameExists(tokens, request.Name) {
				w.WriteHeader(400)
				return
			}
			sequence++
			id := fmt.Sprintf("00000000-0000-4000-8000-%012d", sequence)
			token := client.OrganizationToken{ID: id, Name: request.Name, OrganizationID: request.OrganizationID, Description: request.Description, ExpiresAt: request.ExpiresAt, CreatedAt: "2026-01-01T00:00:00.123456Z", RoleAssignments: request.RoleAssignments, AutoGrantProjectPermissions: request.AutoGrantProjectPermissions}
			tokens[id] = token
			// Create returns nanoseconds; persisted metadata has microsecond precision.
			token.CreatedAt = "2026-01-01T00:00:00.123456789Z"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(client.CreatedOrganizationToken{OrganizationToken: token, Token: fmt.Sprintf("sqco_test-secret-%d", sequence)})
		case r.URL.Path == collection && r.Method == http.MethodGet:
			list := []client.OrganizationToken{}
			for _, token := range tokens {
				list = append(list, token)
			}
			json.NewEncoder(w).Encode(map[string]any{"tokenDefinitions": list, "page": map[string]int{"total": len(list)}})
		case strings.HasPrefix(r.URL.Path, collection+"/") && r.Method == http.MethodDelete:
			delete(tokens, strings.TrimPrefix(r.URL.Path, collection+"/"))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	config := fmt.Sprintf(`provider "sonarqube" {
 url = %q
 api_url = %q
 token = "test-token"
 product = "cloud"
}
`, srv.URL, srv.URL)
	var id string
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories, Steps: scopedTokenSteps("my-org", "test-token", config, &id)})
	mu.Lock()
	defer mu.Unlock()
	if len(tokens) != 0 {
		t.Fatal("destroy left tokens behind")
	}
}

func tokenNameExists(tokens map[string]client.OrganizationToken, name string) bool {
	for _, token := range tokens {
		if token.Name == name {
			return true
		}
	}
	return false
}
