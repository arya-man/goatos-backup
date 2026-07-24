package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// BUG-017 — pre-arrival accepted-history ingestion.
//
// Procurement writes a supplier-attested vaccination card onto the PC handoff and forwards it in
// the `goat.created` payload key `trusted_vaccination_history`. Nothing consumed it, so a procured
// animal with a genuine prior ET+TT course was scheduled from scratch and re-injected.
//
// The claims cannot be pushed through `procurement_hf_vaccination_evidence`: that channel's trust
// gate demands a completed proof artifact and a 28-35 day holding-farm warm-up window containing
// the administration date, none of which a pre-arrival supplier claim has. Writing
// `review_status='trusted'` there without a proof artifact is exactly the "supplier claim silently
// becomes accepted history" failure. So the claims land in their OWN reviewed channel
// (`vaccination_prearrival_history_entries`), and only rows that survive review become history.
//
// Persistence — not a per-run in-memory pass-through — is load bearing: the same goat is
// re-generated on every later `goat.stage_changed` / `goat.location.changed` / `goat.health.changed`
// recheck. An in-memory suppression would evaporate on the next recheck and silently re-create the
// duplicate doses while looking fixed.

// PreArrivalHistoryWriter persists a goat's reviewed pre-arrival claim set. Implemented by the
// vaccination Postgres repository; discovered off the injected GoatLister exactly like the
// cross-vaccine history readers.
type PreArrivalHistoryWriter interface {
	IngestPreArrivalHistory(ctx context.Context, in domain.PreArrivalHistoryIngest) (domain.PreArrivalHistoryIngestResult, error)
}

// Rejection reasons. Every one of them is durable (persisted with the entry) and none of them
// suppresses due work: a rejected claim leaves the animal on its full protocol course.
const (
	preArrivalRejectUnparseable     = "claim_unparseable"
	preArrivalRejectMissingDose     = "missing_vaccine_or_dose_code"
	preArrivalRejectMissingDate     = "missing_or_invalid_administered_at"
	preArrivalRejectFutureDate      = "administered_at_in_future"
	preArrivalRejectAfterArrival    = "administered_at_after_arrival"
	preArrivalRejectUnknownDose     = "no_published_rule_for_vaccine_dose"
	preArrivalRejectPathMismatch    = "dose_not_on_classified_schedule_path"
	preArrivalRejectCourseOutOfOrde = "course_dose_order_violation"
)

// goatCreatedPayload is the slice of the procurement/identity `goat.created` payload vaccination
// consumes. Unknown keys are intentionally tolerated: the producer's OpenAPI schema declares
// `trusted_vaccination_history` items as free-form objects, and the envelope carries producer
// fields (load_id, park_id, ...) this consumer has no business rejecting.
type goatCreatedPayload struct {
	GoatID                    string            `json:"goat_id"`
	EntryDate                 string            `json:"entry_date"`
	ActorID                   string            `json:"actor_id"`
	TrustedVaccinationHistory []json.RawMessage `json:"trusted_vaccination_history"`
}

// preArrivalClaim is the consumed shape of one supplier-claimed administration.
type preArrivalClaim struct {
	VaccineCode    string `json:"vaccine_code"`
	VaccineName    string `json:"vaccine_name"`
	DoseCode       string `json:"dose_code"`
	Sequence       int32  `json:"sequence"`
	AdministeredAt string `json:"administered_at"`
}

// resolvedPreArrivalRule is a claim's match against a published rule in one effective version.
type resolvedPreArrivalRule struct {
	versionID string
	rule      protodomain.Rule
	vaccine   vaccineProfile
	path      string
}

