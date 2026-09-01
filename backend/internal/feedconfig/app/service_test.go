package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feedconfig/domain"
	"github.com/vgoats/goatos/backend/internal/feedconfig/ports"
)

// Fake-backed service tests. They prove the rules the service OWNS -- paging bounds, validate-or-
// reject, and the idempotency envelope -- without a database, so they run in the default suite.
//
// What they deliberately cannot prove is effective dating: a fake returns whatever a test stocked
// it with, so it would happily "confirm" a supersede the SQL never performs. That proof lives in
// the Postgres integration tests, against the real schema.

type fakeRepo struct {
	lastRationRate     domain.UpsertRationRateCommand
	lastShedFactor     domain.UpsertShedFactorCommand
	lastFeedItem       domain.CreateFeedItemCommand
	lastFeedItemStatus domain.SetFeedItemStatusCommand
	lastSessionSlot    domain.SetSessionTemplateItemCommand
	lastSchedule       domain.UpsertScheduleConfigCommand
	lastRateQuery      domain.RationRateQuery
	lastTagQuery       domain.ShedTagQuery
	lastSchedQuery     domain.ScheduleConfigQuery
	lastSessionQuery   domain.SessionTemplateQuery

	lastExperiment       domain.UpsertExperimentConfigCommand
	lastExperimentStatus domain.SetExperimentShedStatusCommand
	lastExperimentQuery  domain.ExperimentConfigQuery
	lastPenQuery         domain.PenQuery
	lastExperimentBatch  domain.UpsertExperimentConfigBatchCommand

	result domain.WriteResult
	err    error

	// writeCalls counts how many times a write actually reached the repository, so a test can assert
	// that a rejected request performed NO side effect rather than merely returning an error.
	writeCalls int
}

func (f *fakeRepo) ListRationRates(_ context.Context, q domain.RationRateQuery) (domain.RationRatePage, error) {
	f.lastRateQuery = q
	return domain.RationRatePage{Limit: q.Page.Limit, Offset: q.Page.Offset}, f.err
}

func (f *fakeRepo) ListRationGroups(_ context.Context, _ string, p domain.Page) (domain.RationGroupPage, error) {
	return domain.RationGroupPage{Limit: p.Limit, Offset: p.Offset}, f.err
}

func (f *fakeRepo) ListShedTags(_ context.Context, q domain.ShedTagQuery) (domain.ShedTagPage, error) {
	f.lastTagQuery = q
	return domain.ShedTagPage{Limit: q.Page.Limit, Offset: q.Page.Offset}, f.err
}

func (f *fakeRepo) ListFeedItems(_ context.Context, _ string, p domain.Page) (domain.FeedItemPage, error) {
	return domain.FeedItemPage{Limit: p.Limit, Offset: p.Offset}, f.err
}

func (f *fakeRepo) ListSessionTemplates(_ context.Context, q domain.SessionTemplateQuery) (domain.SessionTemplatePage, error) {
	f.lastSessionQuery = q
	return domain.SessionTemplatePage{Limit: q.Page.Limit, Offset: q.Page.Offset}, f.err
}

func (f *fakeRepo) ListScheduleConfig(_ context.Context, q domain.ScheduleConfigQuery) (domain.ScheduleConfigPage, error) {
	f.lastSchedQuery = q
	return domain.ScheduleConfigPage{Limit: q.Page.Limit, Offset: q.Page.Offset}, f.err
}

func (f *fakeRepo) ListShedFactors(_ context.Context, q domain.ShedFactorQuery) (domain.ShedFactorPage, error) {
	return domain.ShedFactorPage{Limit: q.Page.Limit, Offset: q.Page.Offset}, f.err
}

func (f *fakeRepo) ListExperimentConfig(_ context.Context, q domain.ExperimentConfigQuery) (domain.ExperimentConfigPage, error) {
	f.lastExperimentQuery = q
	return domain.ExperimentConfigPage{Limit: q.Page.Limit, Offset: q.Page.Offset}, f.err
}

func (f *fakeRepo) UpsertExperimentConfigBatch(_ context.Context, cmd domain.UpsertExperimentConfigBatchCommand) (domain.WriteResult, error) {
	f.lastExperimentBatch = cmd
	return domain.WriteResult{Kind: domain.WriteKindExperimentConfig, Outcome: domain.OutcomeInserted}, f.err
}

func (f *fakeRepo) ListPens(_ context.Context, q domain.PenQuery) (domain.PenPage, error) {
	f.lastPenQuery = q
	return domain.PenPage{Limit: q.Page.Limit, Offset: q.Page.Offset}, f.err
}

func (f *fakeRepo) UpsertExperimentConfig(_ context.Context, cmd domain.UpsertExperimentConfigCommand) (domain.WriteResult, error) {
	f.writeCalls++
	f.lastExperiment = cmd
	return f.result, f.err
}

func (f *fakeRepo) SetExperimentShedStatus(_ context.Context, cmd domain.SetExperimentShedStatusCommand) (domain.WriteResult, error) {
	f.writeCalls++
	f.lastExperimentStatus = cmd
	return f.result, f.err
}

func (f *fakeRepo) UpsertRationRate(_ context.Context, cmd domain.UpsertRationRateCommand) (domain.WriteResult, error) {
	f.writeCalls++
	f.lastRationRate = cmd
	return f.result, f.err
}

func (f *fakeRepo) UpsertShedFactor(_ context.Context, cmd domain.UpsertShedFactorCommand) (domain.WriteResult, error) {
	f.writeCalls++
	f.lastShedFactor = cmd
	return f.result, f.err
}

func (f *fakeRepo) CreateFeedItem(_ context.Context, cmd domain.CreateFeedItemCommand) (domain.WriteResult, error) {
	f.writeCalls++
	f.lastFeedItem = cmd
	return f.result, f.err
}

func (f *fakeRepo) SetFeedItemStatus(_ context.Context, cmd domain.SetFeedItemStatusCommand) (domain.WriteResult, error) {
	f.writeCalls++
	f.lastFeedItemStatus = cmd
	return f.result, f.err
}

