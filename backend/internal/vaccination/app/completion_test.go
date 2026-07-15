package app

import (
	"context"
	"errors"
	"testing"
	"time"

	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func TestAcceptResumesAfterRecordOnlyRetry(t *testing.T) {
	ctx := context.Background()
	repo := newCompletionRepoFake()
	obl := newObligationCompleterFake()
	stock := newStockConsumerFake()
	svc := NewCompletionService(NewService(repo), obl, stock)
	doses := int32(1)
	batchID, lotID := "batch-1", "lot-1"
	in := domain.NewCompletion{
		TenantID: "tenant-1", ObligationID: "obligation-1", GoatID: "goat-1", BatchID: &batchID,
		VaccineInventoryLotID: &lotID, Doses: &doses, ColdChainVerified: true,
		AdministeredAt: time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC),
		IdempotencyKey: "accept-key-1",
	}
	if _, applied, err := repo.RecordCompletion(ctx, in); err != nil || !applied {
		t.Fatalf("pre-record completion: applied=%v err=%v", applied, err)
	}

	result, err := svc.Accept(ctx, AcceptInput{Completion: in})

	if err != nil {
		t.Fatalf("accept retry: %v", err)
	}
	if !result.Applied || !result.Completed {
		t.Fatalf("accept retry result=%#v", result)
	}
	if repo.byID["completion-1"].status != "accepted" {
		t.Fatalf("completion status=%s want accepted", repo.byID["completion-1"].status)
	}
	if !obl.completed["obligation-1"] || stock.consumeCalls != 1 {
		t.Fatalf("side effects completed=%v consumeCalls=%d", obl.completed, stock.consumeCalls)
	}
}

func TestAcceptExistingResumesAcceptedCompletionSideEffects(t *testing.T) {
	ctx := context.Background()
	repo := newCompletionRepoFake()
	obl := newObligationCompleterFake()
	stock := newStockConsumerFake()
	svc := NewCompletionService(NewService(repo), obl, stock)
	doses := int32(1)
	batchID, lotID := "batch-1", "lot-1"
	cid, _, err := repo.RecordCompletion(ctx, domain.NewCompletion{
		TenantID: "tenant-1", ObligationID: "obligation-1", GoatID: "goat-1", BatchID: &batchID,
		VaccineInventoryLotID: &lotID, Doses: &doses, ColdChainVerified: true,
		AdministeredAt: time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC),
		IdempotencyKey: "accept-existing-key-1",
	})
	if err != nil {
		t.Fatalf("record completion: %v", err)
	}
	repo.byID[cid].status = "accepted"

	result, err := svc.AcceptExisting(ctx, AcceptExistingInput{TenantID: "tenant-1", CompletionID: cid})

	if err != nil {
		t.Fatalf("accept-existing retry: %v", err)
	}
	if !result.Applied || !result.Completed {
		t.Fatalf("accept-existing retry result=%#v", result)
	}
	if !obl.completed["obligation-1"] || stock.consumeCalls != 1 {
		t.Fatalf("side effects completed=%v consumeCalls=%d", obl.completed, stock.consumeCalls)
	}
}

func TestAcceptExistingConsumesEachObligationForSameGoatBatch(t *testing.T) {
	ctx := context.Background()
	repo := newCompletionRepoFake()
	obl := newObligationCompleterFake()
	stock := newStockConsumerFake()
	svc := NewCompletionService(NewService(repo), obl, stock)
	doses := int32(1)
	batchID, lotID := "batch-1", "lot-1"
	for _, in := range []domain.NewCompletion{
		{
			TenantID: "tenant-1", ObligationID: "obligation-1", GoatID: "goat-1", BatchID: &batchID,
			VaccineInventoryLotID: &lotID, Doses: &doses, ColdChainVerified: true,
			AdministeredAt: time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC),
			IdempotencyKey: "accept-existing-same-goat-1",
		},
		{
			TenantID: "tenant-1", ObligationID: "obligation-2", GoatID: "goat-1", BatchID: &batchID,
			VaccineInventoryLotID: &lotID, Doses: &doses, ColdChainVerified: true,
			AdministeredAt: time.Date(2026, time.June, 27, 8, 5, 0, 0, time.UTC),
			IdempotencyKey: "accept-existing-same-goat-2",
		},
	} {
		cid, _, err := repo.RecordCompletion(ctx, in)
		if err != nil {
			t.Fatalf("record completion: %v", err)
		}
		if _, err := svc.AcceptExisting(ctx, AcceptExistingInput{TenantID: in.TenantID, CompletionID: cid}); err != nil {
			t.Fatalf("accept existing %s: %v", cid, err)
		}
	}

	if stock.consumeCalls != 2 {
		t.Fatalf("consume calls = %d, want 2", stock.consumeCalls)
	}
	if !stock.seen["batch-1:consume:obligation-1:goat-1"] || !stock.seen["batch-1:consume:obligation-2:goat-1"] {
		t.Fatalf("consume keys = %#v, want per-obligation keys", stock.seen)
	}
}

