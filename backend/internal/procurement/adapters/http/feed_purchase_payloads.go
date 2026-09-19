package http

import (
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// feedPurchasePayload is one ledger row on the wire.
type feedPurchasePayload struct {
	Answers              map[string]string        `json:"answers"`
	QuestionnaireVersion int                      `json:"questionnaire_version"`
	AnswerRows           []vendorAnswerRowPayload `json:"answer_rows"`
	FeedPurchaseID       string                   `json:"feed_purchase_id"`
	PurchaseDate         string                   `json:"purchase_date"`
	Farm                 string                   `json:"farm"`
	FeedItem             string                   `json:"feed_item"`
	BatchNo              int                      `json:"batch_no"`
	QuantityKg           float64                  `json:"quantity_kg"`

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

	// Delivery state (maintainer decision 2026-09-03). ReachedOn and ReachedWeightKg are null
	// while the load is on the road; StockKg is BACKEND-derived (received weight if entered, else
	// buying weight) and null while in transit, so no surface decides for itself what a load is
	// worth in the store.
	DeliveryStatus  string   `json:"delivery_status"`
	ReachedOn       *string  `json:"reached_on"`
	ReachedWeightKg *float64 `json:"reached_weight_kg"`
	StockKg         *float64 `json:"stock_kg"`

	// DaysOfStock is the buyer's stated coverage in days, null when not stated.
	DaysOfStock *int `json:"days_of_stock"`

	EntrySource string `json:"entry_source"`
	CreatedAt   string `json:"created_at"`
}

// feedPurchaseDeliveryWritePayload is the mark-reached / update-arrival body.
type feedPurchaseDeliveryWritePayload struct {
	ReachedOn       string   `json:"reached_on"`
	ReachedWeightKg *float64 `json:"reached_weight_kg"`
}

func (p feedPurchaseDeliveryWritePayload) toDomain() domain.FeedPurchaseDeliveryWrite {
	return domain.FeedPurchaseDeliveryWrite{ReachedOn: p.ReachedOn, ReachedWeightKg: p.ReachedWeightKg}
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

	Vendor      string `json:"vendor"`
	DaysOfStock *int   `json:"days_of_stock"`
}

func (p feedPurchaseEditPayload) toDomain() domain.FeedPurchaseEdit {
	return domain.FeedPurchaseEdit{
		PurchaseDate: p.PurchaseDate, QuantityKg: p.QuantityKg,
		FeedCost: p.FeedCost, TransportCost: p.TransportCost,
		LoadingCost: p.LoadingCost, UnloadingCost: p.UnloadingCost, TotalCost: p.TotalCost,
		Vendor: p.Vendor, DaysOfStock: p.DaysOfStock,
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
	// DeliveryStatuses carries the farm label per state so the phone's chips render backend words.
	DeliveryStatuses []deliveryStatusOptionPayload `json:"delivery_statuses"`
	Vendors          []string                      `json:"vendors"`
}

type deliveryStatusOptionPayload struct {
	Key   string `json:"key"`
	Label string `json:"label"`
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

	// Optional: how many days of feeding the buyer expects the load to cover.
	DaysOfStock *int `json:"days_of_stock"`

	// Optional: a load that already arrived when it is recorded. Absent means still on the road.
	ReachedOn       *string  `json:"reached_on"`
	ReachedWeightKg *float64 `json:"reached_weight_kg"`

	// THE FEED PURCHASE FORM IS AUTHORED (2026-09-20): a form-driven client sends EVERY answer it
	// asked for -- typed and authored alike -- plus the form version it rendered, and the write is
	// checked against exactly that version. A client that sends neither (an older APK) is accepted
	// on its typed fields alone, unchanged.
	Answers              map[string]string `json:"answers"`
	QuestionnaireVersion int               `json:"questionnaire_version"`
}

func (p feedPurchaseWritePayload) toDomain() domain.FeedPurchaseWrite {
	reachedOn := ""
	if p.ReachedOn != nil {
		reachedOn = *p.ReachedOn
	}
	return domain.FeedPurchaseWrite{
		PurchaseDate: p.PurchaseDate, FarmLabel: p.Farm, FeedItemLabel: p.FeedItem,
		BatchNo: p.BatchNo, QuantityKg: p.QuantityKg,
		FeedCost: p.FeedCost, TransportCost: p.TransportCost,
		LoadingCost: p.LoadingCost, UnloadingCost: p.UnloadingCost, TotalCost: p.TotalCost,
		Vendor: p.Vendor, PaymentReleased: p.PaymentReleased, PaymentStatus: p.PaymentStatus,
		DaysOfStock: p.DaysOfStock,
		ReachedOn:   reachedOn, ReachedWeightKg: p.ReachedWeightKg,
		SOPAnswers: p.Answers, QuestionnaireVersion: p.QuestionnaireVersion,
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
	answers := p.SOPAnswers
	if answers == nil {
		answers = map[string]string{}
	}
	answerRows := make([]vendorAnswerRowPayload, 0, len(p.AnswerRows))
	for _, row := range p.AnswerRows {
		answerRows = append(answerRows, vendorAnswerRowPayload{QuestionID: row.QuestionID, Label: row.Label, Value: row.Value})
	}
	return feedPurchasePayload{
		Answers: answers, QuestionnaireVersion: p.QuestionnaireVersion, AnswerRows: answerRows,
		FeedPurchaseID: p.FeedPurchaseID, PurchaseDate: p.PurchaseDate, Farm: p.FarmLabel,
		FeedItem: p.FeedItemLabel, BatchNo: p.BatchNo, QuantityKg: p.QuantityKg,
		FeedCost: p.FeedCost, TransportCost: p.TransportCost, LoadingCost: p.LoadingCost,
		UnloadingCost: p.UnloadingCost, TotalCost: p.TotalCost, PerKgCost: p.PerKgCost,
		Vendor: p.Vendor, PaymentReleased: p.PaymentReleased, PaymentStatus: p.PaymentStatus,
		PaymentBalance: p.PaymentBalance(), Payments: payments,
		DeliveryStatus: p.DeliveryStatus, ReachedOn: p.ReachedOn, ReachedWeightKg: p.ReachedWeightKg,
		StockKg:     p.StockKg(),
		DaysOfStock: p.DaysOfStock,
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
	deliveries := make([]deliveryStatusOptionPayload, 0, len(o.DeliveryStatuses))
	for _, d := range o.DeliveryStatuses {
		deliveries = append(deliveries, deliveryStatusOptionPayload{Key: d.Key, Label: d.Label})
	}
	return feedPurchaseOptionsPayload{
		Farms: farms, FeedItems: items, PaymentStatuses: statuses, DeliveryStatuses: deliveries, Vendors: vendors,
	}
}