// ingestPreArrivalHistory reviews and durably persists the `trusted_vaccination_history` claims on
// a `goat.created` event BEFORE generation runs, so the generation engine (which is already correct
// once history exists) sees the accepted anchors.
//
// It is a no-op when the payload carries no claims. When claims ARE present but no writer is wired
// it fails loudly rather than silently discarding medical history — a silent skip here is worse
// than the open bug because it reads as handled.
func (s *GenerationService) ingestPreArrivalHistory(ctx context.Context, tenantID, goatID, eventID string, rawPayload []byte, asOf time.Time) error {
	claims, entryDate, actorID, err := parseGoatCreatedPreArrivalClaims(rawPayload)
	if err != nil {
		return err
	}
	if len(claims) == 0 {
		return nil
	}
	if s.preArrival == nil {
		return fmt.Errorf("vaccination: goat.created for goat %s carries %d pre-arrival vaccination claims but no pre-arrival history writer is wired; refusing to discard supplier-attested history", goatID, len(claims))
	}

	g, found, err := s.goats.GetGoatForGeneration(ctx, tenantID, goatID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("vaccination: goat.created for unknown goat %s carries pre-arrival vaccination claims; refusing to discard them", goatID)
	}
	// The payload's entry_date is the producer's arrival fact; the goat row is authoritative when set.
	if g.EntryDate != nil {
		entryDate = g.EntryDate
	}

	versionIDs, err := s.proto.ListEffectiveVaccinationVersionsForGoat(ctx, tenantID, g.ParkID, asOf)
	if err != nil {
		return err
	}
	plans := make(map[string]cachedVersionPlan, len(versionIDs))
	if err := s.loadVersionPlans(ctx, tenantID, versionIDs, plans); err != nil {
		return err
	}

	entries, err := reviewPreArrivalClaims(g, versionIDs, plans, claims, entryDate, asOf, preArrivalIdempotencyBase(eventID, tenantID, goatID))
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	in := domain.PreArrivalHistoryIngest{
		TenantID:      tenantID,
		GoatID:        goatID,
		SourceSystem:  domain.PreArrivalHistorySourceProcurementHandoff,
		SourceEventID: strings.TrimSpace(eventID),
		ReviewedAt:    asOf.UTC(),
		Entries:       entries,
	}
	if actor := strings.TrimSpace(actorID); actor != "" {
		in.ReviewedBy = &actor
	}
	_, err = s.preArrival.IngestPreArrivalHistory(ctx, in)
	return err
}

// preArrivalIdempotencyBase derives the stable operation identity for one delivery of one
// goat.created event. The producer's durable event id is preferred; without it (in-process
// publishes and older envelopes) the tenant+goat identity keeps an exact replay idempotent while
// making a CHANGED claim set for the same goat a same-key/different-payload conflict rather than a
// silent second history write.
func preArrivalIdempotencyBase(eventID, tenantID, goatID string) string {
	if id := strings.TrimSpace(eventID); id != "" {
		return "event:" + id
	}
	return "goat:" + tenantID + ":" + goatID
}

func parseGoatCreatedPreArrivalClaims(rawPayload []byte) ([]json.RawMessage, *time.Time, string, error) {
	if len(strings.TrimSpace(string(rawPayload))) == 0 {
		return nil, nil, "", nil
	}
	var p goatCreatedPayload
	if err := json.Unmarshal(rawPayload, &p); err != nil {
		return nil, nil, "", fmt.Errorf("vaccination: decode goat.created payload: %w", err)
	}
	claims := make([]json.RawMessage, 0, len(p.TrustedVaccinationHistory))
	for _, claim := range p.TrustedVaccinationHistory {
		if len(strings.TrimSpace(string(claim))) == 0 || strings.TrimSpace(string(claim)) == "null" {
			continue
		}
		claims = append(claims, claim)
	}
	var entryDate *time.Time
	if raw := strings.TrimSpace(p.EntryDate); raw != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", raw, biztime.DefaultLocation()); err == nil {
			entryDate = &parsed
		}
	}
	return claims, entryDate, strings.TrimSpace(p.ActorID), nil
}