func TestAcceptDoesNotScheduleBoosterWhenObligationDidNotComplete(t *testing.T) {
	ctx := context.Background()
	repo := newCompletionRepoFake()
	obl := newObligationCompleterFake()
	obl.blockComplete["obligation-1"] = true
	stock := newStockConsumerFake()
	boosterWriter := &boosterObligationWriterFake{}
	booster := NewBoosterService(&boosterRuleReaderFake{rules: []protodomain.Rule{
		{RuleID: "rule-2", DoseCode: "dose-b", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 21},
	}}, boosterWriter)
	svc := NewCompletionService(NewService(repo), obl, stock).WithBooster(booster)
	doses := int32(1)
	batchID, lotID := "batch-1", "lot-1"
	in := domain.NewCompletion{
		TenantID: "tenant-1", ObligationID: "obligation-1", GoatID: "goat-1", BatchID: &batchID,
		VaccineInventoryLotID: &lotID, Doses: &doses, ColdChainVerified: true,
		AdministeredAt: time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC),
		IdempotencyKey: "accept-canceled-obligation",
	}

	result, err := svc.Accept(ctx, AcceptInput{
		Completion:        in,
		ProtocolVersionID: "version-1",
		ScopeType:         "shed",
		ScopeID:           "shed-1",
		RuleSequence:      1,
	})

	if err != nil {
		t.Fatalf("accept closed obligation: %v", err)
	}
	if result.Completed || result.NextScheduled {
		t.Fatalf("result=%#v, want no completion transition and no booster", result)
	}
	if len(boosterWriter.inserted) != 0 {
		t.Fatalf("booster inserted %d obligations, want 0", len(boosterWriter.inserted))
	}
}

func TestRejectResumesAfterRecordOnlyRetry(t *testing.T) {
	ctx := context.Background()
	repo := newCompletionRepoFake()
	svc := NewCompletionService(NewService(repo), newObligationCompleterFake(), nil)
	in := domain.NewCompletion{
		TenantID:          "tenant-1",
		ObligationID:      "obligation-1",
		GoatID:            "goat-1",
		AdministeredAt:    time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC),
		IdempotencyKey:    "reject-key-1",
		ColdChainVerified: true,
	}
	if _, applied, err := repo.RecordCompletion(ctx, in); err != nil || !applied {
		t.Fatalf("pre-record completion: applied=%v err=%v", applied, err)
	}

	result, err := svc.Reject(ctx, RejectInput{Completion: in, Reason: "bad_photo"})

	if err != nil {
		t.Fatalf("reject retry: %v", err)
	}
	if !result.Applied || result.CompletionID != "completion-1" {
		t.Fatalf("reject retry result=%#v", result)
	}
	if repo.byID["completion-1"].status != "rejected" {
		t.Fatalf("completion status=%s want rejected", repo.byID["completion-1"].status)
	}
}

func TestAcceptStopsWhenRecordedCompletionWasRejectedBeforeSideEffects(t *testing.T) {
	ctx := context.Background()
	repo := newCompletionRepoFake()
	obl := newObligationCompleterFake()
	stock := newStockConsumerFake()
	svc := NewCompletionService(NewService(repo), obl, stock)
	doses := int32(1)
	batchID, lotID := "batch-1", "lot-1"
	in := domain.NewCompletion{
		TenantID: "tenant-1", ObligationID: "obligation-1", GoatID: "goat-1", BatchID: &batchID,
		VaccineInventoryLotID: &lotID, Doses: &doses, ColdChainVerified: true,
		AdministeredAt: time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC),
		IdempotencyKey: "accept-key-race",
	}
	if _, applied, err := repo.RecordCompletion(ctx, in); err != nil || !applied {
		t.Fatalf("pre-record completion: applied=%v err=%v", applied, err)
	}
	repo.rejectOnAccept["completion-1"] = true

	result, err := svc.Accept(ctx, AcceptInput{Completion: in})

	if err != nil {
		t.Fatalf("accept retry: %v", err)
	}
	if result.Applied || result.Completed {
		t.Fatalf("accept race result=%#v want no-op", result)
	}
	if repo.byID["completion-1"].status != "rejected" {
		t.Fatalf("completion status=%s want rejected", repo.byID["completion-1"].status)
	}
	if obl.completed["obligation-1"] || stock.consumeCalls != 0 {
		t.Fatalf("side effects should not run completed=%v consumeCalls=%d", obl.completed, stock.consumeCalls)
	}
}

