package http

// Wire payloads for the pipeline and evidence writes. Same conventions as the deal payloads:
// snake_case JSON, optional text as *string, optional numbers as *float64 so 0 stays distinct
// from absent.

import "github.com/vgoats/goatos/backend/internal/sales/domain"

// ---- Buyer leads ----

type buyerLeadPayload struct {
	LeadID       string  `json:"lead_id"`
	RecordedDate *string `json:"recorded_date"`
	Farm         *string `json:"farm"`
	BuyerName    string  `json:"buyer_name"`
	BuyerPlace   *string `json:"buyer_place"`
	AnimalType   *string `json:"animal_type"`
	Breed        *string `json:"breed"`
	PhoneNumber  *string `json:"phone_number"`
	CallStatus   *string `json:"call_status"`
	CreatedAt    string  `json:"created_at"`
}

func toBuyerLeadPayload(l domain.BuyerLead) buyerLeadPayload {
	return buyerLeadPayload{
		LeadID:       l.LeadID,
		RecordedDate: l.RecordedDate,
		Farm:         l.Farm,
		BuyerName:    l.BuyerName,
		BuyerPlace:   l.BuyerPlace,
		AnimalType:   l.AnimalType,
		Breed:        l.Breed,
		PhoneNumber:  l.PhoneNumber,
		CallStatus:   l.CallStatus,
		CreatedAt:    l.CreatedAt,
	}
}

type buyerLeadPagePayload struct {
	Leads         []buyerLeadPayload        `json:"leads"`
	Total         int                       `json:"total"`
	StatusOptions []string                  `json:"status_options"`
	StatusFilters []leadStatusFilterPayload `json:"status_filters"`
}

type buyerLeadWritePayload struct {
	RecordedDate string `json:"recorded_date"`
	Farm         string `json:"farm"`
	BuyerName    string `json:"buyer_name"`
	BuyerPlace   string `json:"buyer_place"`
	AnimalType   string `json:"animal_type"`
	Breed        string `json:"breed"`
	PhoneNumber  string `json:"phone_number"`
	CallStatus   string `json:"call_status"`
}

func (p buyerLeadWritePayload) toDomain() domain.BuyerLeadWrite {
	return domain.BuyerLeadWrite{
		RecordedDate: p.RecordedDate,
		Farm:         p.Farm,
		BuyerName:    p.BuyerName,
		BuyerPlace:   p.BuyerPlace,
		AnimalType:   p.AnimalType,
		Breed:        p.Breed,
		PhoneNumber:  p.PhoneNumber,
		CallStatus:   p.CallStatus,
	}
}

// ---- FPO leads ----

type fpoLeadPayload struct {
	LeadID      string  `json:"lead_id"`
	FPOName     string  `json:"fpo_name"`
	Crops       *string `json:"crops"`
	District    *string `json:"district"`
	Taluk       *string `json:"taluk"`
	State       *string `json:"state"`
	PhoneNumber *string `json:"phone_number"`
	CallStatus  *string `json:"call_status"`
	CreatedAt   string  `json:"created_at"`
}

func toFPOLeadPayload(l domain.FPOLead) fpoLeadPayload {
	return fpoLeadPayload{
		LeadID:      l.LeadID,
		FPOName:     l.FPOName,
		Crops:       l.Crops,
		District:    l.District,
		Taluk:       l.Taluk,
		State:       l.State,
		PhoneNumber: l.PhoneNumber,
		CallStatus:  l.CallStatus,
		CreatedAt:   l.CreatedAt,
	}
}

type fpoLeadPagePayload struct {
	Leads         []fpoLeadPayload          `json:"leads"`
	Total         int                       `json:"total"`
	StatusOptions []string                  `json:"status_options"`
	StatusFilters []leadStatusFilterPayload `json:"status_filters"`
}

type fpoLeadWritePayload struct {
	FPOName     string `json:"fpo_name"`
	Crops       string `json:"crops"`
	District    string `json:"district"`
	Taluk       string `json:"taluk"`
	State       string `json:"state"`
	PhoneNumber string `json:"phone_number"`
	CallStatus  string `json:"call_status"`
}

