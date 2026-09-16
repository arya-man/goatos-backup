package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

var (
	// ErrShiftingCancelReasonRequired is returned when a cancellation arrives without a reason.
	ErrShiftingCancelReasonRequired = errors.New("counts: a reason is required to cancel a shifting")
	// ErrVerificationEnqueuerNotWired is returned when a completion cannot enqueue its verification
	// item because the enqueue seam was never wired -- a composition bug, surfaced loudly rather than
	// silently stranding a pending_verification movement.
	ErrVerificationEnqueuerNotWired = errors.New("counts: shifting verification enqueuer is not wired")
	// ErrInvalidShiftingExecutionFilter is returned for a malformed cursor, business date, or status.
	ErrInvalidShiftingExecutionFilter = errors.New("counts: invalid pending-execution filter")
)

const (
	ShiftingActionStatusAll        = "all"
	ShiftingActionStatusPending    = "pending"
	ShiftingActionStatusAuthorized = "authorized"
	ShiftingActionStatusRework     = "rework"
	ShiftingActionStatusCompleted  = "completed"
)

// ShiftingExecutionService owns the post-authorization half of a movement: complete, cancel, and
// the operator's "authorized, waiting to be walked" queue.
//
// It is deliberately SEPARATE from ApprovalService. Those are two different jobs done by two
// different people at two different times -- an approver authorizes from anywhere, an operator
// executes standing in a park -- and they are gated on different permissions
// (CountsApproveShifting vs CountsWrite). Folding them together is what produced the behaviour the
// 2026-07-19 decision retired, where pressing "approve" silently relocated a herd.
type ShiftingExecutionService struct {
	repo     ports.Repository
	now      func() time.Time
	enqueuer ShiftingVerificationEnqueuer
	// SHIFTING SOP (2026-09-16): the rules a movement is pinned to, the pin store and the proof
	// register check. See shifting_sop.go.
	sopRules   ports.ShiftingSOPRulesSource
	sopStore   ports.ShiftingSOPStore
	proofMedia ports.ShiftingProofMedia
}

// NewShiftingExecutionService constructs the service. now may be nil (defaults to time.Now).
func NewShiftingExecutionService(repo ports.Repository, now func() time.Time) *ShiftingExecutionService {
	if now == nil {
		now = time.Now
	}
	svc := &ShiftingExecutionService{repo: repo, now: now}
	// A repository that can read the pin (the Postgres one) is the pin store by default, so a
	// completion judged without an explicit WithSOPRules still knows the movement's priority and
	// gate. Test fakes that do not implement it run the seeded card with no pre-check.
	if store, ok := repo.(ports.ShiftingSOPStore); ok {
		svc.sopStore = store
	}
	return svc
}

// ShiftingVerificationEnqueuer enqueues the mandatory-video verification item for a submitted
// movement (maintainer decision, 2026-07-26). The composition layer adapts the verification module's
// CreateItem to this narrow port so counts never touches verification's tables directly.
type ShiftingVerificationEnqueuer interface {
	EnqueueShiftingMoveVerification(ctx context.Context, in ShiftingVerificationEnqueueRequest) error
}

// ShiftingVerificationEnqueueRequest is one shifting-move video handed to the verification queue.
type ShiftingVerificationEnqueueRequest struct {
	TenantID        string
	ShiftingEventID string
	OperatorID      string
	ParkID          string
	ShedID          string
	MediaRefs       []string
	SubjectLabel    string
	// SubjectNote is the raiser's note on why the animals are moving, passed through to the
	// verification item so the verifier reads the operator's reason beside the video.
	SubjectNote string
	// PartitionLabel is the DESTINATION pen's partition. Carried so the verification item composes
	// its operational location as "Mandela 1 - Part 2" rather than the bare shed "Mandela 1" -- two
	// different places, and a verifier judging a movement video has to know which one.
	PartitionLabel string
	// ContextRows are the backend-composed "what this movement was" facts the verifier reads beside
	// the video: which pen the animals left, which they arrived in, and every answer the raiser and
	// the operator gave, grouped. Rendered verbatim.
	ContextRows []VerificationContextRow
	// MediaMeta names each MediaRefs entry (positional): the pinned card's slot title and the kind
	// the register judged the capture to be. Raise captures read "At raise · <title>".
	MediaMeta      []ports.ProofMeta
	CapturedAt     time.Time
	IdempotencyKey string
}

