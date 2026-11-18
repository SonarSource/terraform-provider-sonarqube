package eventual

import (
	"context"
	"errors"
	"testing"
	"time"
)

var (
	errAgain = errors.New("again")
	errFinal = errors.New("final")
)

func TestRetry(t *testing.T) {
	t.Parallel()
	waits := []time.Duration{0, 0, 0}
	tests := map[string]struct {
		results   []error
		wantErr   error
		wantCalls int
	}{
		"success at once":            {results: []error{nil}, wantCalls: 1},
		"success after retries":      {results: []error{errAgain, errAgain, nil}, wantCalls: 3},
		"another failure stops":      {results: []error{errAgain, errFinal}, wantErr: errFinal, wantCalls: 2},
		"the last failure after all": {results: []error{errAgain}, wantErr: errAgain, wantCalls: 4},
	}
	for name, test := range tests {
		calls := 0
		err := Retry(t.Context(), waits, func(err error) bool { return errors.Is(err, errAgain) }, func() error {
			err := test.results[min(calls, len(test.results)-1)]
			calls++
			return err
		})
		if !errors.Is(err, test.wantErr) || (test.wantErr == nil && err != nil) || calls != test.wantCalls {
			t.Errorf("%s: err = %v, calls = %d", name, err, calls)
		}
	}
}

func TestRetryStopsWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	err := Retry(ctx, []time.Duration{time.Hour}, func(error) bool { return true }, func() error {
		calls++
		return errAgain
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Errorf("err = %v, calls = %d", err, calls)
	}
}
