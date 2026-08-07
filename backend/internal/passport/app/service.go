// Package app assembles a goat's vaccination passport: history, open obligations + next due, and the
// last accepted dose. It is a read-only aggregator over the vaccination and obligation modules; it
// owns no tables and exposes API-shaped DTOs (decoupled from each module's domain types).
package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// VaccinationReader is the slice of the vaccination service the passport needs.
type VaccinationReader interface {
	GoatHistory(ctx context.Context, tenantID, goatID string, limit int32) ([]vaccdomain.CompletionHistoryItem, error)
	LastAccepted(ctx context.Context, tenantID, goatID string) (vaccdomain.LastAccepted, bool, error)
}

// ObligationReader is the slice of the obligation repo the passport needs.
type ObligationReader interface {
	ListOpenByGoat(ctx context.Context, tenantID, goatID string, limit int32) ([]obldomain.OpenObligation, error)
}

// LocationResolver resolves a shed to its operational location (name + partition).
type LocationResolver interface {
	ResolveShedLocation(ctx context.Context, tenantID, shedID string) (oploc.OperationalLocation, error)
}

// Service builds a goat passport from the vaccination + obligation reads.
type Service struct {
	vacc VaccinationReader
	obl  ObligationReader
	locResolver LocationResolver
}

// NewService wires the readers and location resolver.
func NewService(vacc VaccinationReader, obl ObligationReader, locResolver LocationResolver) *Service {
	return &Service{vacc: vacc, obl: obl, locResolver: locResolver}
}

// DueItem is one open obligation in the passport (API DTO).
type DueItem struct {
	ObligationID      string     `json:"obligation_id"`
	ProtocolVersionID string     `json:"protocol_version_id"`
	RuleID            string     `json:"rule_id"`
	BatchID           string     `json:"batch_id,omitempty"`
	WorkflowRowID     string     `json:"workflow_row_id"`
	Status            string     `json:"status"`
	DueAt             time.Time  `json:"due_at"`
	ClinicalDueAt     time.Time  `json:"clinical_due_at"`
	ScheduledFor      *time.Time `json:"scheduled_for,omitempty"`
	Sequence          int32      `json:"sequence"`
	DoseCode          string     `json:"dose_code"`
	VaccineLabel      string     `json:"vaccine_label"`
	DisplayLabel      string     `json:"display_label"`
	ShedID            string     `json:"shed_id,omitempty"`
	ShedName          string     `json:"shed_name,omitempty"`
	PartitionLabel    string     `json:"partition_label,omitempty"`
	OperationalLocationDisplay string `json:"operational_location_display,omitempty"`
}

// HistoryItem is one administered/verified dose in the passport (API DTO).
type HistoryItem struct {
	CompletionID    string     `json:"completion_id"`
	ObligationID    string     `json:"obligation_id"`
	BatchID         string     `json:"batch_id,omitempty"`
	Status          string     `json:"status"`
	RouteSite       string     `json:"route_site,omitempty"`
	AdministeredAt  time.Time  `json:"administered_at"`
	Doses           int32      `json:"doses"`
	DoseCode        string     `json:"dose_code"`
	VaccineLabel    string     `json:"vaccine_label"`
	DisplayLabel    string     `json:"display_label"`
	AdverseReaction bool       `json:"adverse_reaction"`
	WithdrawalUntil *time.Time `json:"withdrawal_until,omitempty"`
	ShedID          string     `json:"shed_id,omitempty"`
	ShedName        string     `json:"shed_name,omitempty"`
	PartitionLabel  string     `json:"partition_label,omitempty"`
	OperationalLocationDisplay string `json:"operational_location_display,omitempty"`
}

// LastDose is a goat's most recent accepted administration (API DTO).
type LastDose struct {
	CompletionID   string    `json:"completion_id"`
	ObligationID   string    `json:"obligation_id"`
	AdministeredAt time.Time `json:"administered_at"`
}

// Passport is the aggregated read model for one goat.
type Passport struct {
	GoatID             string        `json:"goat_id"`
	NextDue            *DueItem      `json:"next_due"`
	OpenObligations    []DueItem     `json:"open_obligations"`
	LastAccepted       *LastDose     `json:"last_accepted"`
	VaccinationHistory []HistoryItem `json:"vaccination_history"`
}

const passportLimit = 200

