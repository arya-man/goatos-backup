package domain

import (
	"errors"
	"strings"
)

// TableSort is one whole-result order a read table asked for: a column KEY of that table's page
// contract and a direction (maintainer request 2026-09-25, "sort all rows"). The Buyer analytics
// and Farm born tables used to sort only the page the browser held -- 25 rows out of hundreds --
// so "revenue, highest first" showed the biggest buyer ON THAT PAGE. The order is now applied to
// the whole filtered set before the page is sliced, so page 2 continues the same order.
type TableSort struct {
	Key  string
	Desc bool
}

// ErrTableSortInvalid rejects an unknown column or direction rather than silently falling back to
// the default order, which would show a sorted-looking table in an order nobody asked for.
var ErrTableSortInvalid = errors.New("procurement: table sort column or direction is not one this table orders by")

// ParseTableSort reads the `sort` / `dir` query pair against the table's orderable keys. An
// absent key is the table's default order; a direction without a key is refused.
func ParseTableSort(key, dir string, orderable map[string]bool, def TableSort) (TableSort, error) {
	key = strings.TrimSpace(key)
	dir = strings.ToLower(strings.TrimSpace(dir))
	if key == "" {
		if dir != "" {
			return TableSort{}, ErrTableSortInvalid
		}
		return def, nil
	}
	if !orderable[key] {
		return TableSort{}, ErrTableSortInvalid
	}
	switch dir {
	case "", "asc":
		return TableSort{Key: key}, nil
	case "desc":
		return TableSort{Key: key, Desc: true}, nil
	default:
		return TableSort{}, ErrTableSortInvalid
	}
}

// compareOrdered is -1/0/1 for two ordered values.
func compareOrdered[T ~int | ~float64 | ~string](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// directed applies the direction to a comparison of PRESENT values; absence is handled by the
// caller so a missing value sits last whichever way the column is sorted.
func directed(c int, desc bool) int {
	if desc {
		return -c
	}
	return c
}
