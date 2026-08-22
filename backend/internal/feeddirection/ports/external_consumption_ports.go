package ports

import "context"

// RecordExternalConsumptionCommand upserts one (park, feed, day) row of the
// feed_external_consumption ledger (migration 000185) — daily consumption of a
// feed GoatOS does not direct through feed sheets. Today's one producer is the
// verified mobile Milk Preparation completion forwarding its UHT-milk answer
// (maintainer decision 2026-08-22: consumption comes from the app; purchases
// arrive later via Procurement). The sheet importer
// (cmd/import-feed-external-consumption) writes the same natural key for the
// bootstrap history, so an overlap day converges instead of double-counting.
type RecordExternalConsumptionCommand struct {
	TenantID string
	ParkID   string
	// FeedItemLabel must resolve in feed_item_catalog ("UHT Milk").
	FeedItemLabel string
	// FeedDay is the ISO business date the feed physically left the store.
	FeedDay string
	// QuantityKg is the day's consumption; UHT litres are recorded 1:1 as kg,
	// the same convention the farm's sheet has always used.
	QuantityKg float64
	// SourceRef names the producing record ("milk-preparation:<completion_id>:attempt=<n>").
	SourceRef string
}

// ExternalConsumptionStore is implemented by the feeddirection Postgres
// repository, which owns feed_external_consumption.
type ExternalConsumptionStore interface {
	RecordExternalConsumption(ctx context.Context, in RecordExternalConsumptionCommand) error
}