func TestAcceptExistingStopsWhenRecordedCompletionWasRejectedBeforeSideEffects(t *testing.T) {
	ctx := context.Background()
	repo := newCompletionRepoFake()
	obl := newObligationCompleterFake()
	stock := newStockConsumerFake()
	svc := NewCompletionService(NewService(repo), obl, stock)
	doses := int32(1)
	batchID, lotID := "batch-1", "lot-1"
	cid, _, err := repo.RecordCompletion(ctx, domain.NewCompletion{
		TenantID: "tenant-1", ObligationID: "obligation-1", GoatID: "goat-1", BatchID: &batchID,
		VaccineInventoryLotID: &lotID, Doses: &doses, ColdChainVerified: true,
		AdministeredAt: time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC),
		IdempotencyKey: "accept-existing-key-race",
	})
	if err != nil {
		t.Fatalf("record completion: %v", err)
	}
	repo.rejectOnAccept[cid] = true

	result, err := svc.AcceptExisting(ctx, AcceptExistingInput{TenantID: "tenant-1", CompletionID: cid})

	if err != nil {
		t.Fatalf("accept-existing race: %v", err)
	}
	if result.Applied || result.Completed {
		t.Fatalf("accept-existing race result=%#v want no-op", result)
	}
	if repo.byID[cid].status != "rejected" {
		t.Fatalf("completion status=%s want rejected", repo.byID[cid].status)
	}
	if obl.completed["obligation-1"] || stock.consumeCalls != 0 {
		t.Fatalf("side effects should not run completed=%v consumeCalls=%d", obl.completed, stock.consumeCalls)
	}
}

type completionRepoFake struct {
	byID           map[string]*completionRowFake
	byKey          map[string]string
	rejectOnAccept map[string]bool
	stockAvailable string
	stockExpiry    *time.Time
	next           int
	// rollup read-model doubles for impact-preview tests.
	rollupAgg   domain.EligibilityRollupAggregate
	dailyCap    int64
	goatScanned bool // set true if any live goat-count method is ever called (impact must not scan goats)
	// stage-review re-verify doubles (VACC-REV-10).
	srGoat              domain.EligibleGoat
	srGoatLoadFound     bool
	srResolved          bool
	srFinishWeeks       int
	srCorrectedResolved bool
	srCorrectedWasOpen  bool
}

type completionRowFake struct {
	status string
	ctx    domain.AcceptedCompletion
}

func (r *completionRowFake) withStatus() domain.AcceptedCompletion {
	out := r.ctx
	out.Status = r.status
	return out
}

func newCompletionRepoFake() *completionRepoFake {
	return &completionRepoFake{byID: map[string]*completionRowFake{}, byKey: map[string]string{}, rejectOnAccept: map[string]bool{}}
}

func (r *completionRepoFake) Ping(context.Context) error { return nil }

func (r *completionRepoFake) RecordCompletion(_ context.Context, in domain.NewCompletion) (string, bool, error) {
	if cid := r.byKey[in.IdempotencyKey]; cid != "" {
		return "", false, nil
	}
	r.next++
	cid := "completion-1"
	if r.next > 1 {
		cid = "completion-2"
	}
	r.byKey[in.IdempotencyKey] = cid
	r.byID[cid] = &completionRowFake{status: "recorded", ctx: domain.AcceptedCompletion{
		CompletionID: cid, Status: "recorded", ObligationID: in.ObligationID, GoatID: in.GoatID,
		BatchID: deref(in.BatchID), LotID: deref(in.VaccineInventoryLotID),
		Doses: derefDose(in.Doses), AdministeredAt: in.AdministeredAt,
	}}
	return cid, true, nil
}

