// Package domain holds the inventory domain types (items, stock lots, ledger movements).
package domain

import "time"

// Item is an inventory item definition (vaccine, dewormer, feed, …).
type Item struct {
	ItemID   string
	TenantID string
	ItemCode string
	Name     string
	Category string
	BaseUnit string
	Status   string
}

// NewItem is the input to create an Item.
type NewItem struct {
	TenantID string
	ItemCode string
	Name     string
	Category string
	BaseUnit string
	Status   string
}

// StockLot is a quantity of an item at a location (a lot with optional expiry).
type StockLot struct {
	StockID          string
	ItemID           string
	LocationID       string
	QuantityInStock  string
	QuantityReserved string
	QuantityUnit     string
	RowVersion       int32
}

// NewStockLot is the input to create a stock lot.
type NewStockLot struct {
	TenantID         string
	ItemID           string
	LocationID       string
	LotCode          string
	ExpiryDate       *time.Time
	QuantityInStock  string
	QuantityReserved string
	QuantityUnit     string
	Status           string
}

// FEFOPick is the earliest-expiring lot with available (unreserved) stock.
type FEFOPick struct {
	StockID           string
	LotCode           string
	ExpiryDate        *time.Time
	QuantityInStock   string
	QuantityReserved  string
	AvailableQuantity string
	QuantityUnit      string
}

// Movement is an append-only ledger entry. Quantity is a positive magnitude; direction is
// implied by MovementType (receive/reserve/consume/release/adjust/expire/transfer_*).
type Movement struct {
	TenantID       string
	LotID          string
	ItemID         string
	LocationID     string
	MovementType   string
	Quantity       string
	QuantityUnit   string
	BatchID        *string
	ActorID        *string
	Reason         string
	IdempotencyKey string
}
