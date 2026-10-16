package client

import (
	"errors"
	"slices"
	"testing"
)

func TestCollectPagesReadsUntilTheTotal(t *testing.T) {
	t.Parallel()
	var pages []int
	items, err := collectPages("item", func(page int) ([]int, int, error) {
		pages = append(pages, page)
		return []int{page * 10, page*10 + 1}, 3, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(items, []int{10, 11, 20, 21}) || !slices.Equal(pages, []int{1, 2}) {
		t.Errorf("items=%v, pages=%v", items, pages)
	}
}

func TestCollectPagesStopsOnAnEmptyPage(t *testing.T) {
	t.Parallel()
	_, err := collectPages("item", func(int) ([]int, int, error) { return nil, 5, nil })
	if err == nil || err.Error() != "item page 1 was empty before the reported total" {
		t.Errorf("err = %v", err)
	}
}

func TestCollectPagesReturnsTheFetchError(t *testing.T) {
	t.Parallel()
	failure := errors.New("failure")
	if _, err := collectPages("item", func(int) ([]int, int, error) { return nil, 0, failure }); !errors.Is(err, failure) {
		t.Errorf("err = %v", err)
	}
}
