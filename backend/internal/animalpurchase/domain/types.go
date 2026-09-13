// Package domain holds the Animal purchases vocabulary and validation (maintainer decision
// 2026-09-13).
//
// A purchase LOAD is recorded on the phone by the procurement desk; the ANIMALS on offer in it
// are recorded one at a time, each with an in-app-camera video; the CEO/CXO ACCEPTS or REJECTS
// each one on admin-web. That decision is where this module stops: an accepted candidate is not a
// goat, carries no RFID and joins no procurement_load. Promotion into the herd is a later stage.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	SpeciesGoat  = "goat"
	SpeciesSheep = "sheep"

	SexMale   = "male"
	SexFemale = "female"

	ConditionHealthy      = "healthy"
	ConditionMinorConcern = "minor_concern"
	ConditionUnwell       = "unwell"

	DecisionPending  = "pending"
	DecisionAccepted = "accepted"
	DecisionRejected = "rejected"

	FarmCBE = "CBE"
	FarmCPT = "CPT"

	LoadStatusOpen   = "open"
	LoadStatusClosed = "closed"

	// DefaultPageSize is the phone's screen page; MaxPageSize bounds any caller.
	DefaultPageSize = 20
	MaxPageSize     = 100

	maxLoadRefLength = 32
	maxTextLength    = 500
	maxBreedLength   = 80
	maxTagLength     = 40
	maxAgeMonths     = 240
	maxWeightKg      = 500
)

// Option is one backend-owned choice for a form: the value the write accepts and the label the
// screen shows, rendered verbatim.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Species, Sexes, Conditions and Farms are the closed vocabularies the write path validates
// against and the phone form renders. Labels are farm language; the values never reach a screen.
func Species() []Option {
	return []Option{{SpeciesGoat, "Goat"}, {SpeciesSheep, "Sheep"}}
}

func Sexes() []Option {
	return []Option{{SexFemale, "Female"}, {SexMale, "Male"}}
}

func Conditions() []Option {
	return []Option{
		{ConditionHealthy, "Looks healthy"},
		{ConditionMinorConcern, "Minor concern"},
		{ConditionUnwell, "Looks unwell"},
	}
}

func Farms() []Option {
	return []Option{{FarmCBE, "CBE"}, {FarmCPT, "CPT"}}
}

func labelOf(options []Option, value string) string {
	for _, o := range options {
		if o.Value == value {
			return o.Label
		}
	}
	return strings.ReplaceAll(strings.TrimSpace(value), "_", " ")
}

func SpeciesLabel(v string) string   { return labelOf(Species(), v) }
func SexLabel(v string) string       { return labelOf(Sexes(), v) }
func ConditionLabel(v string) string { return labelOf(Conditions(), v) }

// DecisionLabel is the chip the phone and the web show on a candidate row.
func DecisionLabel(v string) string {
	switch v {
	case DecisionAccepted:
		return "Accepted"
	case DecisionRejected:
		return "Rejected"
	default:
		return "Awaiting decision"
	}
}

// DecisionTone is the chip tone: ok / bad / neutral.
func DecisionTone(v string) string {
	switch v {
	case DecisionAccepted:
		return "ok"
	case DecisionRejected:
		return "bad"
	default:
		return "neutral"
	}
}

