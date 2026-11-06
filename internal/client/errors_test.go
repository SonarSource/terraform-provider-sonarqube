package client

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

// TestAPIErrorWithoutDetail covers a failure that carries neither a known
// message shape nor a body, which leaves the status as the only information.
func TestAPIErrorWithoutDetail(t *testing.T) {
	t.Parallel()

	err := newAPIError(503, nil)

	if got, want := err.Error(), "unexpected status 503 from the API"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestIsTransient(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		err  error
		want bool
	}{
		"500":                  {&APIError{StatusCode: http.StatusInternalServerError}, true},
		"503":                  {&APIError{StatusCode: http.StatusServiceUnavailable}, true},
		"409":                  {&APIError{StatusCode: http.StatusConflict}, true},
		"429":                  {&APIError{StatusCode: http.StatusTooManyRequests}, true},
		"timeout":              {&url.Error{Op: "Post", URL: "u", Err: timeoutError{}}, true},
		"connection reset":     {&url.Error{Op: "Post", URL: "u", Err: &net.OpError{Op: "read", Err: syscall.ECONNRESET}}, true},
		"connection closed":    {&url.Error{Op: "Post", URL: "u", Err: io.EOF}, true},
		"400":                  {&APIError{StatusCode: http.StatusBadRequest}, false},
		"403":                  {&APIError{StatusCode: http.StatusForbidden}, false},
		"unknown host":         {&url.Error{Op: "Post", URL: "u", Err: &net.DNSError{Err: "no such host", IsNotFound: true}}, false},
		"error without answer": {errors.New("cannot build the request"), false},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := isTransient(test.err); got != test.want {
				t.Errorf("isTransient(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}

func TestScopedTokenHint(t *testing.T) {
	for _, tc := range []struct {
		name   string
		token  string
		status int
		body   string
		hint   bool
	}{
		{"v1 refusal", "sqco_test", 401, ``, true},
		{"v2 refusal", "sqco_test", 403, `{"message":"Forbidden"}`, true},
		{"dop translation", "sqco_test", 400, `{"message":"No enum constant io.example.AuthenticationType.SOT"}`, true},
		{"other bad request", "sqco_test", 400, `{"message":"Invalid key"}`, false},
		{"not found", "sqco_test", 404, ``, false},
		{"user token", "squ_test", 403, ``, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c := New(Config{URL: srv.URL, APIURL: srv.URL, Token: tc.token, Product: ProductCloud, HTTPClient: srv.Client()})
			err := c.apiGet(t.Context(), "/organizations/organizations", nil, nil)
			if err == nil || strings.Contains(err.Error(), sotHint) != tc.hint {
				t.Fatalf("err=%v", err)
			}
			if strings.Contains(err.Error(), tc.token) {
				t.Fatal("the error shows the token")
			}
		})
	}
}