func (f *fakeRepo) SetSessionTemplateItem(_ context.Context, cmd domain.SetSessionTemplateItemCommand) (domain.WriteResult, error) {
	f.writeCalls++
	f.lastSessionSlot = cmd
	return f.result, f.err
}

func (f *fakeRepo) UpsertScheduleConfig(_ context.Context, cmd domain.UpsertScheduleConfigCommand) (domain.WriteResult, error) {
	f.writeCalls++
	f.lastSchedule = cmd
	return f.result, f.err
}

var _ ports.Repository = (*fakeRepo)(nil)

// pinnedService fixes the business clock so effective_from is deterministic. 2026-07-19 21:00 UTC
// is deliberately LATE IN THE UTC DAY: in Asia/Kolkata that is already 2026-07-20, so a service that
// derived the business date from UTC would date the edit to the wrong day and this test would catch
// it.
func pinnedService(repo ports.Repository) *Service {
	return NewService(repo).WithClock(func() time.Time {
		return time.Date(2026, 7, 19, 21, 0, 0, 0, time.UTC)
	})
}

func str(s string) *string { return &s }
func i32(v int32) *int32   { return &v }

// TestUpsertRationRateRejectsAbsentGramsWithoutDefaulting is the single most important test in this
// package.
//
// An absent grams_per_head must FAIL. It must not be defaulted to 0, because absence of a rate means
// "not configured" -- a state the feed path must BLOCK on -- while 0 means "feed nothing", which is
// a correct instruction for milk-fed kids. A service that quietly filled in 0 would produce a clean,
// complete-looking feed sheet for a shed nobody configured.
//
// The assertion is not only on the error: it also proves the repository was never called, so the
// rejection happened before any side effect.
func TestUpsertRationRateRejectsAbsentGramsWithoutDefaulting(t *testing.T) {
	repo := &fakeRepo{}
	svc := pinnedService(repo)

	_, err := svc.UpsertRationRate(context.Background(), UpsertRationRateInput{
		TenantID: "tenant", ActorRef: "actor", ParkID: "park",
		RationGroupLabel: "Boer", ShedTagLabel: "Pregnant", FeedItemLabel: "Concentrate",
		GramsPerHead:       nil, // ABSENT
		IdempotencyKey:     "key-12345678",
		RequestFingerprint: "fp",
	})
	if !errors.Is(err, domain.ErrMissingField) {
		t.Fatalf("absent grams_per_head error = %v, want ErrMissingField", err)
	}
	var fe *domain.FieldError
	if !errors.As(err, &fe) || fe.Field != "grams_per_head" {
		t.Fatalf("error = %v, want a FieldError naming grams_per_head", err)
	}
	if repo.writeCalls != 0 {
		t.Fatalf("repository was called %d times for a rejected write, want 0", repo.writeCalls)
	}
}

// TestUpsertRationRateAcceptsAuthoredZero is the other half of the same rule: 0 is a REAL authored
// value and must reach the repository as "0.000", not be treated as missing.
func TestUpsertRationRateAcceptsAuthoredZero(t *testing.T) {
	repo := &fakeRepo{}
	svc := pinnedService(repo)

	if _, err := svc.UpsertRationRate(context.Background(), UpsertRationRateInput{
		TenantID: "tenant", ActorRef: "actor", ParkID: "park",
		RationGroupLabel: "Kid", ShedTagLabel: "K0", FeedItemLabel: "Concentrate",
		GramsPerHead:       str("0"),
		IdempotencyKey:     "key-12345678",
		RequestFingerprint: "fp",
	}); err != nil {
		t.Fatalf("authored zero rejected: %v", err)
	}
	if repo.lastRationRate.GramsPerHead != "0.000" {
		t.Fatalf("grams_per_head = %q, want %q", repo.lastRationRate.GramsPerHead, "0.000")
	}
}

// TestUpsertRationRateRejectsOutOfRangeValues covers present-but-invalid authored numbers. Each must
// fail rather than be clamped or rounded into something the author never entered.
func TestUpsertRationRateRejectsOutOfRangeValues(t *testing.T) {
	tests := []struct {
		name  string
		grams string
	}{
		{name: "negative rate", grams: "-1"},
		{name: "negative fractional rate", grams: "-0.001"},
		{name: "over-precise rate", grams: "10.12345"},
		{name: "non-numeric rate", grams: "lots"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := pinnedService(repo)
			_, err := svc.UpsertRationRate(context.Background(), UpsertRationRateInput{
				TenantID: "tenant", ActorRef: "actor", ParkID: "park",
				RationGroupLabel: "Boer", ShedTagLabel: "Pregnant", FeedItemLabel: "Concentrate",
				GramsPerHead:       str(tc.grams),
				IdempotencyKey:     "key-12345678",
				RequestFingerprint: "fp",
			})
			if err == nil {
				t.Fatalf("grams_per_head %q was accepted, want rejection", tc.grams)
			}
			if repo.writeCalls != 0 {
				t.Fatalf("repository was called for a rejected write")
			}
		})
	}
}

// TestWriteIdentityDerivesIndiaBusinessDate proves effective_from comes from the Asia/Kolkata
// business calendar rather than the UTC day.
//
// The pinned clock is 2026-07-19 21:00 UTC, which is 2026-07-20 02:30 IST. A UTC-derived date would
// be 2026-07-19 -- and for an effective-dated config row that means the edit claims to have been in
// force yesterday.
func TestWriteIdentityDerivesIndiaBusinessDate(t *testing.T) {
	repo := &fakeRepo{}
	svc := pinnedService(repo)

	if _, err := svc.UpsertRationRate(context.Background(), UpsertRationRateInput{
		TenantID: "tenant", ActorRef: "actor", ParkID: "park",
		RationGroupLabel: "Boer", ShedTagLabel: "Pregnant", FeedItemLabel: "Concentrate",
		GramsPerHead:       str("250"),
		IdempotencyKey:     "key-12345678",
		RequestFingerprint: "fp",
	}); err != nil {
		t.Fatalf("UpsertRationRate: %v", err)
	}
	if got := repo.lastRationRate.EffectiveFrom; got != "2026-07-20" {
		t.Fatalf("effective_from = %q, want 2026-07-20 (Asia/Kolkata), not the UTC day", got)
	}
}

