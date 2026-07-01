// Package app assembles a goat's vaccination passport: history, open obligations + next due, and the
// last accepted dose. It is a read-only aggregator over the vaccination and obligation modules; it
// owns no tables and exposes API-shaped DTOs (decoupled from each module's domain types).
package app

import (
	"context"
	"fmt"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
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

// Service builds a goat passport from the vaccination + obligation reads.
type Service struct {
	vacc VaccinationReader
	obl  ObligationReader
}

// NewService wires the readers.
func NewService(vacc VaccinationReader, obl ObligationReader) *Service {
	return &Service{vacc: vacc, obl: obl}
}

// DueItem is one open obligation in the passport (API DTO).
type DueItem struct {
	ObligationID      string    `json:"obligation_id"`
	ProtocolVersionID string    `json:"protocol_version_id"`
	RuleID            string    `json:"rule_id"`
	BatchID           string    `json:"batch_id,omitempty"`
	WorkflowRowID     string    `json:"workflow_row_id"`
	Status            string    `json:"status"`
	DueAt             time.Time `json:"due_at"`
	Sequence          int32     `json:"sequence"`
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
	for _, o := range open {
		p.OpenObligations = append(p.OpenObligations, DueItem{
			ObligationID:      o.ObligationID,
			ProtocolVersionID: o.ProtocolVersionID,
			RuleID:            o.RuleID,
			BatchID:           o.BatchID,
			WorkflowRowID:     workflowRowID(o),
			Status:            o.Status,
			DueAt:             o.DueAt,
			Sequence:          o.Sequence,
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

func workflowRowID(o obldomain.OpenObligation) string {
	if o.BatchID != "" && o.RuleID != "" && o.ScopeType == "shed" && o.ScopeID != "" {
		return fmt.Sprintf("batch:%s:rule:%s:shed:%s", o.BatchID, o.RuleID, o.ScopeID)
	}
	return "obligation:" + o.ObligationID
}
