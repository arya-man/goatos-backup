package http

import (
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// feedPurchasePayload is one ledger row on the wire.
type feedPurchasePayload struct {
	FeedPurchaseID string  `json:"feed_purchase_id"`
	PurchaseDate   string  `json:"purchase_date"`
	Farm           string  `json:"farm"`
	FeedItem       string  `json:"feed_item"`
	BatchNo        int     `json:"batch_no"`
	QuantityKg     float64 `json:"quantity_kg"`

	FeedCost      *float64 `json:"feed_cost"`
	TransportCost *float64 `json:"transport_cost"`
	LoadingCost   *float64 `json:"loading_cost"`
	UnloadingCost *float64 `json:"unloading_cost"`
	TotalCost     *float64 `json:"total_cost"`
	PerKgCost     *float64 `json:"per_kg_cost"`

	Vendor          string   `json:"vendor"`
	PaymentReleased *float64 `json:"payment_released"`
	PaymentStatus   string   `json:"payment_status"`
	// PaymentBalance is BACKEND-derived (total minus released, floored at zero), so no surface
	// computes its own money figure. Null while the landed cost is unknown.
	PaymentBalance *float64                     `json:"payment_balance"`
	Payments       []feedPurchasePaymentPayload `json:"payments"`

	EntrySource string `json:"entry_source"`
	CreatedAt   string `json:"created_at"`
}

// feedPurchasePaymentPayload is one instalment on the wire.
type feedPurchasePaymentPayload struct {
	PaymentID    string  `json:"payment_id"`
	PaidOn       string  `json:"paid_on"`
	AmountRupees float64 `json:"amount_rupees"`
	Note         string  `json:"note"`
	CreatedAt    string  `json:"created_at"`
}

// feedPurchasePaymentWritePayload is the record-instalment body.
type feedPurchasePaymentWritePayload struct {
	PaidOn       string  `json:"paid_on"`
	AmountRupees float64 `json:"amount_rupees"`
	Note         string  `json:"note"`
}

func (p feedPurchasePaymentWritePayload) toDomain() domain.FeedPurchasePaymentWrite {
	return domain.FeedPurchasePaymentWrite{PaidOn: p.PaidOn, AmountRupees: p.AmountRupees, Note: p.Note}
}

// feedPurchaseEditPayload is the edit-purchase body: the values of an already-recorded load.
// Identity (farm, feed, batch) and payment fields are deliberately absent -- see the domain type.
type feedPurchaseEditPayload struct {
	PurchaseDate string  `json:"purchase_date"`
	QuantityKg   float64 `json:"quantity_kg"`

	FeedCost      *float64 `json:"feed_cost"`
	TransportCost *float64 `json:"transport_cost"`
	LoadingCost   *float64 `json:"loading_cost"`
	UnloadingCost *float64 `json:"unloading_cost"`
	TotalCost     *float64 `json:"total_cost"`

	Vendor string `json:"vendor"`
}

func (p feedPurchaseEditPayload) toDomain() domain.FeedPurchaseEdit {
	return domain.FeedPurchaseEdit{
		PurchaseDate: p.PurchaseDate, QuantityKg: p.QuantityKg,
		FeedCost: p.FeedCost, TransportCost: p.TransportCost,
		LoadingCost: p.LoadingCost, UnloadingCost: p.UnloadingCost, TotalCost: p.TotalCost,
		Vendor: p.Vendor,
	}
}

// feedPurchaseStatusWritePayload is the payment-status edit body.
type feedPurchaseStatusWritePayload struct {
	PaymentStatus string `json:"payment_status"`
}

// feedPurchasePagePayload is one ledger page plus its whole-filter aggregates.
type feedPurchasePagePayload struct {
	Purchases   []feedPurchasePayload `json:"purchases"`
	Total       int                   `json:"total"`
	QuantityKg  float64               `json:"quantity_kg"`
	SpendRupees float64               `json:"spend_rupees"`
	Limit       int                   `json:"limit"`
	Offset      int                   `json:"offset"`
}

// feedPurchaseOptionsPayload is the entry form's backend-owned vocabulary.
type feedPurchaseOptionsPayload struct {
	Farms           []string                `json:"farms"`
	FeedItems       []feedItemOptionPayload `json:"feed_items"`
	PaymentStatuses []string                `json:"payment_statuses"`
	Vendors         []string                `json:"vendors"`
}

type feedItemOptionPayload struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// feedPurchaseWritePayload is the record-purchase body.
//
// Every optional money field is a POINTER so "not entered" stays distinct from "entered as 0": a
// zero transport cost is a real recorded fact (the sheet has such loads), and coercing a blank box
// into it would invent that fact.
type feedPurchaseWritePayload struct {
	PurchaseDate string  `json:"purchase_date"`
	Farm         string  `json:"farm"`
	FeedItem     string  `json:"feed_item"`
	BatchNo      *int    `json:"batch_no"`
	QuantityKg   float64 `json:"quantity_kg"`

	FeedCost      *float64 `json:"feed_cost"`
	TransportCost *float64 `json:"transport_cost"`
	LoadingCost   *float64 `json:"loading_cost"`
	UnloadingCost *float64 `json:"unloading_cost"`
	TotalCost     *float64 `json:"total_cost"`

	Vendor          string   `json:"vendor"`
	PaymentReleased *float64 `json:"payment_released"`
	PaymentStatus   string   `json:"payment_status"`
}

func (p feedPurchaseWritePayload) toDomain() domain.FeedPurchaseWrite {
	return domain.FeedPurchaseWrite{
		PurchaseDate: p.PurchaseDate, FarmLabel: p.Farm, FeedItemLabel: p.FeedItem,
		BatchNo: p.BatchNo, QuantityKg: p.QuantityKg,
		FeedCost: p.FeedCost, TransportCost: p.TransportCost,
		LoadingCost: p.LoadingCost, UnloadingCost: p.UnloadingCost, TotalCost: p.TotalCost,
		Vendor: p.Vendor, PaymentReleased: p.PaymentReleased, PaymentStatus: p.PaymentStatus,
	}
}

func toFeedPurchasePayload(p domain.FeedPurchase) feedPurchasePayload {
	// Empty slice, never nil: a JSON null where the client expects a list is a render crash, and
	// "no instalments yet" is the normal state of sheet history.
	payments := make([]feedPurchasePaymentPayload, 0, len(p.Payments))
	for _, payment := range p.Payments {
		payments = append(payments, feedPurchasePaymentPayload{
			PaymentID: payment.PaymentID, PaidOn: payment.PaidOn,
			AmountRupees: payment.AmountRupees, Note: payment.Note, CreatedAt: payment.CreatedAt,
		})
	}
	return feedPurchasePayload{
		FeedPurchaseID: p.FeedPurchaseID, PurchaseDate: p.PurchaseDate, Farm: p.FarmLabel,
		FeedItem: p.FeedItemLabel, BatchNo: p.BatchNo, QuantityKg: p.QuantityKg,
		FeedCost: p.FeedCost, TransportCost: p.TransportCost, LoadingCost: p.LoadingCost,
		UnloadingCost: p.UnloadingCost, TotalCost: p.TotalCost, PerKgCost: p.PerKgCost,
		Vendor: p.Vendor, PaymentReleased: p.PaymentReleased, PaymentStatus: p.PaymentStatus,
		PaymentBalance: p.PaymentBalance(), Payments: payments,
		EntrySource: p.EntrySource, CreatedAt: p.CreatedAt,
	}
}

func toFeedPurchaseOptionsPayload(o ports.FeedPurchaseOptions) feedPurchaseOptionsPayload {
	items := make([]feedItemOptionPayload, 0, len(o.FeedItems))
	for _, item := range o.FeedItems {
		items = append(items, feedItemOptionPayload{Key: item.Key, Label: item.Label})
	}
	// Empty slices, never nil: a JSON null where the client expects a list is a render crash, and
	// "no feeds in the catalog" is a legitimate state on a fresh tenant.
	farms := o.Farms
	if farms == nil {
		farms = []string{}
	}
	statuses := o.PaymentStatuses
	if statuses == nil {
		statuses = []string{}
	}
	vendors := o.Vendors
	if vendors == nil {
		vendors = []string{}
	}
	return feedPurchaseOptionsPayload{
		Farms: farms, FeedItems: items, PaymentStatuses: statuses, Vendors: vendors,
	}
}