// TestSessionTemplateReadUsesIndiaBusinessDate keeps the config UI in parity with generation: both
// must decide the active recipe from the same Asia/Kolkata business date, not PostgreSQL CURRENT_DATE
// or the UTC day.
func TestSessionTemplateReadUsesIndiaBusinessDate(t *testing.T) {
	repo := &fakeRepo{}
	svc := pinnedService(repo)

	if _, err := svc.ListSessionTemplates(context.Background(), "tenant", "park", nil, nil); err != nil {
		t.Fatalf("ListSessionTemplates: %v", err)
	}
	if got := repo.lastSessionQuery.AsOfDate; got != "2026-07-20" {
		t.Fatalf("as_of_date = %q, want 2026-07-20 (Asia/Kolkata), not the UTC day", got)
	}
}

// TestWritesRequireIdempotencyKeyAndActor proves no authored write can reach persistence without the
// idempotency envelope. Without a key, a browser retry authors a second edit.
func TestWritesRequireIdempotencyKeyAndActor(t *testing.T) {
	tests := []struct {
		name        string
		tenant      string
		actor       string
		key         string
		fingerprint string
		wantErr     error
	}{
		{name: "missing tenant", tenant: "", actor: "a", key: "key-12345678", fingerprint: "fp", wantErr: ErrMissingTenant},
		{name: "missing actor", tenant: "t", actor: "", key: "key-12345678", fingerprint: "fp", wantErr: ErrMissingActor},
		{name: "missing key", tenant: "t", actor: "a", key: "", fingerprint: "fp", wantErr: ErrMissingIdempotencyKey},
		{name: "missing fingerprint", tenant: "t", actor: "a", key: "key-12345678", fingerprint: "", wantErr: ErrMissingIdempotencyKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := pinnedService(repo)
			_, err := svc.UpsertRationRate(context.Background(), UpsertRationRateInput{
				TenantID: tc.tenant, ActorRef: tc.actor, ParkID: "park",
				RationGroupLabel: "Boer", ShedTagLabel: "Pregnant", FeedItemLabel: "Concentrate",
				GramsPerHead:       str("250"),
				IdempotencyKey:     tc.key,
				RequestFingerprint: tc.fingerprint,
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if repo.writeCalls != 0 {
				t.Fatalf("repository was called for a rejected write")
			}
		})
	}
}

// TestUpsertScheduleConfigValidation covers the dispatch-clock rules, including the live experiment
// configuration whose direction and correction times are EQUAL.
func TestUpsertScheduleConfigValidation(t *testing.T) {
	tests := []struct {
		name       string
		workflow   string
		direction  string
		correction string
		transport  *string
		wantErr    bool
		wantDir    string
		wantTrans  string
	}{
		{
			name: "live normal clock", workflow: "normal",
			direction: "07:00", correction: "14:00", transport: str("15:45"),
			wantDir: "07:00:00", wantTrans: "15:45:00",
		},
		{
			// Equal direction and correction: the real experiment workflow. Must be accepted.
			name: "live experiment clock with equal times", workflow: "experiment",
			direction: "14:00", correction: "14:00", transport: str("15:45"),
			wantDir: "14:00:00", wantTrans: "15:45:00",
		},
		{
			name: "absent transport is allowed", workflow: "normal",
			direction: "07:00", correction: "14:00", transport: nil,
			wantDir: "07:00:00",
		},
		{name: "unknown workflow rejected", workflow: "trial", direction: "07:00", correction: "14:00", wantErr: true},
		{name: "correction before direction rejected", workflow: "normal", direction: "14:00", correction: "07:00", wantErr: true},
		{name: "transport before correction rejected", workflow: "normal", direction: "07:00", correction: "14:00", transport: str("13:00"), wantErr: true},
		// A malformed transport time must FAIL, not silently become NULL: NULL means "no declared
		// cutoff", so swallowing the typo would tell the dispatcher this park has no deadline at all.
		{name: "malformed transport rejected not nulled", workflow: "normal", direction: "07:00", correction: "14:00", transport: str("later"), wantErr: true},
		{name: "offset-bearing time rejected", workflow: "normal", direction: "07:00+05:30", correction: "14:00", wantErr: true},
		{name: "missing direction rejected", workflow: "normal", direction: "", correction: "14:00", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := pinnedService(repo)
			_, err := svc.UpsertScheduleConfig(context.Background(), UpsertScheduleConfigInput{
				TenantID: "tenant", ActorRef: "actor", ParkID: "park",
				Workflow: tc.workflow, DirectionTime: tc.direction, CorrectionTime: tc.correction,
				TransportTime: tc.transport, TransportTimeProvided: tc.transport != nil,
				IdempotencyKey: "key-12345678", RequestFingerprint: "fp",
			})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected rejection, got success")
				}
				if repo.writeCalls != 0 {
					t.Fatalf("repository was called for a rejected write")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if repo.lastSchedule.DirectionTime != tc.wantDir {
				t.Fatalf("direction_time = %q, want %q", repo.lastSchedule.DirectionTime, tc.wantDir)
			}
			if tc.wantTrans == "" {
				if repo.lastSchedule.TransportTime != nil {
					t.Fatalf("transport_time = %v, want nil", *repo.lastSchedule.TransportTime)
				}
				return
			}
			if repo.lastSchedule.TransportTime == nil || *repo.lastSchedule.TransportTime != tc.wantTrans {
				t.Fatalf("transport_time = %v, want %q", repo.lastSchedule.TransportTime, tc.wantTrans)
			}
		})
	}
}