// VerificationContextRow is one label/value fact handed to the verifier queue. It mirrors the
// verification module's ContextRow without counts importing that package: both surfaces render it
// verbatim, in this order, and neither parses it back into business logic. Group sections the
// rows (At raise / Completion / High priority); empty rows stand alone.
type VerificationContextRow struct {
	Label string
	Value string
	Group string
}

// WithVerificationEnqueuer wires the evidence-review enqueue seam. Without it, Complete fails
// closed rather than accepting operator evidence that can never reach the verifier queue.
func (s *ShiftingExecutionService) WithVerificationEnqueuer(enqueuer ShiftingVerificationEnqueuer) *ShiftingExecutionService {
	s.enqueuer = enqueuer
	return s
}

// CompleteInput is one "the animals actually moved" confirmation.
type CompleteShiftingInput struct {
	TenantID        string
	ShiftingEventID string

	// CompletedByUserID is ANY operator holding CountsWrite, not only the raiser (maintainer
	// decision, 2026-07-19). No same-actor check is made here, and that is intentional: the person
	// standing in the park when the animals walk is not reliably the person who typed the request,
	// and forcing the raiser to be present would push operators to complete movements they did not
	// witness just to clear the queue.
	CompletedByUserID string
	TraceID           string

	// ProofRef is the MANDATORY video the operator records to prove the move (maintainer decision,
	// 2026-07-26). A blank value is rejected with ports.ErrShiftingProofRequired; verification reviews
	// the video independently of the approval + completion apply gate.
	ProofRef              string
	FeedPackingProofRef   string
	FeedGivenProofRef     string
	FeedConfigFingerprint string

	// SOPProofs / SOPAnswers are the authored card's captures and answers (SHIFTING SOP,
	// 2026-09-16). LegacyShape marks a request that predates them (no `proofs` key): its fixed
	// fields are mapped onto the seeded slots and what it could not send is recorded as
	// "Not captured (older app)" instead of refused. A request carrying the new fields is judged
	// strictly.
	SOPProofs   authored.ProofRefs
	SOPAnswers  authored.Answers
	LegacyShape bool

	// DestinationTag is the OPTIONAL destination management_stage (operational cohort) the moved
	// animals adopt. Required only when the destination shed is empty; derived server-side otherwise.
	DestinationTag string

	IdempotencyKey     string
	RequestFingerprint string
}