func (r *completionRepoFake) GetRecordedCompletion(_ context.Context, _, completionID string) (domain.AcceptedCompletion, bool, error) {
	row := r.byID[completionID]
	if row == nil || row.status != "recorded" {
		return domain.AcceptedCompletion{}, false, nil
	}
	return row.withStatus(), true, nil
}

func (r *completionRepoFake) GetAcceptableCompletion(_ context.Context, _, completionID string) (domain.AcceptedCompletion, bool, error) {
	row := r.byID[completionID]
	if row == nil || (row.status != "recorded" && row.status != "accepted") {
		return domain.AcceptedCompletion{}, false, nil
	}
	return row.withStatus(), true, nil
}

func (r *completionRepoFake) GetAcceptableCompletionByIdempotency(_ context.Context, _, key string) (domain.AcceptedCompletion, bool, error) {
	row := r.byID[r.byKey[key]]
	if row == nil || (row.status != "recorded" && row.status != "accepted" && row.status != "rejected") {
		return domain.AcceptedCompletion{}, false, nil
	}
	return row.withStatus(), true, nil
}

func (r *completionRepoFake) AcceptCompletion(_ context.Context, _, completionID string, _ *string, _ *time.Time) (domain.AcceptedCompletion, bool, error) {
	row := r.byID[completionID]
	if row == nil || row.status != "recorded" {
		return domain.AcceptedCompletion{}, false, nil
	}
	if r.rejectOnAccept[completionID] {
		row.status = "rejected"
		return domain.AcceptedCompletion{}, false, nil
	}
	row.status = "accepted"
	return row.withStatus(), true, nil
}

func (r *completionRepoFake) RejectCompletion(_ context.Context, _, completionID string, _ string, _ *string) (bool, error) {
	row := r.byID[completionID]
	if row == nil || row.status != "recorded" {
		return false, nil
	}
	row.status = "rejected"
	return true, nil
}

func (r *completionRepoFake) ListCompletionsByGoat(context.Context, string, string, int32) ([]domain.CompletionHistoryItem, error) {
	return nil, nil
}

func (r *completionRepoFake) ListRecordedCompletionsByTask(context.Context, string, string) ([]string, error) {
	return nil, nil
}

func (r *completionRepoFake) RecordCompletionsFromSubmission(context.Context, string, string, string, string) (int, error) {
	return 0, nil
}

func (r *completionRepoFake) ListRecordedCompletions(context.Context, string, string, *domain.RecordedCompletionCursor, int32) (domain.RecordedCompletionPage, error) {
	return domain.RecordedCompletionPage{}, nil
}

func (r *completionRepoFake) GetLastAcceptedForGoat(context.Context, string, string) (domain.LastAccepted, bool, error) {
	return domain.LastAccepted{}, false, nil
}

func (r *completionRepoFake) CountEligibleGoats(context.Context, domain.ImpactFilter) (int64, error) {
	r.goatScanned = true
	return 0, nil
}

func (r *completionRepoFake) CountCatchupGoats(context.Context, domain.ImpactFilter) (int64, error) {
	r.goatScanned = true
	return 0, nil
}

func (r *completionRepoFake) CountEligibleShedScopes(context.Context, domain.ImpactFilter) (int64, error) {
	r.goatScanned = true
	return 0, nil
}

func (r *completionRepoFake) SumEligibilityRollup(context.Context, domain.ImpactFilter) (domain.EligibilityRollupAggregate, error) {
	return r.rollupAgg, nil
}

func (r *completionRepoFake) CapacityMaxPerDay(context.Context, string) (int64, error) {
	if r.dailyCap < 1 {
		return 100, nil
	}
	return r.dailyCap, nil
}

func (r *completionRepoFake) RecomputeEligibilityRollup(context.Context, string) (domain.RollupRecomputeResult, error) {
	return domain.RollupRecomputeResult{}, nil
}

func (r *completionRepoFake) SumAvailableStock(context.Context, string, string, *string) (string, *time.Time, error) {
	available := r.stockAvailable
	if available == "" {
		available = "0"
	}
	return available, r.stockExpiry, nil
}

func (r *completionRepoFake) ListEligibleGoatsForGeneration(context.Context, domain.ImpactFilter, string, int32) ([]domain.EligibleGoat, error) {
	return nil, nil
}

func (r *completionRepoFake) GetGoatForGeneration(context.Context, string, string) (domain.EligibleGoat, bool, error) {
	return r.srGoat, r.srGoatLoadFound, nil
}