// DecisionCounts are whole-load aggregates, never page sums.
type DecisionCounts struct {
	Total    int `json:"total"`
	Pending  int `json:"pending"`
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

// Load is one recorded purchase load.
type Load struct {
	LoadID        string
	TenantID      string
	LoadRef       string
	VendorID      string
	VendorName    string
	ParkID        string
	FarmLabel     string
	ExpectedCount int
	Notes         string
	Status        string
	RecordedBy    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	RowVersion    int
	Counts        DecisionCounts
}

// Candidate is one animal on offer in a load.
type Candidate struct {
	CandidateID   string
	TenantID      string
	LoadID        string
	LoadRef       string
	SeqNo         int
	Species       string
	Sex           string
	Breed         string
	AgeMonths     *int
	WeightKg      *float64
	Condition     string
	TempTag       string
	Notes         string
	VideoProofRef string
	Decision      string
	DecidedBy     string
	DecidedByName string
	DecidedAt     *time.Time
	DecisionNote  string
	RecordedBy    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	RowVersion    int
}

// ValidationError names the field the person must fix; the message is what the form shows.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

// ErrValidation is the sentinel every ValidationError unwraps to.
var ErrValidation = errors.New("animal purchase: validation")

func (e *ValidationError) Unwrap() error { return ErrValidation }

func invalid(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}

// LoadWrite is the add-load form.
type LoadWrite struct {
	LoadRef       string
	VendorID      string
	FarmLabel     string
	ExpectedCount int
	Notes         string
}

// Normalize trims and upper-cases what the write compares on.
func (w *LoadWrite) Normalize() {
	w.LoadRef = strings.TrimSpace(w.LoadRef)
	w.VendorID = strings.TrimSpace(w.VendorID)
	w.FarmLabel = strings.ToUpper(strings.TrimSpace(w.FarmLabel))
	w.Notes = strings.TrimSpace(w.Notes)
}

// Validate is the form's rulebook. Every message names the box to fix.
func (w LoadWrite) Validate() error {
	if w.LoadRef == "" {
		return invalid("load_ref", "Enter the load number.")
	}
	if len(w.LoadRef) > maxLoadRefLength {
		return invalid("load_ref", "The load number is too long.")
	}
	if w.VendorID == "" {
		return invalid("vendor_id", "Pick the vendor this load is bought from.")
	}
	if w.FarmLabel != FarmCBE && w.FarmLabel != FarmCPT {
		return invalid("farm", "Pick the farm this load is for.")
	}
	if w.ExpectedCount < 0 || w.ExpectedCount > 10000 {
		return invalid("expected_count", "Enter roughly how many animals are in the load.")
	}
	if len(w.Notes) > maxTextLength {
		return invalid("notes", "The note is too long.")
	}
	return nil
}

// CandidateWrite is the add-animal form.
type CandidateWrite struct {
	Species       string
	Sex           string
	Breed         string
	AgeMonths     *int
	WeightKg      *float64
	Condition     string
	TempTag       string
	Notes         string
	VideoProofRef string
}

func (w *CandidateWrite) Normalize() {
	w.Species = strings.ToLower(strings.TrimSpace(w.Species))
	w.Sex = strings.ToLower(strings.TrimSpace(w.Sex))
	w.Breed = strings.TrimSpace(w.Breed)
	w.Condition = strings.ToLower(strings.TrimSpace(w.Condition))
	w.TempTag = strings.TrimSpace(w.TempTag)
	w.Notes = strings.TrimSpace(w.Notes)
	w.VideoProofRef = strings.TrimSpace(w.VideoProofRef)
}

func (w CandidateWrite) Validate() error {
	if w.Species != SpeciesGoat && w.Species != SpeciesSheep {
		return invalid("species", "Pick goat or sheep.")
	}
	if w.Sex != SexMale && w.Sex != SexFemale {
		return invalid("sex", "Pick male or female.")
	}
	if len(w.Breed) > maxBreedLength {
		return invalid("breed", "The breed name is too long.")
	}
	if w.AgeMonths != nil && (*w.AgeMonths < 0 || *w.AgeMonths > maxAgeMonths) {
		return invalid("age_months", "Enter the age in months, up to 240.")
	}
	if w.WeightKg != nil && (*w.WeightKg <= 0 || *w.WeightKg >= maxWeightKg) {
		return invalid("weight_kg", "Enter the weight in kg.")
	}
	switch w.Condition {
	case ConditionHealthy, ConditionMinorConcern, ConditionUnwell:
	default:
		return invalid("condition", "Say how the animal looks.")
	}
	if len(w.TempTag) > maxTagLength {
		return invalid("temp_tag", "The temporary tag is too long.")
	}
	if len(w.Notes) > maxTextLength {
		return invalid("notes", "The note is too long.")
	}
	if w.VideoProofRef == "" {
		return invalid("video", "Record a video of the animal before saving.")
	}
	return nil
}

// DecisionWrite is the CEO's accept / reject.
type DecisionWrite struct {
	Decision   string
	Note       string
	RowVersion int
}

func (w *DecisionWrite) Normalize() {
	w.Decision = strings.ToLower(strings.TrimSpace(w.Decision))
	switch w.Decision {
	case "accept":
		w.Decision = DecisionAccepted
	case "reject":
		w.Decision = DecisionRejected
	}
	w.Note = strings.TrimSpace(w.Note)
}

func (w DecisionWrite) Validate() error {
	if w.Decision != DecisionAccepted && w.Decision != DecisionRejected {
		return invalid("decision", "Choose Accept or Reject.")
	}
	if len(w.Note) > maxTextLength {
		return invalid("note", "The note is too long.")
	}
	if w.RowVersion <= 0 {
		return invalid("row_version", "Reload and try again.")
	}
	return nil
}

// CandidateTitle is the row's name on both surfaces: "Animal 7 · Female goat".
func CandidateTitle(c Candidate) string {
	return fmt.Sprintf("Animal %d · %s %s", c.SeqNo, SexLabel(c.Sex), strings.ToLower(SpeciesLabel(c.Species)))
}

// LoadTitle is the load row's name: "Load 132 · Vendor".
func LoadTitle(l Load) string {
	if strings.TrimSpace(l.VendorName) == "" {
		return "Load " + l.LoadRef
	}
	return "Load " + l.LoadRef + " · " + l.VendorName
}