// Complete judges the operator's captures and answers against the card the movement is PINNED to,
// records the operator gate, and the repository atomically applies the movement.
//
// Park Head approval must already exist (maintainer decision 2026-08-09): the pin read answers
// that BEFORE any slot is judged, so an unapproved high-priority movement is told "not approved"
// rather than "record the feed videos"; the repository re-checks the same gate under the row lock.
func (s *ShiftingExecutionService) Complete(
	ctx context.Context, in CompleteShiftingInput,
) (domain.ShiftingExecutionResult, bool, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ShiftingEventID) == "" ||
		strings.TrimSpace(in.CompletedByUserID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" ||
		strings.TrimSpace(in.RequestFingerprint) == "" {
		return domain.ShiftingExecutionResult{}, false, ErrMissingRequiredField
	}
	if s.enqueuer == nil {
		// Fail closed: without the verification queue seam the mandatory evidence would have no
		// review path. This does not make verification an apply gate: the approval + completion
		// transaction still owns relocation and the census change.
		return domain.ShiftingExecutionResult{}, false, ErrVerificationEnqueuerNotWired
	}

	// THE PIN, read first. Which version the movement runs under, its priority, whether it may be
	// completed at all, and what a rework already stored.
	var pin ports.ShiftingSOPPin
	if s.sopStore != nil {
		var err error
		pin, err = s.sopStore.ShiftingSOPPin(ctx, in.TenantID, in.ShiftingEventID)
		if err != nil {
			return domain.ShiftingExecutionResult{}, false, err
		}
		// A replay of a completion already recorded is the repository's to answer (it compares the
		// stored fingerprint); everything else must be approved before a single slot is judged.
		if pin.CompletionIdempotencyKey != in.IdempotencyKey && pin.AuthorizationState != "authorized" {
			return domain.ShiftingExecutionResult{}, false, fmt.Errorf(
				"%w: shifting event %s has not been approved by a park head", ports.ErrShiftingNotAuthorized, in.ShiftingEventID)
		}
	}
	// OLDER APP, HIGH PRIORITY: the pre-SOP phone always sent both feed clips and was refused
	// without them, so keep refusing exactly that shape (deploy-day parity, E2E 2026-09-17).
	// Leniency for an older app covers only what it CANNOT send -- slots the farm authored after it
	// was installed -- never a high-priority move applied with no feed evidence at all. A replay of
	// a completion already recorded is still the repository's to answer.
	if in.LegacyShape && strings.TrimSpace(in.ProofRef) == "" {
		// The pre-SOP answer to an older app's completion with no video at all.
		return domain.ShiftingExecutionResult{}, false, ports.ErrShiftingProofRequired
	}
	if in.LegacyShape && pin.CompletionIdempotencyKey != in.IdempotencyKey &&
		strings.EqualFold(strings.TrimSpace(pin.Priority), domain.ShiftingPriorityHigh) &&
		(strings.TrimSpace(in.FeedPackingProofRef) == "" || strings.TrimSpace(in.FeedGivenProofRef) == "") {
		return domain.ShiftingExecutionResult{}, false, ports.ErrShiftingFeedProofsRequired
	}
	rules, err := s.pinnedRules(ctx, in.TenantID, pin.Version)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	card := rules.CompletionCard(pin.Priority)

	refs := legacyCompletionRefs(rules, pin.Priority, map[string]string{
		"proof_ref": in.ProofRef, "feed_packing_proof_ref": in.FeedPackingProofRef, "feed_given_proof_ref": in.FeedGivenProofRef,
	}, in.SOPProofs)
	// A NEW app's completion with no capture at all falls through to the judge, which names the
	// compulsory slot it must fill (every completion card has one); the legacy proof_required answer
	// above belongs to the legacy shape only.
	// REWORK: the verifier rejected the stored captures. The stored answers are kept when the
	// resubmit carries none. A resubmit naming the rejected capture again is NOT refused here:
	// shifting has always accepted it (its same-refs key collapses onto the existing item), and
	// the SOP card must not change daily operations on its own (deploy-day parity, 2026-09-16).
	answers := in.SOPAnswers
	if pin.VerificationState == "rejected" && len(answers) == 0 && len(pin.StoredAnswers) > 0 {
		answers = pin.StoredAnswers
	}
	judged, err := s.judgeShiftingCard(ctx, in.TenantID, card, refs, answers, in.LegacyShape)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	orderedRefs := make([]string, 0, len(judged.Proofs))
	for _, j := range judged.Proofs {
		orderedRefs = append(orderedRefs, j.Ref)
	}
	proofRef, packingRef, feedingRef := domain.LegacyColumnsFromRefs(judged.Stored, orderedRefs)

	result, replay, err := s.repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:              in.TenantID,
		ShiftingEventID:       in.ShiftingEventID,
		CompletedByUserID:     in.CompletedByUserID,
		CompletedAt:           s.now().UTC(),
		TraceID:               in.TraceID,
		ProofRef:              proofRef,
		FeedPackingProofRef:   packingRef,
		FeedGivenProofRef:     feedingRef,
		FeedConfigFingerprint: strings.TrimSpace(in.FeedConfigFingerprint),
		SOPProofs:             judged.Stored,
		SOPAnswers:            judged.Answers,
		DestinationTag:        strings.TrimSpace(in.DestinationTag),
		IdempotencyKey:        in.IdempotencyKey,
		RequestFingerprint:    in.RequestFingerprint,
	})
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}

	// Enqueue one evidence-review item even when approval + completion already applied the move.
	// The enqueue key includes the complete proof set, so transport retries heal idempotently while a
	// verifier-requested rework with newly recorded proof creates the replacement review item.
	if result.EventStatus == domain.ShiftingEventStatusPending ||
		result.EventStatus == domain.ShiftingEventStatusPendingVerification ||
		result.EventStatus == domain.ShiftingEventStatusApplied {
		if err := s.enqueueCompletion(ctx, in, pin, rules, card, judged, result); err != nil {
			return domain.ShiftingExecutionResult{}, false, err
		}
	}
	return result, replay, nil
}

