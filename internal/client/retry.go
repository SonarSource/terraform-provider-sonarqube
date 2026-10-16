package client

import (
	"context"
	"errors"
	"time"
)

// retry calls try, and after a transient failure, calls it again after each
// of waits. A failure that is not transient, or the end of ctx, stops it.
// Each try can take up to the timeout of the HTTP client, in addition to the
// waits.
func retry(ctx context.Context, waits []time.Duration, try func() error) error {
	err := try()
	for _, wait := range waits {
		if err == nil || !isTransient(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-time.After(wait):
		}
		err = try()
	}
	return err
}
