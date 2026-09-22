package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// FarmBornSalesRepository is the Sales > Farm born reporting read (maintainer request 2026-09-18).
//
// RECORDED cross-module reporting read, the load-wise shape (docs/decisions/sales-loadwise.md):
// procurement reads OUT to goats (the origin field that decides membership, and the register
// facts), goat_identifiers, goat_shed_partitions, locations and goat_sale_allocations +
// sales_deals to describe each born-here animal and its sale. Read-only, reporting grain only: nothing here gates a sale or
// an exit, and the sales module's own lock (migration 000173: sales reads nothing from
// herd/procurement) is untouched because the dependency points the other way.
type FarmBornSalesRepository interface {
	// FarmBornAnimals returns one fact per born-here animal the filter admits: every one on
	// the farm today, plus every one whose sale date falls inside the filter's window. The park,
	// pen, species, breed, sex and stage predicates are applied to both sides.
	FarmBornAnimals(ctx context.Context, tenantID string, filter domain.FarmBornFilter) ([]domain.FarmBornAnimalFact, error)

	// FarmBornOptions returns the filter bar's vocabulary: the parks, pens, species, breeds, sexes
	// and stages the born-here population actually has, on farm or sold, so the bar never
	// offers a choice that matches nothing.
	FarmBornOptions(ctx context.Context, tenantID string) (domain.FarmBornOptions, error)
}
