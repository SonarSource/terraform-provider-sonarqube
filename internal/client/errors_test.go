package client

import "testing"

// TestAPIErrorWithoutDetail covers a failure that carries neither a known
// message shape nor a body, which leaves the status as the only information.
func TestAPIErrorWithoutDetail(t *testing.T) {
	t.Parallel()

	err := newAPIError(503, nil)

	if got, want := err.Error(), "unexpected status 503 from the API"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