// reviewPreArrivalClaims validates every claim and returns one persistable entry per claim —
// accepted or rejected, never dropped.
//
// Classification rule (locked): the schedule path comes from the goat's DOB / entry date /
// management stage as INDEPENDENT evidence, evaluated with an EMPTY history. The claim's own dose
// code (`K1`/`K2`, `origin=birth`, `..._kid_...`) must never be the evidence for its own path,
// otherwise a stale supplier tag would elect the kid course for an adult animal.
func reviewPreArrivalClaims(
	g domain.EligibleGoat,
	versionIDs []string,
	plans map[string]cachedVersionPlan,
	claims []json.RawMessage,
	entryDate *time.Time,
	asOf time.Time,
	idempotencyBase string,
) ([]domain.PreArrivalHistoryEntry, error) {
	pathByVersion := make(map[string]string, len(versionIDs))
	for _, versionID := range versionIDs {
		plan, ok := plans[versionID]
		if !ok {
			continue
		}
		pathByVersion[versionID] = schedulePathForGoat(g, plan.policies.Procurement, asOf, nil)
	}
	// A goat with no effective version still gets a durable rejected record per claim; the fallback
	// path only labels the row.
	fallbackPath := SchedulePathForGoat(g, SchedulePathProcurementPolicy{}, asOf, nil)

	entries := make([]domain.PreArrivalHistoryEntry, 0, len(claims))
	accepted := make([]resolvedPreArrivalRule, 0, len(claims))
	acceptedIndex := make([]int, 0, len(claims))

	for i, raw := range claims {
		fingerprint, err := preArrivalClaimFingerprint(raw)
		if err != nil {
			return nil, err
		}
		entry := domain.PreArrivalHistoryEntry{
			SchedulePath:       fallbackPath,
			Claim:              append([]byte(nil), raw...),
			IdempotencyKey:     preArrivalEntryKey(idempotencyBase, i),
			RequestFingerprint: fingerprint,
			ReviewStatus:       domain.PreArrivalHistoryReviewRejected,
		}

		var claim preArrivalClaim
		if err := json.Unmarshal(raw, &claim); err != nil {
			entry.RejectionReason = preArrivalRejectUnparseable
			entries = append(entries, entry)
			continue
		}
		vaccineCode := strings.TrimSpace(claim.VaccineCode)
		if vaccineCode == "" {
			vaccineCode = strings.TrimSpace(claim.VaccineName)
		}
		doseCode := strings.TrimSpace(claim.DoseCode)
		entry.VaccineCode = vaccineCode
		entry.DoseCode = doseCode
		entry.Sequence = claim.Sequence
		if vaccineCode == "" || doseCode == "" {
			entry.RejectionReason = preArrivalRejectMissingDose
			entries = append(entries, entry)
			continue
		}
		administeredAt, ok := parsePreArrivalAdministeredAt(claim.AdministeredAt)
		if !ok {
			entry.RejectionReason = preArrivalRejectMissingDate
			entries = append(entries, entry)
			continue
		}
		entry.AdministeredAt = administeredAt
		if administeredAt.After(asOf) {
			entry.RejectionReason = preArrivalRejectFutureDate
			entries = append(entries, entry)
			continue
		}
		if entryDate != nil && businessDayStart(administeredAt).After(businessDayStart(*entryDate)) {
			// Pre-arrival history is, by definition, given before the animal reached the farm.
			// Anything later belongs to an in-farm completion or the proof-backed holding-farm
			// channel, not to an unproven supplier claim.
			entry.RejectionReason = preArrivalRejectAfterArrival
			entries = append(entries, entry)
			continue
		}

		match, matchedDose, err := resolvePreArrivalRule(versionIDs, plans, pathByVersion, vaccineCode, doseCode)
		if err != nil {
			return nil, err
		}
		if !matchedDose {
			entry.RejectionReason = preArrivalRejectUnknownDose
			entries = append(entries, entry)
			continue
		}
		if match == nil {
			entry.RejectionReason = preArrivalRejectPathMismatch
			entries = append(entries, entry)
			continue
		}

		entry.SchedulePath = match.path
		entry.ProtocolVersionID = match.versionID
		entry.RuleID = match.rule.RuleID
		entry.VaccineCode = match.vaccine.Code
		entry.DoseCode = match.rule.DoseCode
		entry.Sequence = match.rule.Sequence
		entry.ReviewStatus = domain.PreArrivalHistoryReviewAccepted
		entry.RejectionReason = ""
		entries = append(entries, entry)
		accepted = append(accepted, *match)
		acceptedIndex = append(acceptedIndex, len(entries)-1)
	}

	demotePreArrivalCourseOrderViolations(entries, accepted, acceptedIndex)
	return entries, nil
}

