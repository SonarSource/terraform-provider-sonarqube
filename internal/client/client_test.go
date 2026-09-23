package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(srv *httptest.Server) *Client {
	return New(Config{
		URL:        srv.URL,
		Token:      "test-token",
		Product:    ProductCloud,
		HTTPClient: srv.Client(),
	})
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
// empty password. Pin that, because it differs from the REST API.
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
