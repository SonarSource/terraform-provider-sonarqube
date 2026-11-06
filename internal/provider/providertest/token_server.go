package providertest

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

// The organization and the projects of TokenServer.
const (
	TokenOrganization   = "my-org"
	TokenOrganizationID = "00000000-0000-4000-8000-000000000001"
	FirstProjectKey     = "first-project"
	FirstProjectID      = "00000000-0000-4000-8000-0000000000a1"
	SecondProjectKey    = "second-project"
	SecondProjectID     = "00000000-0000-4000-8000-0000000000a2"
)

const (
	tokensPath = "/authentication/token-definitions"
	linksPath  = "/authentication/token-resource-links"
)

// TokenServer is a fake SonarQube Cloud that keeps scoped organization
// tokens, their resource links and two projects in memory. Set the failure
// fields before the first request.
type TokenServer struct {
	*httptest.Server
	t *testing.T

	// FailLinkCreate makes each link creation answer 500.
	FailLinkCreate bool
	// FailRevoke makes each token revocation answer 500.
	FailRevoke bool
	// LinksGone makes the link list answer 404, as for a token that was just revoked.
	LinksGone bool

	mu       sync.Mutex
	tokens   map[string]client.OrganizationToken
	links    map[string]client.TokenResourceLink
	projects []client.Project
	sequence int
}

func NewTokenServer(t *testing.T) *TokenServer {
	t.Helper()
	s := &TokenServer{t: t, tokens: map[string]client.OrganizationToken{}, links: map[string]client.TokenResourceLink{}, projects: []client.Project{
		{ID: FirstProjectID, Key: FirstProjectKey, OrganizationID: TokenOrganizationID},
		{ID: SecondProjectID, Key: SecondProjectKey, OrganizationID: TokenOrganizationID},
	}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.serve(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// ProviderConfig is a provider block that points at the server.
func (s *TokenServer) ProviderConfig() string {
	return fmt.Sprintf(`provider "sonarqube" {
 url = %q
 api_url = %q
 token = "test-token"
 product = "cloud"
}
`, s.URL, s.URL)
}

// AddToken stores a token and returns its ID. Without assignments, the token
// has the scan project permission only.
func (s *TokenServer) AddToken(name string, assignments ...client.TokenRoleAssignment) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(assignments) == 0 {
		assignments = []client.TokenRoleAssignment{{ResourceType: client.TokenResourceProject, Roles: []string{"scan"}}}
	}
	id := s.nextID()
	s.tokens[id] = client.OrganizationToken{ID: id, Name: name, OrganizationID: TokenOrganizationID, CreatedAt: "2026-01-01T00:00:00Z", RoleAssignments: assignments}
	return id
}

// AddLink links a token to a resource, also one that does not exist.
func (s *TokenServer) AddLink(tokenID, resourceType, resourceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID()
	s.links[id] = client.TokenResourceLink{ID: id, OrganizationTokenID: tokenID, ResourceType: resourceType, ResourceID: resourceID}
}

// LinkedResources returns the sorted resource IDs that the token links to.
func (s *TokenServer) LinkedResources(tokenID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for _, link := range s.links {
		if link.OrganizationTokenID == tokenID {
			ids = append(ids, link.ResourceID)
		}
	}
	slices.Sort(ids)
	return ids
}

// Unlink removes all links of a token, as an edit in the UI does.
func (s *TokenServer) Unlink(tokenID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unlink(tokenID)
}

// TokenNamed returns the token with the name.
func (s *TokenServer) TokenNamed(name string) (client.OrganizationToken, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, token := range s.tokens {
		if token.Name == name {
			return token, true
		}
	}
	return client.OrganizationToken{}, false
}

// AssertEmpty fails the test when a token or a link remains.
func (s *TokenServer) AssertEmpty(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tokens) != 0 || len(s.links) != 0 {
		t.Fatalf("%d tokens and %d links remain", len(s.tokens), len(s.links))
	}
}

func (s *TokenServer) nextID() string {
	s.sequence++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", s.sequence)
}

func (s *TokenServer) unlink(tokenID string) {
	for id, link := range s.links {
		if link.OrganizationTokenID == tokenID {
			delete(s.links, id)
		}
	}
}

func (s *TokenServer) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/organizations/organizations":
		fmt.Fprintf(w, `[{"key":%q,"uuidV4":%q}]`, TokenOrganization, TokenOrganizationID)
	case r.URL.Path == "/projects/projects":
		s.listProjects(w, r)
	case strings.HasPrefix(r.URL.Path, tokensPath):
		s.serveTokens(w, r)
	case strings.HasPrefix(r.URL.Path, linksPath):
		s.serveLinks(w, r)
	default:
		s.unexpected(w, r)
	}
}