func TestUpsertShedFactorValidation(t *testing.T) {
	tests := []struct {
		name       string
		multiplier *string
		wantErr    bool
		want       string
	}{
		{name: "typical multiplier", multiplier: str("1.5"), want: "1.5000"},
		// An explicitly authored 0 is legal: a shed deliberately fed none of an item. It is a different
		// statement from having no row, which reads as the safe 1.0.
		{name: "authored zero accepted", multiplier: str("0"), want: "0.0000"},
		{name: "absent rejected not defaulted to one", multiplier: nil, wantErr: true},
		{name: "negative rejected", multiplier: str("-1"), wantErr: true},
		{name: "over-precise rejected", multiplier: str("1.234567"), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := pinnedService(repo)
			_, err := svc.UpsertShedFactor(context.Background(), UpsertShedFactorInput{
				TenantID: "tenant", ActorRef: "actor", ParkID: "park", ShedID: "shed",
				FeedItemLabel: "Concentrate", Multiplier: tc.multiplier,
				IdempotencyKey: "key-12345678", RequestFingerprint: "fp",
			})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected rejection, got success")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if repo.lastShedFactor.Multiplier != tc.want {
				t.Fatalf("multiplier = %q, want %q", repo.lastShedFactor.Multiplier, tc.want)
			}
		})
	}
}

// TestPagingIsBoundedAndNeverSilentlyClamped proves a present-but-out-of-range paging value is a
// REJECTION. A caller that asked for 5000 rows and silently received 50 has a truncated grid it
// believes is complete.
func TestPagingIsBoundedAndNeverSilentlyClamped(t *testing.T) {
	tests := []struct {
		name       string
		limit      *int32
		offset     *int32
		wantErr    bool
		wantLimit  int32
		wantOffset int32
	}{
		{name: "absent uses defaults", limit: nil, offset: nil, wantLimit: defaultLimit, wantOffset: 0},
		{name: "in-range values are honoured", limit: i32(25), offset: i32(100), wantLimit: 25, wantOffset: 100},
		{name: "max limit is allowed", limit: i32(maxLimit), wantLimit: maxLimit},
		{name: "max offset is allowed", offset: i32(maxOffset), wantLimit: defaultLimit, wantOffset: maxOffset},
		{name: "limit above max is rejected", limit: i32(maxLimit + 1), wantErr: true},
		{name: "zero limit is rejected", limit: i32(0), wantErr: true},
		{name: "negative limit is rejected", limit: i32(-1), wantErr: true},
		{name: "offset above max is rejected", offset: i32(maxOffset + 1), wantErr: true},
		{name: "negative offset is rejected", offset: i32(-1), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := pinnedService(repo)
			page, err := svc.ListRationRates(context.Background(), "tenant", RationRateFilter{ParkID: "park", Limit: tc.limit, Offset: tc.offset})
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidPaging) {
					t.Fatalf("error = %v, want ErrInvalidPaging", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if page.Limit != tc.wantLimit || page.Offset != tc.wantOffset {
				t.Fatalf("limit/offset = %d/%d, want %d/%d", page.Limit, page.Offset, tc.wantLimit, tc.wantOffset)
			}
		})
	}
}

// TestListRationRatesRequiresPark pins park_id as required rather than an optional tenant-wide
// fallback. Rates are park-scoped and two parks genuinely differ, so a tenant-wide listing would
// interleave rows under identical labels with no way to tell them apart.
func TestListRationRatesRequiresPark(t *testing.T) {
	svc := pinnedService(&fakeRepo{})
	if _, err := svc.ListRationRates(context.Background(), "tenant", RationRateFilter{ParkID: "  "}); !errors.Is(err, ErrMissingPark) {
		t.Fatalf("error = %v, want ErrMissingPark", err)
	}
}

// TestListFiltersAreValidatedNotIgnored proves an unrecognised enum filter fails instead of quietly
// widening the result set.
func TestListFiltersAreValidatedNotIgnored(t *testing.T) {
	repo := &fakeRepo{}
	svc := pinnedService(repo)

	if _, err := svc.ListShedTags(context.Background(), "tenant", "juvenile", nil, nil); !errors.Is(err, domain.ErrInvalidAppliesTo) {
		t.Fatalf("bad applies_to error = %v, want ErrInvalidAppliesTo", err)
	}
	if _, err := svc.ListScheduleConfig(context.Background(), "tenant", "park", "trial", nil, nil); !errors.Is(err, domain.ErrInvalidWorkflow) {
		t.Fatalf("bad workflow error = %v, want ErrInvalidWorkflow", err)
	}

	// An EMPTY filter is not an error: it means "no filter".
	if _, err := svc.ListShedTags(context.Background(), "tenant", "", nil, nil); err != nil {
		t.Fatalf("empty applies_to rejected: %v", err)
	}
	if repo.lastTagQuery.AppliesTo != "" {
		t.Fatalf("applies_to = %q, want empty (no filter)", repo.lastTagQuery.AppliesTo)
	}
	if _, err := svc.ListScheduleConfig(context.Background(), "tenant", "park", "", nil, nil); err != nil {
		t.Fatalf("empty workflow rejected: %v", err)
	}
	if repo.lastSchedQuery.Workflow != "" {
		t.Fatalf("workflow = %q, want empty (no filter)", repo.lastSchedQuery.Workflow)
	}
}