// enqueueCompletion builds the verifier item from the row's STORED captures and answers (a replay
// queues exactly what the row holds), in the order completion -> high priority -> raise.
func (s *ShiftingExecutionService) enqueueCompletion(
	ctx context.Context, in CompleteShiftingInput, pin ports.ShiftingSOPPin, rules domain.ShiftingRules,
	card domain.ShiftingCardRules, judged shiftingJudgement, result domain.ShiftingExecutionResult,
) error {
	destination := oploc.OperationalLocation{
		ShedName: result.DestinationShedName, PartitionLabel: result.DestinationPartitionLabel,
	}.Display()
	source := oploc.OperationalLocation{
		ShedName: result.SourceShedName, PartitionLabel: result.SourcePartitionLabel,
	}.Display()
	// A movement is a FROM and a TO, and the verifier is judging a clip of animals walking
	// between them. Naming only the destination left her checking half the claim: a video of a
	// pen she cannot place against a label that says where they ended up.
	//
	// The destination stays the segment right after "Pen move" because both queue lists lead
	// with it -- admin-web promotes the segment matching the item's own resolved location to
	// the headline, and Android drops its appended pen label when the subject already carries
	// it. Putting the source first would demote the destination on both. The source follows as
	// its own "from …" segment, which lands on the meta line beneath.
	subject := "Pen move · " + destination
	if source != "" {
		subject += " · from " + source
	}
	subject += " · " + strconv.Itoa(len(result.MovedGoatIDs)) + " " + animalNoun(len(result.MovedGoatIDs))
	// The same two facts, unambiguously labelled, for the detail screen beside the video --
	// where the reviewer is actually deciding. A movement with no recorded source states the
	// destination alone rather than an empty row: a label with no value reads as a bug.
	contextRows := []VerificationContextRow{}
	if source != "" {
		contextRows = append(contextRows, VerificationContextRow{Label: "Moved from", Value: source})
	}
	if destination != "" {
		contextRows = append(contextRows, VerificationContextRow{Label: "Moved to", Value: destination})
	}

	// The STORED map wins over what this request judged: a repair retry must queue what the row
	// holds. A store that returns none (a fake, a pre-card caller) keeps the judged map.
	stored := result.SOPProofs
	if len(stored) == 0 {
		stored = judged.Stored
	}
	answers := result.SOPAnswers
	if answers == nil {
		answers = judged.Answers
	}
	mediaRefs, mediaMeta := completionMedia(card, stored, judged.Proofs)
	raiseCard := rules.RaiseCard()
	raiseRefs, raiseMeta := raiseMedia(raiseCard, result.RaiseSOPProofs, result.RaiseCaptureEvidence)

	// Answers and not-captured items, grouped: the raise card's first (the park head already read
	// them), then the completion section's, then the high-priority section's.
	raiseMissing := notCaptured(raiseCard, result.RaiseSOPProofs, result.RaiseSOPAnswers)
	contextRows = append(contextRows, answerRows(raiseCard, result.RaiseSOPAnswers, raiseMissing, GroupAtRaise)...)
	completionSection := domain.ShiftingCardRules{Proofs: rules.Completion.Proofs, Questions: rules.Completion.Questions}
	contextRows = append(contextRows, answerRows(completionSection, answers, notCaptured(completionSection, stored, answers), GroupCompletion)...)
	if strings.EqualFold(strings.TrimSpace(pin.Priority), domain.ShiftingPriorityHigh) {
		highSection := domain.ShiftingCardRules{Proofs: rules.HighPriority.Proofs, Questions: rules.HighPriority.Questions}
		contextRows = append(contextRows, answerRows(highSection, answers, notCaptured(highSection, stored, answers), GroupHighPriority)...)
	}

	return s.enqueuer.EnqueueShiftingMoveVerification(ctx, ShiftingVerificationEnqueueRequest{
		TenantID:        in.TenantID,
		ShiftingEventID: in.ShiftingEventID,
		OperatorID:      in.CompletedByUserID,
		ParkID:          result.DestinationParkID,
		ShedID:          result.DestinationShedID,
		MediaRefs:       append(mediaRefs, raiseRefs...),
		MediaMeta:       append(mediaMeta, raiseMeta...),
		SubjectLabel:    subject,
		SubjectNote:     derefString(result.RaiseComment),
		PartitionLabel:  result.DestinationPartitionLabel,
		ContextRows:     contextRows,
		CapturedAt:      s.now().UTC(),
		// Keyed to the EVENT + complete proof set (a seeded submission keeps the pre-SOP key shape),
		// so a retry collapses onto one queue item.
		IdempotencyKey: domain.ShiftingVerificationKey(in.ShiftingEventID, mediaRefs, answers, raiseRefs, result.VerificationRound),
	})
}

