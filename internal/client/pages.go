package client

import "fmt"

// collectPages calls fetch for page 1, 2 and on, until the items reach the
// total that the answers report. A page without items before that total is an
// error, so that a wrong total cannot make an endless loop. what names the
// items in that error.
func collectPages[T any](what string, fetch func(page int) (items []T, total int, err error)) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		items, total, err := fetch(page)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(all) >= total {
			return all, nil
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("%s page %d was empty before the reported total", what, page)
		}
	}
}