// TestGramsComparisonIsValidatedAsAPair proves the grams filter is accepted only as a complete,
// well-formed (operator, value) pair -- and REJECTED, never defaulted, otherwise.
//
// The defect this forbids is quiet, which is why it is pinned: every possible fallback for a
// missing half answers a different question than the one the operator asked. A default operator
// silently reinterprets the value they typed; a default value silently reinterprets the operator
// they picked; and an unrecognised operator that degrades to "no filter" shows the WHOLE grid to
// someone who asked to narrow it. On this screen a wrong row set is a wrong feeding decision.
func TestGramsComparisonIsValidatedAsAPair(t *testing.T) {
	tests := []struct {
		name    string
		op      string
		value   string
		wantErr bool
		// wantCompare is the comparison the repository should receive; nil means "no filter".
		wantCompare *domain.GramsComparison
	}{
		{name: "neither half is no filter", wantCompare: nil},
		{name: "the common case: more than zero", op: "gt", value: "0", wantCompare: &domain.GramsComparison{Op: domain.GramsOpGreaterThan, Value: "0"}},
		{name: "exactly zero isolates the authored zeros", op: "eq", value: "0", wantCompare: &domain.GramsComparison{Op: domain.GramsOpEquals, Value: "0"}},
		{name: "exact decimals survive verbatim", op: "lte", value: "149.995", wantCompare: &domain.GramsComparison{Op: domain.GramsOpAtMost, Value: "149.995"}},
		{name: "operator without a value is rejected", op: "gt", wantErr: true},
		{name: "value without an operator is rejected", value: "0", wantErr: true},
		{name: "unknown operator is rejected, not ignored", op: "approximately", value: "0", wantErr: true},
		{name: "non-numeric value is rejected", op: "gt", value: "lots", wantErr: true},
		{name: "float notation is rejected as inexact", op: "gt", value: "1e3", wantErr: true},
		{name: "more than three decimals is rejected", op: "gt", value: "0.0001", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := pinnedService(repo)
			_, err := svc.ListRationRates(context.Background(), "tenant", RationRateFilter{
				ParkID: "park", GramsOp: tc.op, GramsValue: tc.value,
			})
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidFilter) {
					t.Fatalf("error = %v, want ErrInvalidFilter", err)
				}
				// A rejected filter must not reach the database at all: a query that ran and
				// returned rows would be answering the malformed question.
				if repo.lastRateQuery.ParkID != "" {
					t.Fatalf("repository was queried for a rejected filter: %+v", repo.lastRateQuery)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := repo.lastRateQuery.GramsCompare
			if tc.wantCompare == nil {
				if got != nil {
					t.Fatalf("comparison = %+v, want nil (no filter)", got)
				}
				return
			}
			if got == nil {
				t.Fatal("comparison = nil, want a filter")
			}
			if got.Op != tc.wantCompare.Op || got.Value != tc.wantCompare.Value {
				t.Fatalf("comparison = %+v, want %+v", got, tc.wantCompare)
			}
		})
	}
}

// TestFeedItemSetIsAMatchSetNotAMatchNothing pins the nil-vs-empty distinction the SQL depends on.
//
// An empty text[] bound into `= ANY(...)` matches NO rows. If a set of blanks collapsed to an empty
// slice instead of nil, a filter nobody applied would blank the grid -- and an empty ration grid
// reads on this screen as "every shed resolving here is BLOCKED", which is a statement about the
// farm rather than about the filter.
func TestFeedItemSetIsAMatchSetNotAMatchNothing(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "no items is no filter", in: nil, want: nil},
		{name: "blanks alone collapse to no filter", in: []string{"", "  "}, want: nil},
		{name: "one item narrows to it", in: []string{"Hybrid"}, want: []string{"Hybrid"}},
		{name: "several items are a set", in: []string{"Hybrid", "COFS"}, want: []string{"Hybrid", "COFS"}},
		{name: "blanks are dropped from a real set", in: []string{"Hybrid", " ", "COFS"}, want: []string{"Hybrid", "COFS"}},
		{name: "values are trimmed", in: []string{"  Hybrid "}, want: []string{"Hybrid"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := pinnedService(repo)
			if _, err := svc.ListRationRates(context.Background(), "tenant", RationRateFilter{
				ParkID: "park", FeedItems: tc.in,
			}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := repo.lastRateQuery.FeedItems
			if len(got) != len(tc.want) {
				t.Fatalf("feed items = %#v, want %#v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("feed items = %#v, want %#v", got, tc.want)
				}
			}
			if tc.want == nil && got != nil {
				t.Fatalf("feed items = %#v, want nil so the SQL reads it as no filter", got)
			}
		})
	}
}