// CancelShiftingInput retires an authorized movement that will never be executed.
type CancelShiftingInput struct {
	TenantID        string
	ShiftingEventID string

	CanceledByUserID string
	Reason           string

	IdempotencyKey     string
	RequestFingerprint string
}

// Cancel retires an authorized movement. It moves NOTHING.
func (s *ShiftingExecutionService) Cancel(
	ctx context.Context, in CancelShiftingInput,
) (domain.ShiftingExecutionResult, bool, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ShiftingEventID) == "" ||
		strings.TrimSpace(in.CanceledByUserID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" ||
		strings.TrimSpace(in.RequestFingerprint) == "" {
		return domain.ShiftingExecutionResult{}, false, ErrMissingRequiredField
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return domain.ShiftingExecutionResult{}, false, ErrShiftingCancelReasonRequired
	}
	if len(reason) > domain.MaxShiftingCancelReasonLength {
		return domain.ShiftingExecutionResult{}, false, ErrInvalidJSON
	}
	return s.repo.CancelShiftingEvent(ctx, domain.ShiftingCancellationCommand{
		TenantID:           in.TenantID,
		ShiftingEventID:    in.ShiftingEventID,
		CanceledByUserID:   in.CanceledByUserID,
		CanceledAt:         s.now().UTC(),
		Reason:             reason,
		IdempotencyKey:     in.IdempotencyKey,
		RequestFingerprint: in.RequestFingerprint,
	})
}