func (s *TokenServer) unexpected(w http.ResponseWriter, r *http.Request) {
	s.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	w.WriteHeader(http.StatusNotFound)
}

// listProjects matches the comma-separated keys or ids, as the server does.
func (s *TokenServer) listProjects(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	keys, ids := strings.Split(query.Get("keys"), ","), strings.Split(query.Get("ids"), ",")
	list := []client.Project{}
	for _, project := range s.projects {
		if project.OrganizationID == query.Get("organizationIds") && (slices.Contains(keys, project.Key) || slices.Contains(ids, project.ID)) {
			list = append(list, project)
		}
	}
	writePage(w, "projects", list)
}

func (s *TokenServer) serveTokens(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == tokensPath && r.Method == http.MethodPost:
		s.createToken(w, r)
	case r.URL.Path == tokensPath && r.Method == http.MethodGet:
		writePage(w, "tokenDefinitions", slices.Collect(maps.Values(s.tokens)))
	case r.Method == http.MethodDelete && s.FailRevoke:
		w.WriteHeader(http.StatusInternalServerError)
	case r.Method == http.MethodDelete:
		id := strings.TrimPrefix(r.URL.Path, tokensPath+"/")
		delete(s.tokens, id)
		s.unlink(id)
		w.WriteHeader(http.StatusNoContent)
	default:
		s.unexpected(w, r)
	}
}

func (s *TokenServer) createToken(w http.ResponseWriter, r *http.Request) {
	var request client.OrganizationTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.t.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	for _, token := range s.tokens {
		if token.Name == request.Name {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	id := s.nextID()
	token := client.OrganizationToken{ID: id, Name: request.Name, OrganizationID: request.OrganizationID, Description: request.Description, ExpiresAt: request.ExpiresAt, CreatedAt: "2026-01-01T00:00:00.123457Z", RoleAssignments: request.RoleAssignments, AutoGrantProjectPermissions: request.AutoGrantProjectPermissions}
	s.tokens[id] = token
	// Create returns nanoseconds; the list rounds them to microseconds.
	token.CreatedAt = "2026-01-01T00:00:00.123456789Z"
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(client.CreatedOrganizationToken{OrganizationToken: token, Token: fmt.Sprintf("sqco_test-secret-%d", s.sequence)})
}

func (s *TokenServer) serveLinks(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == linksPath && r.Method == http.MethodPost && s.FailLinkCreate:
		w.WriteHeader(http.StatusInternalServerError)
	case r.URL.Path == linksPath && r.Method == http.MethodPost:
		s.createLink(w, r)
	case r.URL.Path == linksPath && r.Method == http.MethodGet:
		s.listLinks(w, r.URL.Query().Get("organizationTokenId"))
	case r.Method == http.MethodDelete:
		delete(s.links, strings.TrimPrefix(r.URL.Path, linksPath+"/"))
		w.WriteHeader(http.StatusNoContent)
	default:
		s.unexpected(w, r)
	}
}

// createLink returns the existing link for a repeated request, as the server does.
func (s *TokenServer) createLink(w http.ResponseWriter, r *http.Request) {
	var request client.TokenResourceLink
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.t.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if request.ResourceType != client.TokenResourceProject || !slices.ContainsFunc(s.projects, func(p client.Project) bool { return p.ID == request.ResourceID }) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	for _, link := range s.links {
		if link.OrganizationTokenID == request.OrganizationTokenID && link.ResourceID == request.ResourceID {
			json.NewEncoder(w).Encode(link)
			return
		}
	}
	request.ID = s.nextID()
	s.links[request.ID] = request
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(request)
}

func (s *TokenServer) listLinks(w http.ResponseWriter, tokenID string) {
	if _, found := s.tokens[tokenID]; !found || s.LinksGone {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Token not found"}`)
		return
	}
	var list []client.TokenResourceLink
	for _, link := range s.links {
		if link.OrganizationTokenID == tokenID {
			list = append(list, link)
		}
	}
	writePage(w, "tokenResourceLinks", list)
}

func writePage[T any](w http.ResponseWriter, collection string, items []T) {
	if items == nil {
		items = []T{}
	}
	json.NewEncoder(w).Encode(map[string]any{collection: items, "page": map[string]int{"total": len(items)}})
}
