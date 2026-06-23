package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// ProtocolReader is the slice of the protocol repo SM-1 generation needs.
type ProtocolReader interface {
	GetVersion(ctx context.Context, tenantID, versionID string) (protodomain.Version, error)
	ListRules(ctx context.Context, tenantID, versionID string) ([]protodomain.Rule, error)
}

// GoatLister is the chunked eligible-goat source (the vaccination repo).
type GoatLister interface {
	ListEligibleGoatsForGeneration(ctx context.Context, f domain.ImpactFilter, afterGoatID string, limit int32) ([]domain.EligibleGoat, error)
}

// ObligationWriter is the slice of the obligation repo SM-1 generation needs.
type ObligationWriter interface {
	InsertObligation(ctx context.Context, in obldomain.NewObligation) (string, bool, error)
	RecordStatusEvent(ctx context.Context, ev obldomain.NewStatusEvent) (string, bool, error)
}

// GenerationService implements SM-1: expand a published protocol version's rules into per-goat
// obligations over the in-care cohort, idempotently, deferring (visibly) ICU/quarantine/sick goats.
type GenerationService struct {
	proto ProtocolReader
	goats GoatLister
	obl   ObligationWriter
	page  int32
}

// NewGenerationService wires the three repos.
func NewGenerationService(proto ProtocolReader, goats GoatLister, obl ObligationWriter) *GenerationService {
	return &GenerationService{proto: proto, goats: goats, obl: obl, page: 500}
}

type genEligibility struct {
	Stage       string   `json:"stage"`
	Sex         string   `json:"sex"`
	Breed       string   `json:"breed"`
	Health      string   `json:"health"`
	DeferStates []string `json:"defer_states"`
}

type genDSL struct {
	Eligibility genEligibility `json:"eligibility"`
}

// normDim maps "all"/"any"/"" to "" (no filter); any other value is an exact-match dimension.
func normDim(v string) string {
	switch v {
	case "", "all", "any":
		return ""
	default:
		return v
	}
}

// GenerateForVersion generates obligations for every applicable rule of a PUBLISHED version across
// the eligible in-care cohort. Idempotent (deterministic key → ON CONFLICT no-op). Returns counts.
func (s *GenerationService) GenerateForVersion(ctx context.Context, tenantID, versionID string, asOf time.Time) (domain.GenerateResult, error) {
	var res domain.GenerateResult

	v, err := s.proto.GetVersion(ctx, tenantID, versionID)
	if err != nil {
		return res, err
	}
	if v.Status != "published" {
		return res, fmt.Errorf("vaccination: generate requires a published version, got status %q", v.Status)
	}
	rules, err := s.proto.ListRules(ctx, tenantID, versionID)
	if err != nil {
		return res, err
	}

	var dsl genDSL
	if len(v.RuleDsl) > 0 {
		_ = json.Unmarshal(v.RuleDsl, &dsl)
	}
	elig := dsl.Eligibility
	filter := domain.ImpactFilter{
		TenantID: tenantID,
		Stage:    normDim(elig.Stage),
		Sex:      normDim(elig.Sex),
		Breed:    normDim(elig.Breed),
	}
	if v.ScopeType == "park" && v.ScopeID != "" {
		park := v.ScopeID
		filter.ParkID = &park
	}
	defersEnabled := len(elig.DeferStates) > 0

	after := ""
	for {
		goats, err := s.goats.ListEligibleGoatsForGeneration(ctx, filter, after, s.page)
		if err != nil {
			return res, err
		}
		if len(goats) == 0 {
			break
		}
		for _, g := range goats {
			for _, rule := range rules {
				due, ok, skip := dueAt(rule, g, asOf)
				if skip {
					res.SkippedNoDueDate++
					continue
				}
				if !ok {
					continue // trigger handled elsewhere (after_previous_completion → SM-7, manual_campaign)
				}
				scopeType, scopeID := "tenant", tenantID
				if g.ParkID != "" {
					scopeType, scopeID = "park", g.ParkID
				}
				key := obligationKey(tenantID, versionID, rule.RuleID, "goat", g.GoatID, due.UTC().Format(time.RFC3339), strconv.Itoa(int(rule.Sequence)))
				obID, applied, err := s.obl.InsertObligation(ctx, obldomain.NewObligation{
					TenantID:          tenantID,
					ProtocolVersionID: versionID,
					RuleID:            rule.RuleID,
					TargetType:        "goat",
					TargetID:          g.GoatID,
					ScopeType:         scopeType,
					ScopeID:           scopeID,
					DueAt:             due,
					Status:            "scheduled",
					IdempotencyKey:    key,
					Sequence:          rule.Sequence,
				})
				if err != nil {
					return res, err
				}
				if !applied {
					continue // replay no-op
				}
				res.Generated++

				if defersEnabled && g.LifecycleStatus != "alive" {
					// Visible deferred obligation: it exists (scheduled) but a deferred event explains
					// why it will not fire until the goat recovers. Never a silent skip.
					payload, _ := json.Marshal(map[string]string{"reason": "defer_state", "lifecycle_status": g.LifecycleStatus})
					if _, _, err := s.obl.RecordStatusEvent(ctx, obldomain.NewStatusEvent{
						TenantID:       tenantID,
						ObligationID:   obID,
						EventType:      "deferred",
						OccurredAt:     asOf,
						Payload:        payload,
						IdempotencyKey: obID + ":deferred",
						Scope:          "obligation.status_event",
						RequestHash:    "defer:" + g.LifecycleStatus,
					}); err != nil {
						return res, err
					}
					res.Deferred++
				}
			}
		}
		if int32(len(goats)) < s.page {
			break
		}
		after = goats[len(goats)-1].GoatID
	}
	return res, nil
}

// dueAt computes a rule's due date for a goat. ok=false with skip=false means the trigger is not an
// SM-1 trigger (after_previous_completion → SM-7, manual_campaign). skip=true means an SM-1 trigger
// that cannot be scheduled for this goat (missing dob/entry_date).
func dueAt(rule protodomain.Rule, g domain.EligibleGoat, asOf time.Time) (due time.Time, ok bool, skip bool) {
	off := time.Duration(rule.OffsetDays) * 24 * time.Hour
	switch rule.TriggerType {
	case "birth_age":
		if g.DOB == nil {
			return time.Time{}, false, true
		}
		return g.DOB.Add(off), true, false
	case "post_arrival":
		if g.EntryDate == nil {
			return time.Time{}, false, true
		}
		return g.EntryDate.Add(off), true, false
	case "calendar":
		return asOf.Add(off), true, false
	default: // after_previous_completion (SM-7), manual_campaign
		return time.Time{}, false, false
	}
}

func obligationKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte("|"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
