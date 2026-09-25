package app

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// testParks stands in for the tenant's active parks (Configuration > Items & settings > Parks).
// A third park, Hosur, is included so tests can prove a park added after the first two is
// understood by name and code exactly like them.
type testParks struct{}

func (testParks) ActiveParks(context.Context, string) ([]domain.ParkRef, error) {
	return []domain.ParkRef{
		{Code: "CPT", Name: "Channapatna", ID: "00000000-0000-4000-8000-000000003002"},
		{Code: "CBE", Name: "Coimbatore", ID: "00000000-0000-4000-8000-000000003001"},
		{Code: "HSR", Name: "Hosur", ID: "00000000-0000-4000-8000-000000003099"},
	}, nil
}