// demotePreArrivalCourseOrderViolations rejects an accepted claim set whose course order
// contradicts the published rule order: within one vaccine family of one protocol version, a
// later primary-course dose cannot have been given before an earlier one. A card that says so is
// protocol-impossible, and accepting it would anchor the 182-day repeat off the wrong dose.
func demotePreArrivalCourseOrderViolations(entries []domain.PreArrivalHistoryEntry, accepted []resolvedPreArrivalRule, acceptedIndex []int) {
	type courseDose struct {
		entryIdx int
		offset   int32
		sequence int32
		at       time.Time
	}
	byFamily := make(map[string][]courseDose)
	for i, match := range accepted {
		if !isPrimaryAnchorRule(match.rule) {
			continue
		}
		key := match.versionID + "\x00" + strings.ToLower(strings.TrimSpace(match.vaccine.Code))
		byFamily[key] = append(byFamily[key], courseDose{
			entryIdx: acceptedIndex[i],
			offset:   match.rule.OffsetDays,
			sequence: match.rule.Sequence,
			at:       entries[acceptedIndex[i]].AdministeredAt,
		})
	}
	for _, doses := range byFamily {
		if len(doses) < 2 {
			continue
		}
		sort.SliceStable(doses, func(a, b int) bool {
			if doses[a].offset != doses[b].offset {
				return doses[a].offset < doses[b].offset
			}
			return doses[a].sequence < doses[b].sequence
		})
		for i := 1; i < len(doses); i++ {
			if doses[i].at.Before(doses[i-1].at) {
				entry := &entries[doses[i].entryIdx]
				entry.ReviewStatus = domain.PreArrivalHistoryReviewRejected
				entry.RejectionReason = preArrivalRejectCourseOutOfOrde
				entry.ProtocolVersionID = ""
				entry.RuleID = ""
			}
		}
	}
}

// resolvePreArrivalRule finds the published rule a claim names. It returns matchedDose=true when
// SOME published rule carries that vaccine+dose code, and a non-nil match only when such a rule is
// also on the goat's independently classified schedule path.
func resolvePreArrivalRule(
	versionIDs []string,
	plans map[string]cachedVersionPlan,
	pathByVersion map[string]string,
	vaccineCode, doseCode string,
) (*resolvedPreArrivalRule, bool, error) {
	matchedDose := false
	for _, versionID := range versionIDs {
		plan, ok := plans[versionID]
		if !ok {
			continue
		}
		path := pathByVersion[versionID]
		for _, rule := range plan.rules {
			if !strings.EqualFold(strings.TrimSpace(rule.DoseCode), doseCode) {
				continue
			}
			_, ruleVaccine, err := ruleGenerationContext(rule, plan.eligibility, plan.vaccineProfile)
			if err != nil {
				return nil, false, err
			}
			if !strings.EqualFold(strings.TrimSpace(ruleVaccine.Code), vaccineCode) {
				continue
			}
			matchedDose = true
			if !ruleMatchesSchedulePath(rule, path) {
				continue
			}
			match := resolvedPreArrivalRule{versionID: versionID, rule: rule, vaccine: ruleVaccine, path: path}
			return &match, true, nil
		}
	}
	return nil, matchedDose, nil
}

func parsePreArrivalAdministeredAt(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), true
	}
	if parsed, err := time.ParseInLocation("2006-01-02", value, biztime.DefaultLocation()); err == nil {
		return parsed.UTC(), true
	}
	return time.Time{}, false
}

func preArrivalEntryKey(base string, ordinal int) string {
	return fmt.Sprintf("vaccination.prearrival:%s:%d", base, ordinal)
}

// preArrivalClaimFingerprint is the semantic request fingerprint. The claim is decoded and
// re-marshalled so key order and insignificant whitespace cannot change the fingerprint, while any
// change to a value (a corrected date, a different dose) does.
func preArrivalClaimFingerprint(raw json.RawMessage) (string, error) {
	var canonical any
	if err := json.Unmarshal(raw, &canonical); err != nil {
		// An unparseable claim still gets a stable fingerprint over its literal bytes.
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:]), nil
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("vaccination: canonicalize pre-arrival claim: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
