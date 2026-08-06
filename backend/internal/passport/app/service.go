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

// LocationReader resolves a goat's current ground location. Passport owns no location data itself
// (goats/locations/goat_shed_partitions belong to the org/herd tables, not to passport); this is a
// read-only port so the adapter can be swapped without passport depending on another module's
// package, matching the pattern vaccination/counts already use for the same tables.
type LocationReader interface {
	GoatLocation(ctx context.Context, tenantID, goatID string) (oploc.OperationalLocation, bool, error)
}

// Service builds a goat passport from the vaccination + obligation reads.
type Service struct {
	vacc VaccinationReader
	obl  ObligationReader
	loc  LocationReader
}

// NewService wires the readers. loc may be nil (e.g. in tests that do not exercise location); a nil
// reader simply leaves the passport's location fields empty rather than erroring, so a goat's
// vaccination history is never blocked on location resolution.
func NewService(vacc VaccinationReader, obl ObligationReader, loc LocationReader) *Service {
	return &Service{vacc: vacc, obl: obl, loc: loc}
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
}

// LastDose is a goat's most recent accepted administration (API DTO).
type LastDose struct {
	CompletionID   string    `json:"completion_id"`
	ObligationID   string    `json:"obligation_id"`
	AdministeredAt time.Time `json:"administered_at"`
}

// Passport is the aggregated read model for one goat.
type Passport struct {
	GoatID   string `json:"goat_id"`
	ParkID   string `json:"park_id,omitempty"`
	ParkName string `json:"park_name,omitempty"`
	ShedID   string `json:"shed_id,omitempty"`
	ShedName string `json:"shed_name,omitempty"`
	// PartitionLabel is the raw stored partition label ("2", "Part 3"), or "" when the shed is not
	// partitioned or the goat's location could not be resolved. Never the "whole" sentinel.
	PartitionLabel string `json:"partition_label,omitempty"`
	// OperationalLocationDisplay is oploc.OperationalLocation.Display(): "Castro 2" for a partition,
	// bare "Yashoda" for a non-partitioned shed, "" if location could not be resolved.
	OperationalLocationDisplay string        `json:"operational_location_display,omitempty"`
	NextDue                    *DueItem      `json:"next_due"`
	OpenObligations            []DueItem     `json:"open_obligations"`
	LastAccepted               *LastDose     `json:"last_accepted"`
	VaccinationHistory         []HistoryItem `json:"vaccination_history"`
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
	if s.loc != nil {
		if loc, found, err := s.loc.GoatLocation(ctx, tenantID, goatID); err != nil {
			return Passport{}, err
		} else if found {
			p.ParkID = loc.ParkID
			p.ParkName = loc.ParkName
			p.ShedID = loc.ShedID
			p.ShedName = loc.ShedName
			p.PartitionLabel = loc.PartitionLabel
			p.OperationalLocationDisplay = loc.Display()
		}
	}
	for _, o := range open {
		p.OpenObligations = append(p.OpenObligations, DueItem{
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
		})
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
