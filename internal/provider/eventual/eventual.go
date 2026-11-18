// Package eventual reads the eventually consistent lists of SonarQube Cloud
// again until they show the result of a write.
package eventual

import (
	"context"
	"time"
)

// ReadAgain calls read, and while settled does not accept the result, calls
// it again after each wait. The lists of SonarQube Cloud are eventually
// consistent, so a read soon after a write can show the old values. After the
// last wait, the result is returned as it is.
func ReadAgain[T any](ctx context.Context, waits []time.Duration, read func() (T, error), settled func(T, error) bool) (T, error) {
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

// GroupChangeWaits are the waits before a call that names a group is tried
// again. For some seconds after a group is created, renamed or deleted, the
// calls that find a group by its name can fail.
var GroupChangeWaits = []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second}

// Retry calls try, and calls it again after each wait while again accepts
// the failure. Success, another failure, or the end of ctx stops it.
func Retry(ctx context.Context, waits []time.Duration, again func(error) bool, try func() error) error {
	_, err := ReadAgain(ctx, waits,
		func() (struct{}, error) { return struct{}{}, try() },
		func(_ struct{}, err error) bool { return err == nil || !again(err) })
	return err
}
