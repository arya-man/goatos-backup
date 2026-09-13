package http

import (
	"time"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
)

type countsPayload struct {
	Total    int `json:"total"`
	Pending  int `json:"pending"`
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

type loadPayload struct {
	LoadID        string        `json:"load_id"`
	LoadRef       string        `json:"load_ref"`
	Title         string        `json:"title"`
	VendorID      string        `json:"vendor_id"`
	VendorName    string        `json:"vendor_name"`
	Farm          string        `json:"farm"`
	ExpectedCount int           `json:"expected_count"`
	Notes         string        `json:"notes"`
	Status        string        `json:"status"`
	Counts        countsPayload `json:"counts"`
	// Summary is the backend-owned one-liner under the title: "12 animals · 3 awaiting decision".
	Summary    string    `json:"summary"`
	RecordedBy string    `json:"recorded_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	RowVersion int       `json:"row_version"`
}

type loadPagePayload struct {
	Loads      []loadPayload `json:"loads"`
	NextCursor string        `json:"next_cursor,omitempty"`
	// CanRecord says whether THIS caller may add loads and animals (the write permission,
	// resolved per request from the caller's own grants). The read routes are open to the CEO,
	// who watches but never records, so the phone must be told rather than offer a form the
	// write route then refuses.
	CanRecord bool `json:"can_record"`
}

type candidatePayload struct {
	CandidateID    string     `json:"candidate_id"`
	LoadID         string     `json:"load_id"`
	LoadRef        string     `json:"load_ref"`
	SeqNo          int        `json:"seq_no"`
	Title          string     `json:"title"`
	Species        string     `json:"species"`
	SpeciesLabel   string     `json:"species_label"`
	Sex            string     `json:"sex"`
	SexLabel       string     `json:"sex_label"`
	Breed          string     `json:"breed"`
	AgeMonths      *int       `json:"age_months,omitempty"`
	WeightKg       *float64   `json:"weight_kg,omitempty"`
	Condition      string     `json:"condition"`
	ConditionLabel string     `json:"condition_label"`
	TempTag        string     `json:"temp_tag"`
	Notes          string     `json:"notes"`
	VideoProofRef  string     `json:"video_proof_ref"`
	MediaURL       string     `json:"media_url,omitempty"`
	MediaMime      string     `json:"media_mime,omitempty"`
	Decision       string     `json:"decision"`
	DecisionLabel  string     `json:"decision_label"`
	DecisionTone   string     `json:"decision_tone"`
	DecidedByName  string     `json:"decided_by_name"`
	DecidedAt      *time.Time `json:"decided_at,omitempty"`
	DecisionNote   string     `json:"decision_note"`
	RecordedBy     string     `json:"recorded_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	RowVersion     int        `json:"row_version"`
}

type candidatePagePayload struct {
	Animals    []candidatePayload `json:"animals"`
	NextCursor string             `json:"next_cursor,omitempty"`
	// Counts are WHOLE-FILTER decision counts, never page sums.
	Counts countsPayload `json:"counts"`
}

type candidateMediaPayload struct {
	CandidateID string `json:"candidate_id"`
	MediaURL    string `json:"media_url"`
	MediaMime   string `json:"media_mime"`
}

type loadDetailPayload struct {
	Load       loadPayload        `json:"load"`
	Animals    []candidatePayload `json:"animals"`
	NextCursor string             `json:"next_cursor,omitempty"`
	CanRecord  bool               `json:"can_record"`
}

type reviewPagePayload struct {
	Animals    []candidatePayload `json:"animals"`
	NextCursor string             `json:"next_cursor,omitempty"`
	Counts     countsPayload      `json:"counts"`
	// Filters are the review page's decision chips, backend-owned labels with whole-filter counts.
	Filters []filterPayload `json:"filters"`
}

type filterPayload struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Count    int    `json:"count"`
	Selected bool   `json:"selected"`
}

type optionsPayload struct {
	Species          []domain.Option `json:"species"`
	Sexes            []domain.Option `json:"sexes"`
	Conditions       []domain.Option `json:"conditions"`
	Farms            []domain.Option `json:"farms"`
	BreedSuggestions []string        `json:"breed_suggestions"`
	// Copy the phone form renders verbatim.
	Copy map[string]string `json:"copy"`
}

type createLoadBody struct {
	LoadRef       string `json:"load_ref"`
	VendorID      string `json:"vendor_id"`
	Farm          string `json:"farm"`
	ExpectedCount int    `json:"expected_count"`
	Notes         string `json:"notes"`
}

type addAnimalBody struct {
	Species       string   `json:"species"`
	Sex           string   `json:"sex"`
	Breed         string   `json:"breed"`
	AgeMonths     *int     `json:"age_months"`
	WeightKg      *float64 `json:"weight_kg"`
	Condition     string   `json:"condition"`
	TempTag       string   `json:"temp_tag"`
	Notes         string   `json:"notes"`
	VideoProofRef string   `json:"video_proof_ref"`
}

type decisionBody struct {
	Decision   string `json:"decision"`
	Note       string `json:"note"`
	RowVersion int    `json:"row_version"`
}

func toCounts(c domain.DecisionCounts) countsPayload {
	return countsPayload{Total: c.Total, Pending: c.Pending, Accepted: c.Accepted, Rejected: c.Rejected}
}

func toLoadPayload(l domain.Load) loadPayload {
	return loadPayload{
		LoadID: l.LoadID, LoadRef: l.LoadRef, Title: domain.LoadTitle(l), VendorID: l.VendorID, VendorName: l.VendorName,
		Farm: l.FarmLabel, ExpectedCount: l.ExpectedCount, Notes: l.Notes, Status: l.Status, Counts: toCounts(l.Counts),
		Summary: loadSummary(l), RecordedBy: l.RecordedBy, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt, RowVersion: l.RowVersion,
	}
}

func toCandidatePayload(c domain.Candidate, mediaURL, mediaMime string) candidatePayload {
	return candidatePayload{
		CandidateID: c.CandidateID, LoadID: c.LoadID, LoadRef: c.LoadRef, SeqNo: c.SeqNo, Title: domain.CandidateTitle(c),
		Species: c.Species, SpeciesLabel: domain.SpeciesLabel(c.Species), Sex: c.Sex, SexLabel: domain.SexLabel(c.Sex),
		Breed: c.Breed, AgeMonths: c.AgeMonths, WeightKg: c.WeightKg,
		Condition: c.Condition, ConditionLabel: domain.ConditionLabel(c.Condition), TempTag: c.TempTag, Notes: c.Notes,
		VideoProofRef: c.VideoProofRef, MediaURL: mediaURL, MediaMime: mediaMime,
		Decision: c.Decision, DecisionLabel: domain.DecisionLabel(c.Decision), DecisionTone: domain.DecisionTone(c.Decision),
		DecidedByName: c.DecidedByName, DecidedAt: c.DecidedAt, DecisionNote: c.DecisionNote, RecordedBy: c.RecordedBy,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, RowVersion: c.RowVersion,
	}
}