// TestBreedAndRationGroupAreSeparateFilters proves the two are carried independently rather than
// one being folded into the other.
//
// They land on the same column but ask different questions: a ration group is a group, and a breed
// resolves INTO one many-to-one (Beetal and Sirohi share "Beetal/Sirohi"). Folding breed into
// ration_group here would make "show me what a Sirohi eats" silently match nothing, because no
// group is named "Sirohi".
func TestBreedAndRationGroupAreSeparateFilters(t *testing.T) {
	repo := &fakeRepo{}
	svc := pinnedService(repo)
	if _, err := svc.ListRationRates(context.Background(), "tenant", RationRateFilter{
		ParkID: "park", Breed: " Sirohi ", RationGroup: " Beetal/Sirohi ",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastRateQuery.Breed != "Sirohi" {
		t.Fatalf("breed = %q, want %q", repo.lastRateQuery.Breed, "Sirohi")
	}
	if repo.lastRateQuery.RationGroup != "Beetal/Sirohi" {
		t.Fatalf("ration group = %q, want %q", repo.lastRateQuery.RationGroup, "Beetal/Sirohi")
	}
}

// TestRepositoryErrorsPassThrough proves the service does not swallow or reinterpret the
// repository's idempotency verdict -- the handler needs it verbatim to answer 409.
func TestRepositoryErrorsPassThrough(t *testing.T) {
	repo := &fakeRepo{err: ports.ErrIdempotencyConflict}
	svc := pinnedService(repo)
	_, err := svc.UpsertRationRate(context.Background(), UpsertRationRateInput{
		TenantID: "tenant", ActorRef: "actor", ParkID: "park",
		RationGroupLabel: "Boer", ShedTagLabel: "Pregnant", FeedItemLabel: "Concentrate",
		GramsPerHead: str("250"), IdempotencyKey: "key-12345678", RequestFingerprint: "fp",
	})
	if !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("error = %v, want ErrIdempotencyConflict", err)
	}
}

// =================================================================================================
// Experiment sheds
// =================================================================================================

// TestUpsertExperimentConfigRejectsAbsentGramsPerHead is the experiment twin of
// TestUpsertRationRateRejectsAbsentGramsWithoutDefaulting, and the consequence of getting it wrong
// is arguably worse.
//
// On the ration grid, a missing rate BLOCKS the shed: the failure is loud and an operator sees a
// gap. Here it is silent. Membership in feed_experiment_config IS the workflow flag, so a shed whose
// cells were never authored does not appear as unconfigured -- it falls through to NormalPlanner and
// is fed head_count x grams_per_head x shed_factor off the ration grid. Measured on the live
// 2026-07-20 data that was roughly 2.2x the authored quantity (398.8 kg vs 182.0 kg of CBE
// concentrate) printed on a sheet that looked complete.
//
// So an absent grams_per_head must fail, and it must fail BEFORE any side effect.
func TestUpsertExperimentConfigRejectsAbsentGramsPerHead(t *testing.T) {
	repo := &fakeRepo{}
	svc := pinnedService(repo)

	_, err := svc.UpsertExperimentConfig(context.Background(), UpsertExperimentConfigInput{
		TenantID: "tenant", ActorRef: "actor", ParkID: "park", ShedID: "shed",
		FeedItemLabel: "RGS Concentrate", ExperimentCategory: "Sheep M NEW",
		GramsPerHead:       nil, // ABSENT
		IdempotencyKey:     "key-12345678",
		RequestFingerprint: "fp",
	})
	if !errors.Is(err, domain.ErrMissingField) {
		t.Fatalf("absent grams_per_head error = %v, want ErrMissingField", err)
	}
	var fe *domain.FieldError
	if !errors.As(err, &fe) || fe.Field != "grams_per_head" {
		t.Fatalf("error = %v, want a FieldError naming grams_per_head", err)
	}
	if repo.writeCalls != 0 {
		t.Fatalf("repository was called %d times for a rejected write, want 0", repo.writeCalls)
	}
}

// TestUpsertExperimentConfigAcceptsAuthoredZero: 0 g is a REAL authored quantity here, exactly as
// 0 g is on the ration grid. An arm that deliberately gets none of an item is part of the experiment
// design -- the live CBE data has "Castro 1" on 0.0 kg of three of its five items -- so it must
// reach the repository as an exact "0.000" rather than being treated as missing.
func TestUpsertExperimentConfigAcceptsAuthoredZero(t *testing.T) {
	repo := &fakeRepo{}
	svc := pinnedService(repo)

	if _, err := svc.UpsertExperimentConfig(context.Background(), UpsertExperimentConfigInput{
		TenantID: "tenant", ActorRef: "actor", ParkID: "park", ShedID: "shed",
		FeedItemLabel: "Mesha Concentrate Goat", ExperimentCategory: "Sheep M NEW",
		GramsPerHead:       str("0"),
		IdempotencyKey:     "key-12345678",
		RequestFingerprint: "fp",
	}); err != nil {
		t.Fatalf("authored zero rejected: %v", err)
	}
	if repo.lastExperiment.GramsPerHead != "0.000" {
		t.Fatalf("grams_per_head = %q, want %q", repo.lastExperiment.GramsPerHead, "0.000")
	}
}

// NOBODY TYPES A HEAD COUNT ANY MORE (maintainer instruction 2026-09-01, "use live only, forget
// recorded"). The command carries no such field, and a client that still sends one is refused rather
// than quietly ignored: an accepted-and-dropped field reads to the sender as recorded.
//
// This is the inverse of the test it replaces, which locked the nullable "not recorded" state of a
// figure the author typed. That figure disagreed with the pen's actual population on 15 of 34 live
// pens, which is why it stopped being read, written and asked for.
func TestUpsertExperimentConfigTakesNoHeadCount(t *testing.T) {
	repo := &fakeRepo{}
	if _, err := pinnedService(repo).UpsertExperimentConfig(context.Background(), UpsertExperimentConfigInput{
		TenantID: "tenant", ActorRef: "actor", ParkID: "park", ShedID: "shed",
		FeedItemLabel: "Vijay Concentrate", ExperimentCategory: "Goat F NEW",
		GramsPerHead:       str("12.5"),
		IdempotencyKey:     "key-12345678",
		RequestFingerprint: "fp",
	}); err != nil {
		t.Fatalf("write without a head count rejected: %v", err)
	}
	if repo.writeCalls != 1 {
		t.Fatalf("repository calls = %d, want 1", repo.writeCalls)
	}
}

// TestUpsertExperimentConfigRequiresTheExperimentArm guards the field the direction sheet prints in
// the shed-tag column for an experiment shed. Blank there produces a row that reads like an ordinary
// untagged shed, removing the operator's only cue that these numbers were hand-entered rather than
// computed from the grid.
func TestUpsertExperimentConfigRequiresTheExperimentArm(t *testing.T) {
	repo := &fakeRepo{}
	_, err := pinnedService(repo).UpsertExperimentConfig(context.Background(), UpsertExperimentConfigInput{
		TenantID: "tenant", ActorRef: "actor", ParkID: "park", ShedID: "shed",
		FeedItemLabel: "RGS Concentrate", ExperimentCategory: "   ",
		GramsPerHead:       str("10"),
		IdempotencyKey:     "key-12345678",
		RequestFingerprint: "fp",
	})
	var fe *domain.FieldError
	if !errors.As(err, &fe) || fe.Field != "experiment_category" {
		t.Fatalf("blank arm error = %v, want a FieldError naming experiment_category", err)
	}
	if repo.writeCalls != 0 {
		t.Fatalf("repository was called for a rejected write")
	}
}

// TestSetExperimentShedStatusRequiresAnExplicitStatus proves the workflow switch has NO default.
//
// The two values are the two feeding regimes: 'active' feeds the shed authored absolute kg,
// 'retired' returns it to projected head count x grams per head x shed factor. Neither is a safe
// fallback for a caller who did not say, because either choice silently changes what the animals
// eat. An unrecognised value is rejected for the same reason rather than coerced to the nearer one.
func TestSetExperimentShedStatusRequiresAnExplicitStatus(t *testing.T) {
	for name, status := range map[string]string{
		"absent":       "",
		"unrecognised": "paused",
	} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepo{}
			_, err := pinnedService(repo).SetExperimentShedStatus(context.Background(), SetExperimentShedStatusInput{
				TenantID: "tenant", ActorRef: "actor", ParkID: "park", ShedID: "shed",
				Status:             status,
				IdempotencyKey:     "key-12345678",
				RequestFingerprint: "fp",
			})
			var fe *domain.FieldError
			if !errors.As(err, &fe) || fe.Field != "status" {
				t.Fatalf("status %q error = %v, want a FieldError naming status", status, err)
			}
			if repo.writeCalls != 0 {
				t.Fatalf("repository was called for a rejected workflow switch")
			}
		})
	}

	// Both legal values reach the repository verbatim.
	for _, want := range []string{domain.ExperimentStatusActive, domain.ExperimentStatusRetired} {
		repo := &fakeRepo{}
		if _, err := pinnedService(repo).SetExperimentShedStatus(context.Background(), SetExperimentShedStatusInput{
			TenantID: "tenant", ActorRef: "actor", ParkID: "park", ShedID: "shed",
			Status:             strings.ToUpper(want), // case-insensitive on the way in
			IdempotencyKey:     "key-12345678",
			RequestFingerprint: "fp",
		}); err != nil {
			t.Fatalf("status %q rejected: %v", want, err)
		}
		if repo.lastExperimentStatus.Status != want {
			t.Fatalf("status = %q, want %q", repo.lastExperimentStatus.Status, want)
		}
	}
}

