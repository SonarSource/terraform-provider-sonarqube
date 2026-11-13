package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(srv *httptest.Server) *Client {
	return newTestClientWithUserAgent(srv, "")
}

func newTestClientWithUserAgent(srv *httptest.Server, userAgent string) *Client {
	// The test server answers on one host, so both surfaces point at it. A
	// derived api host would name a host that does not exist.
	return New(Config{
		URL:        srv.URL,
		APIURL:     srv.URL,
		Token:      "test-token",
		Product:    ProductCloud,
		UserAgent:  userAgent,
		HTTPClient: srv.Client(),
	})
}

// DeriveAPIURL replaces a sub-domain instead of prefixing one, because the api
// host sits beside the web application rather than below it.
func TestDeriveAPIURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		url  string
		want string
	}{
		{"production", "https://sonarcloud.io", "https://api.sonarcloud.io"},
		{"united states", "https://sonarqube.us", "https://api.sonarqube.us"},
		{"development instance", "https://dev.example.io", "https://api.example.io"},
		{"keeps the port", "http://localhost:9000", "http://api.localhost:9000"},
		{"leaves an address by number alone", "http://127.0.0.1:9000", "http://127.0.0.1:9000"},
		// A hostname may end in a dot. The empty last label must not make a
		// host of two labels look like one of three.
		{"absolute hostname", "https://sonarcloud.io.", "https://api.sonarcloud.io."},
		{"absolute hostname with a sub-domain", "https://dev.example.io.", "https://api.example.io."},
		{"address with no scheme comes back unchanged", "sonarcloud.io", "sonarcloud.io"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := DeriveAPIURL(tc.url); got != tc.want {
				t.Errorf("DeriveAPIURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

func TestNewDerivesTheAPIURL(t *testing.T) {
	t.Parallel()

	c := New(Config{URL: "https://dev.example.io/"})

	if got, want := c.APIURL(), "https://api.example.io"; got != want {
		t.Errorf("APIURL() = %q, want %q", got, want)
	}
}

func TestNewKeepsAnExplicitAPIURL(t *testing.T) {
	t.Parallel()

	c := New(Config{URL: "https://dev.example.io", APIURL: "https://api.example.com/"})

	if got, want := c.APIURL(), "https://api.example.com"; got != want {
		t.Errorf("APIURL() = %q, want %q", got, want)
	}
}

func TestNewTrimsTrailingSlash(t *testing.T) {
	t.Parallel()

	c := New(Config{URL: "https://sonarcloud.io/", Product: ProductCloud})

	if got, want := c.URL(), "https://sonarcloud.io"; got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
}

func TestIsCloud(t *testing.T) {
	t.Parallel()

	cases := map[Product]bool{
		ProductCloud:  true,
		ProductServer: false,
	}

	for product, want := range cases {
		c := New(Config{Product: product})
		if got := c.IsCloud(); got != want {
			t.Errorf("IsCloud() with product %q = %v, want %v", product, got, want)
		}
	}
}

// The web service authenticates the token as the basic-auth user name, with an
// empty password. Web API v2 takes a bearer token instead.
func TestGetSendsTheToken(t *testing.T) {
	t.Parallel()

	var gotUser, gotPassword string
	var gotOK bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPassword, gotOK = r.BasicAuth()
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if err := newTestClient(srv).get(t.Context(), "/api/anything", nil, nil); err != nil {
		t.Fatalf("get() returned %v", err)
	}

	if !gotOK {
		t.Fatal("the request carried no basic-auth credentials")
	}
	if gotUser != "test-token" {
		t.Errorf("user name = %q, want the token", gotUser)
	}
	if gotPassword != "" {
		t.Errorf("password = %q, want an empty password", gotPassword)
	}
}

func TestDoReportsWebServiceErrors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errors":[{"msg":"first problem"},{"msg":"second problem"}]}`))
	}))
	defer srv.Close()

	err := newTestClient(srv).get(t.Context(), "/api/anything", nil, nil)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("get() returned %v, want an *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}
	if got, want := apiErr.Error(), "status 400: first problem; second problem"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// The REST API reports a failure in another shape, and both shapes share one
// reader.
func TestDoReportsRestAPIErrors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"insufficient privileges"}`))
	}))
	defer srv.Close()

	err := newTestClient(srv).get(t.Context(), "/api/anything", nil, nil)

	if got, want := err.Error(), "status 403: insufficient privileges"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// A body in neither known shape must still reach the user.
func TestDoKeepsAnUnknownErrorBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("the gateway is unwell"))
	}))
	defer srv.Close()

	err := newTestClient(srv).get(t.Context(), "/api/anything", nil, nil)

	if got, want := err.Error(), "status 500: the gateway is unwell"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestDoMapsNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errors":[{"msg":"no such thing"}]}`))
	}))
	defer srv.Close()

	err := newTestClient(srv).get(t.Context(), "/api/anything", nil, nil)

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("get() returned %v, want an error that matches ErrNotFound", err)
	}
}