func (p fpoLeadWritePayload) toDomain() domain.FPOLeadWrite {
	return domain.FPOLeadWrite{
		FPOName:     p.FPOName,
		Crops:       p.Crops,
		District:    p.District,
		Taluk:       p.Taluk,
		State:       p.State,
		PhoneNumber: p.PhoneNumber,
		CallStatus:  p.CallStatus,
	}
}

// ---- Shared status write ----

// leadStatusFilterPayload is one option on the board's call-status facet: the value to send back
// and the farm's word for it. Backend-owned so the phone and the web word the bucket identically.
type leadStatusFilterPayload struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

func toStatusFilterPayloads(in []domain.LeadStatusFilter) []leadStatusFilterPayload {
	out := make([]leadStatusFilterPayload, 0, len(in))
	for _, f := range in {
		out = append(out, leadStatusFilterPayload{Value: f.Value, Label: f.Label})
	}
	return out
}

type leadStatusWritePayload struct {
	CallStatus string `json:"call_status"`
}

func leadStatusFromPayload(p leadStatusWritePayload) domain.LeadStatusWrite {
	return domain.LeadStatusWrite{CallStatus: p.CallStatus}
}

// ---- Market quotes ----

type benchmarkWritePayload struct {
	Market           string   `json:"market"`
	Category         string   `json:"category"`
	Breed            string   `json:"breed"`
	Source           string   `json:"source"`
	ExFarmRate       string   `json:"ex_farm_rate"`
	TransportRate    string   `json:"transport_rate"`
	LandingCostPerKg *float64 `json:"landing_cost_per_kg"`
	MarketPricePerKg *float64 `json:"market_price_per_kg"`
}

func (p benchmarkWritePayload) toDomain() domain.BenchmarkWrite {
	return domain.BenchmarkWrite{
		Market:           p.Market,
		Category:         p.Category,
		Breed:            p.Breed,
		Source:           p.Source,
		ExFarmRate:       p.ExFarmRate,
		TransportRate:    p.TransportRate,
		LandingCostPerKg: p.LandingCostPerKg,
		MarketPricePerKg: p.MarketPricePerKg,
	}
}

// ---- Sold-tag lists ----

type soldTagRowPayload struct {
	AnimalLabel string   `json:"animal_label"`
	TagNumber   string   `json:"tag_number"`
	WeightKg    *float64 `json:"weight_kg"`
}

type soldTagsWritePayload struct {
	Farm string              `json:"farm"`
	Rows []soldTagRowPayload `json:"rows"`
}

func (p soldTagsWritePayload) toDomain() domain.SoldTagsWrite {
	rows := make([]domain.SoldTagRow, 0, len(p.Rows))
	for _, row := range p.Rows {
		rows = append(rows, domain.SoldTagRow{
			AnimalLabel: row.AnimalLabel,
			TagNumber:   row.TagNumber,
			WeightKg:    row.WeightKg,
		})
	}
	return domain.SoldTagsWrite{Farm: p.Farm, Rows: rows}
}

type soldTagsResultPayload struct {
	Recorded int `json:"recorded"`
}

// ---- Weight checks ----

type weightCheckWritePayload struct {
	TagNumber     string  `json:"tag_number"`
	BookWeightKg  float64 `json:"book_weight_kg"`
	VideoWeightKg float64 `json:"video_weight_kg"`
	FarmBorn      bool    `json:"farm_born"`
}

func (p weightCheckWritePayload) toDomain() domain.WeightCheckWrite {
	return domain.WeightCheckWrite{
		TagNumber:     p.TagNumber,
		BookWeightKg:  p.BookWeightKg,
		VideoWeightKg: p.VideoWeightKg,
		FarmBorn:      p.FarmBorn,
	}
}

type recordedPayload struct {
	Recorded bool `json:"recorded"`
}
