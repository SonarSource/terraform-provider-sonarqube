package shared

import (
	"context"
	"time"
)

// readAgain calls read, and while settled does not accept the result, calls
// it again after each wait. The lists of SonarQube Cloud are eventually
// consistent, so a read soon after a write can show the old values. After the
// last wait, the result is returned as it is.
func readAgain[T any](ctx context.Context, waits []time.Duration, read func() (T, error), settled func(T, error) bool) (T, error) {
	value, err := read()
	for _, wait := range waits {
		if settled(value, err) {
			return value, err
		}
		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		case <-time.After(wait):
		}
		value, err = read()
	}
	return value, err
}