// TestListExperimentConfigDefaultsToBothStatuses locks the read default.
//
// A withdrawn shed's rows are retired rather than deleted, and the config screen must keep showing
// them: they are the authored quantities that come back if the shed is restored, and hiding them
// would make an accidental withdrawal invisible on the very screen that owns the decision. An empty
// status filter must therefore stay empty (meaning BOTH) rather than being defaulted to 'active'.
func TestListExperimentConfigDefaultsToBothStatuses(t *testing.T) {
	repo := &fakeRepo{}
	if _, err := pinnedService(repo).ListExperimentConfig(context.Background(), "tenant", ExperimentConfigFilter{ParkID: "park"}); err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if repo.lastExperimentQuery.Status != "" {
		t.Fatalf("status filter = %q, want empty (both statuses)", repo.lastExperimentQuery.Status)
	}

	// A present-but-unrecognised status is rejected rather than silently dropped: dropping it would
	// widen the result to both statuses for a caller who asked for one, and the two statuses are two
	// different feeding regimes.
	repo2 := &fakeRepo{}
	if _, err := pinnedService(repo2).ListExperimentConfig(context.Background(), "tenant", ExperimentConfigFilter{ParkID: "park", Status: "paused"}); err == nil {
		t.Fatal("unrecognised status filter was accepted; it must be rejected rather than ignored")
	}
}

// ---------------------------------------------------------------------------
// Feed items (the catalog)
// ---------------------------------------------------------------------------

// TestCreateFeedItemKeepsUnmeasuredAttributesNull is the feed-item twin of the absent-vs-zero tests
// above, and it asserts the OPPOSITE conclusion for a reason worth stating.
//
// On a ration rate, an absent value is REJECTED: a missing rate means "not configured", which the
// feed path must block on, so filling it in with 0 would author "feed nothing" for a shed nobody
// configured. On a catalog attribute, an absent value is ACCEPTED AS NULL: nobody measured the
// item's energy, which blocks a nutritional rollup and nothing else.
//
// What must hold in BOTH directions is that absence is never converted into a number. A nil energy
// that arrived at the repository as "0.000" would state that someone measured the item as carrying
// no energy at all.
func TestCreateFeedItemKeepsUnmeasuredAttributesNull(t *testing.T) {
	repo := &fakeRepo{}
	svc := pinnedService(repo)

	if _, err := svc.CreateFeedItem(context.Background(), CreateFeedItemInput{
		TenantID: "tenant", ActorRef: "actor",
		FeedItemLabel:      "RGS Concentrate",
		IdempotencyKey:     "key-12345678",
		RequestFingerprint: "fp",
	}); err != nil {
		t.Fatalf("feed item with no attributes rejected: %v", err)
	}
	cmd := repo.lastFeedItem
	if cmd.FeedItemLabel != "RGS Concentrate" {
		t.Fatalf("feed_item = %q, want %q", cmd.FeedItemLabel, "RGS Concentrate")
	}
	for name, got := range map[string]*string{
		"energy_kcal_per_kg": cmd.EnergyKcalPerKg,
		"dry_matter_factor":  cmd.DryMatterFactor,
		"wastage_factor":     cmd.WastageFactor,
	} {
		if got != nil {
			t.Fatalf("%s = %q, want nil — an unmeasured attribute must reach the repository as NULL, never as a number", name, *got)
		}
	}
	// An absent display_order stays nil so the WRITE PATH can append the item to the end of the
	// catalog. Resolving it to 0 here would place every new item first in every dropdown.
	if cmd.DisplayOrder != nil {
		t.Fatalf("display_order = %d, want nil so the write path appends to the end", *cmd.DisplayOrder)
	}
}

// TestCreateFeedItemKeepsMeasuredZeroDistinctFromUnmeasured is the other half of the same rule: an
// explicit 0 is a MEASUREMENT and must survive as one. Wastage is the honest example — an item with
// no expected wastage really is 0.0000, and collapsing that into "not measured" would lose a fact
// the author recorded.
func TestCreateFeedItemKeepsMeasuredZeroDistinctFromUnmeasured(t *testing.T) {
	repo := &fakeRepo{}
	svc := pinnedService(repo)

	if _, err := svc.CreateFeedItem(context.Background(), CreateFeedItemInput{
		TenantID: "tenant", ActorRef: "actor",
		FeedItemLabel:      "Dry Masoor Bhusa",
		EnergyKcalPerKg:    str("0"),
		WastageFactor:      str("0"),
		DryMatterFactor:    str("0.85"),
		IdempotencyKey:     "key-12345678",
		RequestFingerprint: "fp",
	}); err != nil {
		t.Fatalf("authored zero attributes rejected: %v", err)
	}
	cmd := repo.lastFeedItem
	if cmd.EnergyKcalPerKg == nil || *cmd.EnergyKcalPerKg != "0.000" {
		t.Fatalf("energy_kcal_per_kg = %v, want an exact authored %q", cmd.EnergyKcalPerKg, "0.000")
	}
	if cmd.WastageFactor == nil || *cmd.WastageFactor != "0.0000" {
		t.Fatalf("wastage_factor = %v, want an exact authored %q", cmd.WastageFactor, "0.0000")
	}
	// Canonicalized to the column's scale, never rounded away from what was typed.
	if cmd.DryMatterFactor == nil || *cmd.DryMatterFactor != "0.8500" {
		t.Fatalf("dry_matter_factor = %v, want %q", cmd.DryMatterFactor, "0.8500")
	}
}