// GetPassport assembles the passport for a goat. Empty sections are returned as empty arrays so the
// API shape is stable.
func (s *Service) GetPassport(ctx context.Context, tenantID, goatID string) (Passport, error) {
	history, err := s.vacc.GoatHistory(ctx, tenantID, goatID, passportLimit)
	if err != nil {
		return Passport{}, err
	}
	open, err := s.obl.ListOpenByGoat(ctx, tenantID, goatID, passportLimit)
	if err != nil {
		return Passport{}, err
	}
	last, found, err := s.vacc.LastAccepted(ctx, tenantID, goatID)
	if err != nil {
		return Passport{}, err
	}

	p := Passport{
		GoatID:             goatID,
		OpenObligations:    make([]DueItem, 0, len(open)),
		VaccinationHistory: make([]HistoryItem, 0, len(history)),
	}
	// Cache location resolutions to avoid duplicate queries for the same shed
	locCache := make(map[string]oploc.OperationalLocation)
	for _, o := range open {
		due := DueItem{
			ObligationID:      o.ObligationID,
			ProtocolVersionID: o.ProtocolVersionID,
			RuleID:            o.RuleID,
			BatchID:           o.BatchID,
			WorkflowRowID:     workflowRowID(o),
			Status:            o.Status,
			DueAt:             o.DueAt,
			ClinicalDueAt:     o.ClinicalDueAt,
			ScheduledFor:      o.ScheduledFor,
			Sequence:          o.Sequence,
			DoseCode:          o.DoseCode,
			VaccineLabel:      o.VaccineLabel,
			DisplayLabel:      vaccineDisplayLabel(o.VaccineLabel, o.DoseCode, o.Sequence),
		}
		// Populate location if obligation is shed-scoped
		if o.ScopeType == "shed" && o.ScopeID != "" {
			due.ShedID = o.ScopeID
			// Check cache first
			loc, cached := locCache[o.ScopeID]
			if !cached {
				// Resolve and cache (cache prevents duplicate queries for same shed)
				// scale-guard:ignore: cached resolution + passportLimit=200 bounded + low-volume read-only aggregator
				resolved, err := s.locResolver.ResolveShedLocation(ctx, tenantID, o.ScopeID)
				if err == nil {
					loc = resolved
					locCache[o.ScopeID] = loc
				} else {
					// On error, leave location fields empty; degradation is acceptable
					loc = oploc.OperationalLocation{}
				}
			}
			due.ShedName = loc.ShedName
			due.PartitionLabel = loc.PartitionLabel
			due.OperationalLocationDisplay = loc.Display()
		}
		p.OpenObligations = append(p.OpenObligations, due)
	}
	if len(p.OpenObligations) > 0 {
		next := p.OpenObligations[0] // earliest due first
		p.NextDue = &next
	}
	for _, h := range history {
		p.VaccinationHistory = append(p.VaccinationHistory, HistoryItem{
			CompletionID:    h.CompletionID,
			ObligationID:    h.ObligationID,
			BatchID:         h.BatchID,
			Status:          h.Status,
			RouteSite:       h.RouteSite,
			AdministeredAt:  h.AdministeredAt,
			Doses:           h.Doses,
			DoseCode:        h.DoseCode,
			VaccineLabel:    h.VaccineLabel,
			DisplayLabel:    vaccineDisplayLabel(h.VaccineLabel, h.DoseCode, 0),
			AdverseReaction: h.AdverseReaction,
			WithdrawalUntil: h.WithdrawalUntilDate,
		})
	}
	if found {
		p.LastAccepted = &LastDose{
			CompletionID:   last.CompletionID,
			ObligationID:   last.ObligationID,
			AdministeredAt: last.AdministeredAt,
		}
	}
	return p, nil
}

func vaccineDisplayLabel(vaccineLabel, doseCode string, sequence int32) string {
	label := readableVaccineLabel(vaccineLabel)
	if label == "" {
		label = readableVaccineLabel(doseCode)
	}
	if label == "" {
		return ""
	}
	if wave := doseWaveLabel(doseCode); wave != "" {
		return fmt.Sprintf("%s %s", label, wave)
	}
	if sequence > 0 && doseCode == "" {
		return fmt.Sprintf("%s W%d", label, sequence)
	}
	return label
}

var doseWavePattern = regexp.MustCompile(`(?i)(?:^|_)w([0-9]+)$`)

func doseWaveLabel(doseCode string) string {
	match := doseWavePattern.FindStringSubmatch(strings.TrimSpace(doseCode))
	if len(match) != 2 {
		return ""
	}
	return "W" + match[1]
}

func readableVaccineLabel(label string) string {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" {
		return ""
	}
	replacer := strings.NewReplacer("ET_TT", "ET+TT", "et_tt", "ET+TT", "_", " ")
	return strings.TrimSpace(replacer.Replace(trimmed))
}

func workflowRowID(o obldomain.OpenObligation) string {
	if o.BatchID != "" && o.RuleID != "" && o.ScopeType == "shed" && o.ScopeID != "" {
		return fmt.Sprintf("batch:%s:rule:%s:shed:%s", o.BatchID, o.RuleID, o.ScopeID)
	}
	return "obligation:" + o.ObligationID
}