func (r *completionRepoFake) StaleKidFinishWeeks(context.Context, string) (int, error) {
	if r.srFinishWeeks == 0 {
		return 20, nil
	}
	return r.srFinishWeeks, nil
}

func (r *completionRepoFake) ResolveStageReviewItemCorrected(context.Context, string, string, string, string, time.Time, int) (bool, bool, error) {
	return r.srCorrectedResolved, r.srCorrectedWasOpen, nil
}

type obligationCompleterFake struct {
	completed     map[string]bool
	blockComplete map[string]bool
}

func newObligationCompleterFake() *obligationCompleterFake {
	return &obligationCompleterFake{completed: map[string]bool{}, blockComplete: map[string]bool{}}
}

func (o *obligationCompleterFake) MarkCompleted(_ context.Context, _, obligationID string) (bool, error) {
	if o.blockComplete[obligationID] {
		return false, nil
	}
	if o.completed[obligationID] {
		return false, nil
	}
	o.completed[obligationID] = true
	return true, nil
}

func (o *obligationCompleterFake) GetBoosterContext(context.Context, string, string) (string, string, string, int32, error) {
	return "", "", "", 0, nil
}

func (o *obligationCompleterFake) IsCompleted(_ context.Context, _, obligationID string) (bool, error) {
	return o.completed[obligationID], nil
}

type stockConsumerFake struct {
	seen         map[string]bool
	consumeCalls int
}

func newStockConsumerFake() *stockConsumerFake {
	return &stockConsumerFake{seen: map[string]bool{}}
}

func (s *stockConsumerFake) ConsumeForBatch(_ context.Context, _, _, _, key string, _ int64) error {
	if s.seen[key] {
		return nil
	}
	s.seen[key] = true
	s.consumeCalls++
	return nil
}

func (r *completionRepoFake) ListOpenStageReviewItems(context.Context, string, *domain.StageReviewItemCursor, int) (domain.StageReviewItemPage, error) {
	return domain.StageReviewItemPage{}, nil
}

func (r *completionRepoFake) ResolveStageReviewItem(context.Context, string, string, string, string, string, time.Time) (bool, error) {
	return r.srResolved, nil
}

// TestResolveStageReviewItemCorrectedReverify is the VACC-REV-10 service-branching guard: a 'corrected'
// resolution maps the atomic repo outcome to 200 (resolved), 409 (open but re-check blocked — still
// stale or goat missing), or 404 (not open); an 'exception' resolution skips the re-check entirely.
// The actual stale re-check SQL is exercised by the Postgres integration test.
func TestResolveStageReviewItemCorrectedReverify(t *testing.T) {
	ctx := context.Background()

	// corrected + repo says still-open-but-not-resolved (re-check blocked) -> 409.
	svc := NewService(&completionRepoFake{srCorrectedResolved: false, srCorrectedWasOpen: true})
	if _, err := svc.ResolveStageReviewItem(ctx, "t", "ri", "actor", "claims fixed", "corrected", time.Now()); !errors.Is(err, ErrStageReviewStillActive) {
		t.Fatalf("corrected-blocked err = %v, want ErrStageReviewStillActive", err)
	}

	// corrected + repo resolved -> ok.
	svc = NewService(&completionRepoFake{srCorrectedResolved: true, srCorrectedWasOpen: true})
	if ok, err := svc.ResolveStageReviewItem(ctx, "t", "ri", "actor", "advanced to adult", "corrected", time.Now()); err != nil || !ok {
		t.Fatalf("corrected-resolved ok=%v err=%v, want true/nil", ok, err)
	}

	// corrected + item not open -> 404 (false, nil), not an error.
	svc = NewService(&completionRepoFake{srCorrectedResolved: false, srCorrectedWasOpen: false})
	if ok, err := svc.ResolveStageReviewItem(ctx, "t", "ri", "actor", "x", "corrected", time.Now()); err != nil || ok {
		t.Fatalf("corrected-not-open ok=%v err=%v, want false/nil", ok, err)
	}

	// exception -> plain resolve, no re-check.
	svc = NewService(&completionRepoFake{srResolved: true})
	if ok, err := svc.ResolveStageReviewItem(ctx, "t", "ri", "actor", "already vaccinated; tag fix scheduled", "exception", time.Now()); err != nil || !ok {
		t.Fatalf("exception ok=%v err=%v, want true/nil", ok, err)
	}
}