// ListPendingExecution returns one keyset page of date-scoped Shifting Actions history. Date is an
// Asia/Kolkata business day and status buckets are disjoint backend-owned workflow states. Farm and
// shed are deliberately not list filters: each row already identifies its source and destination.
func (s *ShiftingExecutionService) ListPendingExecution(
	ctx context.Context, tenantID, businessDate, status string, pageSize int, cursor string,
) (domain.ShiftingExecutionPage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.ShiftingExecutionPage{}, ErrMissingRequiredField
	}
	decoded, err := domain.DecodeShiftingExecutionCursor(cursor)
	if err != nil {
		return domain.ShiftingExecutionPage{}, ErrInvalidShiftingExecutionFilter
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		status = ShiftingActionStatusAll
	}
	if status != ShiftingActionStatusAll && status != ShiftingActionStatusPending &&
		status != ShiftingActionStatusAuthorized && status != ShiftingActionStatusRework &&
		status != ShiftingActionStatusCompleted {
		return domain.ShiftingExecutionPage{}, ErrInvalidShiftingExecutionFilter
	}
	var raisedFrom, raisedBefore *time.Time
	if strings.TrimSpace(businessDate) != "" {
		date, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(businessDate), biztime.DefaultLocation())
		if err != nil {
			return domain.ShiftingExecutionPage{}, ErrInvalidShiftingExecutionFilter
		}
		from := date.UTC()
		before := date.AddDate(0, 0, 1).UTC()
		raisedFrom, raisedBefore = &from, &before
	}
	page, err := s.repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID:     tenantID,
		RaisedFrom:   raisedFrom,
		RaisedBefore: raisedBefore,
		Status:       status,
		PageSize:     pageSize,
		Cursor:       decoded,
		// The ACTIONS LEAD TIME is evaluated against the service's business clock, not the
		// database's, so one clock owns business time across the module.
		Now: s.now(),
	})
	if err != nil {
		return domain.ShiftingExecutionPage{}, err
	}
	if err := s.attachPinnedCards(ctx, tenantID, page.Items); err != nil {
		return domain.ShiftingExecutionPage{}, err
	}
	return page, nil
}

// attachPinnedCards resolves each row's pinned document ONCE PER VERSION PER PAGE (never per row)
// and hands the phone the cards it renders: the completion card always, the high-priority card for
// a high movement. A version the source cannot resolve falls back to the seeded document so the
// operator still sees a card; the completion write refuses the unknown pin by name.
func (s *ShiftingExecutionService) attachPinnedCards(ctx context.Context, tenantID string, rows []domain.ShiftingExecutionRow) error {
	if len(rows) == 0 {
		return nil
	}
	seeded := domain.SeededShiftingRules()
	byVersion := map[int]domain.ShiftingRules{0: seeded}
	if s.sopRules != nil {
		want := make([]int, 0, 4)
		seen := map[int]bool{0: true}
		for _, row := range rows {
			if row.SOPVersion == nil || *row.SOPVersion == 0 || seen[*row.SOPVersion] {
				continue
			}
			seen[*row.SOPVersion] = true
			want = append(want, *row.SOPVersion)
		}
		if len(want) > 0 {
			resolved, err := s.sopRules.RulesVersions(ctx, tenantID, want)
			if err != nil {
				return err
			}
			for v, r := range resolved {
				byVersion[v] = r
			}
		}
	}
	for i := range rows {
		version := 0
		if rows[i].SOPVersion != nil {
			version = *rows[i].SOPVersion
		}
		rules, ok := byVersion[version]
		if !ok {
			rules = seeded
		}
		completion := domain.ShiftingCardRules{Version: rules.Version, Stage: domain.SectionCompletion,
			Instruction: strings.TrimSpace(rules.Completion.Instruction), Proofs: rules.Completion.Proofs, Questions: rules.Completion.Questions}
		rows[i].SOP = &completion
		if strings.EqualFold(strings.TrimSpace(rows[i].Priority), domain.ShiftingPriorityHigh) {
			high := rules.HighPriorityCard()
			rows[i].HighPrioritySOP = &high
		}
	}
	return nil
}

// derefString reads an optional string as a value, mapping absent to empty. The enqueue request
// carries strings rather than pointers, and ptrIfSet on the bridge side maps empty back to absent —
// so "no note" survives the round trip as absent rather than becoming an empty note.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// animalNoun is the farm count noun for the verification subject: "1 animal", "3 animals". A
// one-animal move used to read "1 animals" on every push about it.
func animalNoun(n int) string {
	if n == 1 {
		return "animal"
	}
	return "animals"
}