// TestCreateFeedItemRejectsOutOfRangeAttributes locks the bounds to the columns' own CHECKs, and
// locks that they are REJECTIONS rather than clamps. A dry-matter factor of 1.5 says the item is
// 150% dry matter; a wastage factor of 1 says the entire quantity is lost. Neither is a value to
// quietly pull into range on the author's behalf.
//
// It also proves the repository was never called, so nothing was written for a rejected add.
func TestCreateFeedItemRejectsOutOfRangeAttributes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input CreateFeedItemInput
		field string
	}{
		{"dry matter above 1", CreateFeedItemInput{DryMatterFactor: str("1.5")}, "dry_matter_factor"},
		// 0 is excluded on dry matter (an item that is entirely water is not a feed), which is the
		// one place these three attributes disagree about zero.
		{"dry matter of zero", CreateFeedItemInput{DryMatterFactor: str("0")}, "dry_matter_factor"},
		{"wastage of one", CreateFeedItemInput{WastageFactor: str("1")}, "wastage_factor"},
		{"wastage above one", CreateFeedItemInput{WastageFactor: str("1.2")}, "wastage_factor"},
		{"negative energy", CreateFeedItemInput{EnergyKcalPerKg: str("-1")}, "energy_kcal_per_kg"},
		{"negative display order", CreateFeedItemInput{DisplayOrder: i32(-1)}, "display_order"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			in := tc.input
			in.TenantID, in.ActorRef = "tenant", "actor"
			in.FeedItemLabel = "Some Item"
			in.IdempotencyKey, in.RequestFingerprint = "key-12345678", "fp"

			_, err := pinnedService(repo).CreateFeedItem(context.Background(), in)
			var fe *domain.FieldError
			if !errors.As(err, &fe) || fe.Field != tc.field {
				t.Fatalf("error = %v, want a FieldError naming %s", err, tc.field)
			}
			if repo.writeCalls != 0 {
				t.Fatalf("repository was called %d times for a rejected add, want 0", repo.writeCalls)
			}
		})
	}
}

// TestCreateFeedItemDryMatterUpperBoundIsInclusive pins the boundary the two factors treat
// differently, because "0 to 1" is ambiguous prose and the columns are not: dry matter may BE 1
// (an entirely dry item), wastage may not (nothing would reach the animals).
func TestCreateFeedItemDryMatterUpperBoundIsInclusive(t *testing.T) {
	repo := &fakeRepo{}
	if _, err := pinnedService(repo).CreateFeedItem(context.Background(), CreateFeedItemInput{
		TenantID: "tenant", ActorRef: "actor",
		FeedItemLabel:      "Fully Dry Item",
		DryMatterFactor:    str("1"),
		IdempotencyKey:     "key-12345678",
		RequestFingerprint: "fp",
	}); err != nil {
		t.Fatalf("dry_matter_factor of exactly 1 rejected: %v — the column allows it", err)
	}
	if got := repo.lastFeedItem.DryMatterFactor; got == nil || *got != "1.0000" {
		t.Fatalf("dry_matter_factor = %v, want %q", got, "1.0000")
	}
}

// TestCreateFeedItemRequiresAName: the label is the item's identity and the whole point of the
// write. A blank one is a missing field, never an empty-named catalog row.
func TestCreateFeedItemRequiresAName(t *testing.T) {
	repo := &fakeRepo{}
	_, err := pinnedService(repo).CreateFeedItem(context.Background(), CreateFeedItemInput{
		TenantID: "tenant", ActorRef: "actor",
		FeedItemLabel:      "   ",
		IdempotencyKey:     "key-12345678",
		RequestFingerprint: "fp",
	})
	var fe *domain.FieldError
	if !errors.As(err, &fe) || fe.Field != "feed_item" {
		t.Fatalf("error = %v, want a FieldError naming feed_item", err)
	}
	if repo.writeCalls != 0 {
		t.Fatalf("repository was called %d times for a rejected add, want 0", repo.writeCalls)
	}
}

// TestCreateFeedItemSucceedsWithoutAParkAndDatesTheLedger covers the two things that make this
// write different from every other one in the module.
//
// It takes NO park — feed_item_catalog is keyed (tenant, item), so the vocabulary is shared and an
// add cannot be scoped to one park in a way that leaves the other unable to author rates for it.
// Every other write here would fail without a park_id; this one must not.
//
// And the business date still travels on the write identity, so the ledger records WHEN the
// vocabulary changed even though the catalog itself is not effective-dated. The pinned clock is
// late in the UTC day, so a service deriving the date from UTC would record 2026-07-19.
func TestCreateFeedItemSucceedsWithoutAParkAndDatesTheLedger(t *testing.T) {
	repo := &fakeRepo{}
	if _, err := pinnedService(repo).CreateFeedItem(context.Background(), CreateFeedItemInput{
		TenantID: "tenant", ActorRef: "actor",
		FeedItemLabel:      "Tenant Wide Item",
		IdempotencyKey:     "key-12345678",
		RequestFingerprint: "fp",
	}); err != nil {
		t.Fatalf("tenant-scoped add rejected: %v", err)
	}
	if repo.lastFeedItem.EffectiveFrom != "2026-07-20" {
		t.Fatalf("effective_from = %q, want the Asia/Kolkata business date 2026-07-20", repo.lastFeedItem.EffectiveFrom)
	}
}
