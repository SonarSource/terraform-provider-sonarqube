package client

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

var (
	transient = &APIError{StatusCode: http.StatusServiceUnavailable}
	final     = &APIError{StatusCode: http.StatusForbidden}
)

// tries returns a try that answers each of results in turn, and counts the
// calls.
func tries(results ...error) (func() error, *int) {
	calls := 0
	return func() error {
		err := results[min(calls, len(results)-1)]
		calls++
		return err
	}, &calls
}

func TestRetry(t *testing.T) {
	t.Parallel()
	waits := []time.Duration{0, 0, 0}
	tests := map[string]struct {
		results   []error
		wantErr   error
		wantCalls int
	}{
		"success at once":               {results: []error{nil}, wantCalls: 1},
		"success after transient fails": {results: []error{transient, transient, nil}, wantCalls: 3},
		"a final failure stops":         {results: []error{transient, final}, wantErr: final, wantCalls: 2},
		"the last failure after all":    {results: []error{transient}, wantErr: transient, wantCalls: 4},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			try, calls := tries(test.results...)
			if err := retry(t.Context(), waits, try); !errors.Is(err, test.wantErr) || (test.wantErr == nil && err != nil) {
				t.Errorf("err = %v, want %v", err, test.wantErr)
			}
			if *calls != test.wantCalls {
				t.Errorf("calls = %d, want %d", *calls, test.wantCalls)
			}
		})
	}
}

func TestRetryStopsWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	try, calls := tries(transient)
	cancel()

	err := retry(ctx, []time.Duration{time.Hour}, try)

	if !errors.Is(err, context.Canceled) || !errors.Is(err, transient) {
		t.Errorf("err = %v, want the failure and context.Canceled", err)
	}
	if *calls != 1 {
		t.Errorf("calls = %d, want 1", *calls)
	}
}