func TestProduct(t *testing.T) {
	t.Parallel()

	if got := New(Config{Product: ProductServer}).Product(); got != ProductServer {
		t.Errorf("Product() = %q, want %q", got, ProductServer)
	}
}

// A success status can still carry a body the caller cannot decode.
func TestDoReportsAnUndecodableBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("this is not JSON"))
	}))
	defer srv.Close()

	var out struct{}
	err := newTestClient(srv).get(t.Context(), "/api/anything", nil, &out)

	if err == nil || !strings.Contains(err.Error(), "cannot decode the response body") {
		t.Errorf("get() returned %v, want a decoding error", err)
	}
}

func TestAPIGetSendsABearerToken(t *testing.T) {
	t.Parallel()

	var gotAuthorization string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	if err := newTestClient(srv).apiGet(t.Context(), "/anything", nil, nil); err != nil {
		t.Fatalf("apiGet() returned %v", err)
	}

	if got, want := gotAuthorization, "Bearer test-token"; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

// ValidateURL is what keeps an address with no scheme out of the client: it
// parses without an error and leaves the host empty, so every later request
// would fail with a message about the protocol scheme instead.
func TestValidateURL(t *testing.T) {
	t.Parallel()

	valid := []string{"https://sonarcloud.io", "http://localhost:9000", "https://dev.example.io/"}
	invalid := map[string]string{
		"sonarcloud.io":       "no http or https scheme",
		"":                    "no http or https scheme",
		"ftp://sonarcloud.io": "no http or https scheme",
		"https://":            "names no host",
		"https://:9000":       "names no host",
	}

	for _, value := range valid {
		if err := ValidateURL(value); err != nil {
			t.Errorf("ValidateURL(%q) = %v, want no error", value, err)
		}
	}
	for value, want := range invalid {
		err := ValidateURL(value)
		if err == nil {
			t.Errorf("ValidateURL(%q) reported no error, want one", value)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateURL(%q) = %v, want an error about %q", value, err, want)
		}
	}
}

// Both API surfaces send the User-Agent header.
func TestRequestsSendTheUserAgent(t *testing.T) {
	t.Parallel()

	const configured = "Terraform/1.9.8 (+https://www.terraform.io) sonarqube-terraform/1.2.3"

	cases := []struct {
		name      string
		userAgent string
		want      string
	}{
		{"configured", configured, configured},
		{"default", "", "sonarqube-terraform"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = append(got, r.UserAgent())
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			c := newTestClientWithUserAgent(srv, tc.userAgent)

			if err := c.get(context.Background(), "/api/web", nil, nil); err != nil {
				t.Fatalf("get: %v", err)
			}
			if err := c.apiGet(context.Background(), "/api-v2", nil, nil); err != nil {
				t.Fatalf("apiGet: %v", err)
			}

			if len(got) != 2 || got[0] != tc.want || got[1] != tc.want {
				t.Errorf("User-Agent headers = %q, want %q on both requests", got, tc.want)
			}
		})
	}
}
