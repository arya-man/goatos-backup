package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// TaskCreator spawns one SOP task per batch. Implemented by a thin adapter over the SOP module
// (sop.CreateTask) at wiring time; the sweeper stays decoupled from SOP types. nil disables spawn.
type TaskCreator interface {
	CreateTaskForBatch(ctx context.Context, tenantID, batchID, sopVersionID, taskType, title, scopeType, scopeID string) (taskID string, err error)
}

// BatchTaskCreate is one SOP task spawn request for a planned batch.
type BatchTaskCreate struct {
	BatchID      string
	SOPVersionID string
	TaskType     string
	Title        string
	ScopeType    string
	ScopeID      string
}

// BatchTaskCreator lets the sweeper spawn a page of SOP tasks through one adapter call.
type BatchTaskCreator interface {
	CreateTasksForBatches(ctx context.Context, tenantID string, batches []BatchTaskCreate) (map[string]string, error)
}

// StockReserver reserves doses for a batch (FEFO). Satisfied by the inventory app service; nil
// disables reserve. Best-effort: no stock is a no-op.
type StockReserver interface {
	ReserveForBatch(ctx context.Context, tenantID, batchID, locationID, itemID string, qty int64, validOn time.Time) error
}

// BatchStockReservation is one stock reservation request for a planned batch/rule item.
type BatchStockReservation struct {
	BatchID    string
	LocationID string
	ItemID     string
	Qty        int64
	ValidOn    time.Time
}

// StockReservationKey returns the stable key used in BatchStockReserver result maps.
func StockReservationKey(req BatchStockReservation) string {
	return req.BatchID + "\x00" + req.ItemID
}

// BatchStockReserver lets the sweeper reserve a page of planned batches through one adapter call.
// The returned map is keyed by StockReservationKey; an entry means that request failed.
type BatchStockReserver interface {
	ReserveForBatches(ctx context.Context, tenantID string, reservations []BatchStockReservation) (map[string]error, error)
}

// BatchStockBlock records stock-block metadata for one failed reservation.
type BatchStockBlock = domain.BatchStockBlock

type batchingHoldRecorder interface {
	RecordBatchingHoldForObligations(ctx context.Context, tenantID string, obligationIDs []string, holdUntil time.Time, occurredAt time.Time) (int64, error)
}

type clinicalSweepDeferrer interface {
	DeferBlockedVaccinationSweepCandidates(ctx context.Context, tenantID, versionID string, dueBefore, occurredAt time.Time, limit int32) (int, error)
}

type plannedBatchRuleCounter interface {
	CountAttachedObligationsByRuleForBatches(ctx context.Context, tenantID string, batchIDs []string) (map[string][]domain.RuleAttachmentCount, error)
}

type plannedBatchTaskLinker interface {
	SetBatchSOPTasks(ctx context.Context, tenantID string, taskIDsByBatch map[string]string) error
}

type plannedBatchStockStateWriter interface {
	MarkBatchStockBlocks(ctx context.Context, tenantID string, blocks []BatchStockBlock) error
	ClearBatchStockBlocks(ctx context.Context, tenantID string, batchIDs []string) error
}

// RuleVaccineIdentity holds per-rule vaccine identity: code, priority, compatibility group.
// Used by the sweeper to thread vaccine identity through cap/tie detection and grouping.
type RuleVaccineIdentity struct {
	VaccineCode      string
	VaccinePriority  int32
	CompatibilityGrp string
	VaccineItemID    string
}

// SweepConfig carries the per-version batch config resolved by the caller from the protocol version:
// the SOP to instantiate, the vaccine item to reserve, and doses per goat.
type SweepConfig struct {
	SOPVersionID      string
	VaccineItemID     string
	VaccineCode       string
	DosesPerGoat      int32
	RuleConfigs       map[string]SweepRuleConfig
	ParkConsolidation domain.ParkConsolidationSettings
	DrivePlanner      domain.DrivePlannerSettings
	// RuleVaccineIDs is the per-rule vaccine identity cache (BUG #1 / R2-05(a) fix). A builder MUST
	// only store a COMPLETE identity here (non-empty VaccineCode) -- never an empty
	// RuleVaccineIdentity{} for a rule whose matrix extraction failed or found no vaccine. An absent
	// or cached-empty entry is treated identically by getRuleVaccineIdentity: both fall back to the
	// version-level identity (VaccineCode/VaccinePriority above).
	RuleVaccineIDs map[string]RuleVaccineIdentity
}

// SweepRuleConfig overrides version-level execution bindings for one protocol rule.
type SweepRuleConfig struct {
	SOPVersionID  string
	VaccineItemID string
	DosesPerGoat  int32
}

// SweeperService implements SM-4 batching: collect unbatched due obligations for a version, group
// them by scope/rule/due window, spawn one SOP task per batch, reserve stock, and create one batch per
// operational drive/session. Idempotent: a re-sweep finds no unbatched rows → no new work.
type SweeperService struct {
	repo     ports.Repository
	tasks    TaskCreator
	reserver StockReserver
	page     int32
}

const maxPlannedFinalizationPagesPerSweep = 10000

// NewSweeperService constructs the sweeper. tasks/reserver may be nil to skip spawn/reserve.
func NewSweeperService(repo ports.Repository, tasks TaskCreator, reserver StockReserver) *SweeperService {
	return &SweeperService{repo: repo, tasks: tasks, reserver: reserver, page: 1000}
}

// SetPageSize overrides the sweeper's internal pagination page size (default 1000). It exists so
// tests outside this package can exercise multi-page pagination boundaries (e.g. a combo-alignment
// group that spans more than one page, R2-06) against a small fixture instead of seeding thousands
// of rows to force the default page size to split. n<=0 is ignored; production callers should leave
// the default.
func (s *SweeperService) SetPageSize(n int32) {
	if n > 0 {
		s.page = n
	}
}

// tenantSweepLocker is implemented by the production Postgres repo to serialize the whole sweep per
// tenant (RV-03). Test fakes need not implement it -- single-process tests have no second writer.
type tenantSweepLocker interface {
	LockTenantSweep(ctx context.Context, tenantID string) (bool, func(context.Context) error, error)
}

// LockTenantSweep acquires the per-tenant whole-sweep advisory lock so the sweep is the single
// priority-ordered writer for the tenant (RV-03). acquired=false means another sweeper already owns
// the tenant and this run must skip. A repo that does not implement tenantSweepLocker (test fakes)
// always "acquires" a noop lock, preserving single-process test behavior.
func (s *SweeperService) LockTenantSweep(ctx context.Context, tenantID string) (bool, func(context.Context) error, error) {
	if locker, ok := s.repo.(tenantSweepLocker); ok {
		return locker.LockTenantSweep(ctx, tenantID)
	}
	return true, func(context.Context) error { return nil }, nil
}

// sweepHighWaterMarkCapturer is implemented by the production Postgres repo (RV-05): it returns the
// database server's current time, the frozen boundary that PreflightVisitShotCapTies and the real
// per-version sweep both bound their unbatched-due candidate reads by (created_at <=
// createdAtHWM), so an obligation generated concurrently -- outside the tenant-sweep lock, e.g. by
// vaccination/app's generation.go/booster.go InsertObligation callers -- cannot introduce a
// same-priority tie mid-sweep after earlier plans already committed. A repo that does not implement
// this (simple test fakes, single-process, no concurrent generator to race) is used unbounded.
type sweepHighWaterMarkCapturer interface {
	CaptureSweepHighWaterMark(ctx context.Context) (time.Time, error)
}

// CaptureSweepHighWaterMark returns the RV-05 high-water mark for one locked sweep cycle (zero
// time.Time when the repo does not support it, meaning "unbounded" everywhere it is threaded).
// Callers MUST capture this ONCE per cycle -- right after acquiring LockTenantSweep and before
// calling PreflightVisitShotCapTiesWithSnapshot -- and pass the same value plus the returned
// snapshot to every SweepVersionWithSessionNoFinalizeSnapshot call in that cycle.
func (s *SweeperService) CaptureSweepHighWaterMark(ctx context.Context) (time.Time, error) {
	if capturer, ok := s.repo.(sweepHighWaterMarkCapturer); ok {
		return capturer.CaptureSweepHighWaterMark(ctx)
	}
	return time.Time{}, nil
}

// hwmUnbatchedDueLister is implemented by the production Postgres repo (RV-05): the real-sweep-path
// sibling of unbatchedDueKeysetLister (preflight.go), bounding the unbatched-due read ALSO by
// created_at <= createdAtHWM.
type hwmUnbatchedDueLister interface {
	ListUnbatchedDueForVersionHWM(ctx context.Context, tenantID, versionID string, dueBefore time.Time, limit int32, createdAtHWM time.Time) ([]domain.UnbatchedDue, error)
}

type snapshotUnbatchedDueLister interface {
	ListUnbatchedDueForVersionSnapshot(ctx context.Context, tenantID, versionID string, dueBefore time.Time, limit int32, createdAtHWM time.Time, candidateIDs []string) ([]domain.UnbatchedDue, error)
}

// listUnbatchedDueForVersionBounded reads the unbatched-due candidate page, bounded by createdAtHWM
// when both createdAtHWM is set AND the repo supports hwmUnbatchedDueLister (RV-05); otherwise it
// falls back to the plain, unbounded ListUnbatchedDueForVersion (test fakes; or createdAtHWM left
// zero by a caller not participating in the HWM protocol -- e.g. the standalone
// SweepVersion/SweepVersionWithSession/SweepVersionWithSessionNoFinalize entry points, which keep
// their pre-RV-05 unbounded behavior so no existing caller's semantics change).
func (s *SweeperService) listUnbatchedDueForVersionBounded(ctx context.Context, tenantID, versionID string, dueBefore time.Time, limit int32, createdAtHWM time.Time, candidateIDs []string) ([]domain.UnbatchedDue, error) {
	if candidateIDs != nil {
		if lister, ok := s.repo.(snapshotUnbatchedDueLister); ok {
			return lister.ListUnbatchedDueForVersionSnapshot(ctx, tenantID, versionID, dueBefore, limit, createdAtHWM, candidateIDs)
		}
		rows, err := s.repo.ListUnbatchedDueForVersion(ctx, tenantID, versionID, dueBefore, limit)
		return filterUnbatchedDueSnapshot(rows, candidateIDs), err
	}
	if !createdAtHWM.IsZero() {
		if lister, ok := s.repo.(hwmUnbatchedDueLister); ok {
			return lister.ListUnbatchedDueForVersionHWM(ctx, tenantID, versionID, dueBefore, limit, createdAtHWM)
		}
	}
	return s.repo.ListUnbatchedDueForVersion(ctx, tenantID, versionID, dueBefore, limit)
}

// hwmParkConsolidationLister is the RV-05 sibling of hwmUnbatchedDueLister for the park-
// consolidation candidate read (park_consolidation.go / preflight.go's park replay).
type hwmParkConsolidationLister interface {
	ListUnbatchedShedDueForParkConsolidationHWM(ctx context.Context, tenantID, versionID string, dueBefore time.Time, limit int32, after *domain.ParkConsolidationCursor, createdAtHWM time.Time) ([]domain.ParkConsolidationCandidate, error)
}

type snapshotParkConsolidationLister interface {
	ListUnbatchedShedDueForParkConsolidationSnapshot(ctx context.Context, tenantID, versionID string, dueBefore time.Time, limit int32, after *domain.ParkConsolidationCursor, createdAtHWM time.Time, candidateIDs []string) ([]domain.ParkConsolidationCandidate, error)
}

// listUnbatchedShedDueForParkConsolidationBounded is listUnbatchedDueForVersionBounded's sibling for
// the park-consolidation candidate read.
func (s *SweeperService) listUnbatchedShedDueForParkConsolidationBounded(ctx context.Context, tenantID, versionID string, dueBefore time.Time, limit int32, after *domain.ParkConsolidationCursor, createdAtHWM time.Time, candidateIDs []string) ([]domain.ParkConsolidationCandidate, error) {
	if candidateIDs != nil {
		if lister, ok := s.repo.(snapshotParkConsolidationLister); ok {
			return lister.ListUnbatchedShedDueForParkConsolidationSnapshot(ctx, tenantID, versionID, dueBefore, limit, after, createdAtHWM, candidateIDs)
		}
		rows, err := s.repo.ListUnbatchedShedDueForParkConsolidation(ctx, tenantID, versionID, dueBefore, limit, after)
		return filterParkConsolidationSnapshot(rows, candidateIDs), err
	}
	if !createdAtHWM.IsZero() {
		if lister, ok := s.repo.(hwmParkConsolidationLister); ok {
			return lister.ListUnbatchedShedDueForParkConsolidationHWM(ctx, tenantID, versionID, dueBefore, limit, after, createdAtHWM)
		}
	}
	return s.repo.ListUnbatchedShedDueForParkConsolidation(ctx, tenantID, versionID, dueBefore, limit, after)
}

func snapshotIDSet(candidateIDs []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(candidateIDs))
	for _, id := range candidateIDs {
		allowed[id] = struct{}{}
	}
	return allowed
}

func filterUnbatchedDueSnapshot(rows []domain.UnbatchedDue, candidateIDs []string) []domain.UnbatchedDue {
	allowed := snapshotIDSet(candidateIDs)
	out := rows[:0]
	for _, row := range rows {
		if _, ok := allowed[row.ObligationID]; ok {
			out = append(out, row)
		}
	}
	return out
}

func filterParkConsolidationSnapshot(rows []domain.ParkConsolidationCandidate, candidateIDs []string) []domain.ParkConsolidationCandidate {
	allowed := snapshotIDSet(candidateIDs)
	out := rows[:0]
	for _, row := range rows {
		if _, ok := allowed[row.ObligationID]; ok {
			out = append(out, row)
		}
	}
	return out
}

func snapshotIDChunks(candidateIDs []string, size int32) [][]string {
	if len(candidateIDs) == 0 {
		return [][]string{{}}
	}
	if size <= 0 {
		size = 1000
	}
	n := int(size)
	chunks := make([][]string, 0, (len(candidateIDs)+n-1)/n)
	for start := 0; start < len(candidateIDs); start += n {
		end := start + n
		if end > len(candidateIDs) {
			end = len(candidateIDs)
		}
		chunks = append(chunks, candidateIDs[start:end])
	}
	return chunks
}

func appendSnapshotIDs(dst []string, seen map[string]struct{}, ids []string) []string {
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		dst = append(dst, id)
	}
	return dst
}

// SweepVersion batches all currently-unbatched due obligations for a version (due_at <= dueBefore).
// It uses a private, single-call shot-cap session: MaxShotsPerAnimalPerDrive is enforced only
// within this one version's own obligations. A caller sweeping multiple protocol
// versions/vaccines for the same due window (the production obligation-sweeper) must share ONE
// session across those calls via SweepVersionWithSession, or the per-animal cap silently resets
// per vaccine and can over-schedule an animal with more than MaxShotsPerAnimalPerDrive shots on
// one visit.
func (s *SweeperService) SweepVersion(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time) (domain.SweepResult, error) {
	return s.SweepVersionAsOf(ctx, tenantID, versionID, cfg, dueBefore, dueBefore)
}

// SweepVersionAsOf batches all dueBefore-eligible obligations while using asOf as the operational
// business day for hold/backdate/planned-date decisions. Backfills and wide-window proofs must pass
// the real sweep day as asOf and the eligibility horizon as dueBefore.
func (s *SweeperService) SweepVersionAsOf(ctx context.Context, tenantID, versionID string, cfg SweepConfig, asOf, dueBefore time.Time) (domain.SweepResult, error) {
	return s.sweepVersion(ctx, tenantID, versionID, cfg, asOf, dueBefore, NewSweepSession(), true, time.Time{}, nil)
}

// SweepVersionWithSession behaves like SweepVersion but claims MaxShotsPerAnimalPerDrive slots
// against the caller-supplied session, so the cap spans every version swept against that same
// session in one run. Vaccines/versions competing for a shared visit should be swept in
// ascending resolved-priority order (see SortSweepVersionsByPriority) so higher-priority
// vaccines claim an over-subscribed animal's slots first; an unresolved same-priority conflict
// is reported as *ShotCapPriorityTieError rather than resolved by call/arrival order.
func (s *SweeperService) SweepVersionWithSession(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time, session *SweepSession) (domain.SweepResult, error) {
	return s.SweepVersionWithSessionAsOf(ctx, tenantID, versionID, cfg, dueBefore, dueBefore, session)
}

// SweepVersionWithSessionAsOf is SweepVersionWithSession with split asOf/dueBefore semantics.
func (s *SweeperService) SweepVersionWithSessionAsOf(ctx context.Context, tenantID, versionID string, cfg SweepConfig, asOf, dueBefore time.Time, session *SweepSession) (domain.SweepResult, error) {
	return s.sweepVersion(ctx, tenantID, versionID, cfg, asOf, dueBefore, sessionOrNew(session), true, time.Time{}, nil)
}

// SweepVersionWithSessionNoFinalize is like SweepVersionWithSession but defers BOTH stock
// reservation AND SOP-task creation/linking for every planned batch of this version -- fresh ones
// created by this call AND any pre-existing/retry batch left over from an interrupted prior run --
// for use in the kernel stage where AlignComboDrives must run before a combo batch's stock/task are
// finalized against its FINAL planned_date (BUG #6 / R2-04 fix). The caller MUST finalize every
// swept plan exactly once via FinalizePlannedBatches AFTER AlignComboDrives has run for this pass;
// until then, no batch produced or repaired by this call has its stock reserved or its SOP task
// created.
func (s *SweeperService) SweepVersionWithSessionNoFinalize(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time, session *SweepSession) (domain.SweepResult, error) {
	return s.SweepVersionWithSessionNoFinalizeAsOf(ctx, tenantID, versionID, cfg, dueBefore, dueBefore, session)
}

// SweepVersionWithSessionNoFinalizeAsOf is SweepVersionWithSessionNoFinalize with split
// asOf/dueBefore semantics.
func (s *SweeperService) SweepVersionWithSessionNoFinalizeAsOf(ctx context.Context, tenantID, versionID string, cfg SweepConfig, asOf, dueBefore time.Time, session *SweepSession) (domain.SweepResult, error) {
	return s.sweepVersion(ctx, tenantID, versionID, cfg, asOf, dueBefore, sessionOrNew(session), false, time.Time{}, nil)
}

// SweepVersionWithSessionNoFinalizeHWM behaves exactly like SweepVersionWithSessionNoFinalize but
// bounds every unbatched-due candidate read (main loop, park consolidation, shed fallback) to
// createdAtHWM (RV-05, zero = unbounded, identical to SweepVersionWithSessionNoFinalize). Kept for
// compatibility and focused HWM tests; production orchestration uses
// SweepVersionWithSessionNoFinalizeSnapshot so pre-existing rows cannot enter after preflight.
func (s *SweeperService) SweepVersionWithSessionNoFinalizeHWM(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time, session *SweepSession, createdAtHWM time.Time) (domain.SweepResult, error) {
	return s.SweepVersionWithSessionNoFinalizeHWMAsOf(ctx, tenantID, versionID, cfg, dueBefore, dueBefore, session, createdAtHWM)
}

// SweepVersionWithSessionNoFinalizeHWMAsOf is SweepVersionWithSessionNoFinalizeHWM with split
// asOf/dueBefore semantics.
func (s *SweeperService) SweepVersionWithSessionNoFinalizeHWMAsOf(ctx context.Context, tenantID, versionID string, cfg SweepConfig, asOf, dueBefore time.Time, session *SweepSession, createdAtHWM time.Time) (domain.SweepResult, error) {
	return s.sweepVersion(ctx, tenantID, versionID, cfg, asOf, dueBefore, sessionOrNew(session), false, createdAtHWM, nil)
}

// SweepVersionWithSessionNoFinalizeSnapshot is the production RV-05 entry point. In addition to
// the created-at HWM it constrains every candidate read to IDs observed by the successful
// preflight, closing the deferred->scheduled/reschedule race for pre-existing rows.
func (s *SweeperService) SweepVersionWithSessionNoFinalizeSnapshot(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time, session *SweepSession, createdAtHWM time.Time, snapshot *SweepCandidateSnapshot) (domain.SweepResult, error) {
	return s.SweepVersionWithSessionNoFinalizeSnapshotAsOf(ctx, tenantID, versionID, cfg, dueBefore, dueBefore, session, createdAtHWM, snapshot)
}

// SweepVersionWithSessionNoFinalizeSnapshotAsOf is the production RV-05 entry point with split
// asOf/dueBefore semantics.
func (s *SweeperService) SweepVersionWithSessionNoFinalizeSnapshotAsOf(ctx context.Context, tenantID, versionID string, cfg SweepConfig, asOf, dueBefore time.Time, session *SweepSession, createdAtHWM time.Time, snapshot *SweepCandidateSnapshot) (domain.SweepResult, error) {
	return s.sweepVersion(ctx, tenantID, versionID, cfg, asOf, dueBefore, sessionOrNew(session), false, createdAtHWM, snapshot.candidateIDs(versionID))
}

func (s *SweeperService) sweepVersion(ctx context.Context, tenantID, versionID string, cfg SweepConfig, asOf, dueBefore time.Time, session *SweepSession, finalizeNow bool, createdAtHWM time.Time, candidateIDs []string) (domain.SweepResult, error) {
	var res domain.SweepResult
	// R2-04 fix: only finalize retry/pre-existing planned batches up front when this call owns its
	// own finalization end to end (finalizeNow=true, i.e. no subsequent AlignComboDrives pass is
	// coming for this run). When finalization is deferred (finalizeNow=false), finalizing a
	// pre-existing combo-session batch HERE -- before this run's AlignComboDrives has had a chance to
	// run across every swept plan -- would reserve its stock and/or create its SOP task against its
	// STALE (pre-alignment) planned_date and permanently exclude it from
	// ListPlannedComboBatchesKeyset's candidate set (sop_task_id IS NULL AND NOT context ?
	// 'stock_reservation'), so it could never be aligned again. The caller finalizes every plan --
	// fresh batches AND genuinely-stale retry batches alike -- in one pass, after alignment, via
	// FinalizePlannedBatches.
	if finalizeNow {
		if err := s.finalizePlannedBatches(ctx, tenantID, versionID, cfg); err != nil {
			return res, err
		}
	}
	if err := s.deferBlockedSweepCandidates(ctx, tenantID, versionID, dueBefore); err != nil {
		return res, err
	}
	touchedScopes := make(map[string]bool)
	planner := normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode)
	parkCandidateIDs := candidateIDs
	if candidateIDs != nil {
		// Preserve the snapshot sentinel even when no shed group defers to park:
		// nil means legacy/unbounded, while a non-nil empty slice means the
		// preflight snapshot has no remaining park/fallback candidates.
		parkCandidateIDs = []string{}
		parkCandidateSeen := map[string]struct{}{}
		var snapshotRows []domain.UnbatchedDue
		for _, chunk := range snapshotIDChunks(candidateIDs, s.page) {
			rows, err := s.listUnbatchedDueForVersionBounded(ctx, tenantID, versionID, dueBefore, s.page, createdAtHWM, chunk)
			if err != nil {
				return res, err
			}
			snapshotRows = append(snapshotRows, rows...)
		}
		order, groups := groupUnbatchedDue(snapshotRows, planner.SpeciesGroupingPolicy)
		order = orderDueGroupsByVaccinePriority(order, groups, cfg)

		for _, k := range order {
			g := groups[k]
			if deferShedGroupToPark(cfg, g.scopeType, len(g.ids)) {
				parkCandidateIDs = appendSnapshotIDs(parkCandidateIDs, parkCandidateSeen, g.ids)
				continue
			}
			batched, n, err := s.batchDueGroup(ctx, tenantID, versionID, cfg, planner, asOf, session, g)
			if err != nil {
				return res, err
			}
			if !batched {
				parkCandidateIDs = appendSnapshotIDs(parkCandidateIDs, parkCandidateSeen, g.ids)
				continue
			}
			if !touchedScopes[k] {
				res.Batches++
				touchedScopes[k] = true
			}
			res.Obligations += int(n)
		}
	} else {
		for {
			rows, err := s.listUnbatchedDueForVersionBounded(ctx, tenantID, versionID, dueBefore, s.page, createdAtHWM, nil)
			if err != nil {
				return res, err
			}
			if len(rows) == 0 {
				break
			}

			order, groups := groupUnbatchedDue(rows, planner.SpeciesGroupingPolicy)
			order = orderDueGroupsByVaccinePriority(order, groups, cfg)

			var progressed int64
			for _, k := range order {
				g := groups[k]
				if deferShedGroupToPark(cfg, g.scopeType, len(g.ids)) {
					continue
				}
				batched, n, err := s.batchDueGroup(ctx, tenantID, versionID, cfg, planner, asOf, session, g)
				if err != nil {
					return res, err
				}
				if batched {
					if !touchedScopes[k] {
						res.Batches++
						touchedScopes[k] = true
					}
					res.Obligations += int(n)
					progressed += n
				}
			}
			if progressed == 0 || int32(len(rows)) < s.page {
				break
			}
		}
	}
	parkRes, err := s.consolidateParkDrivesWithVisitCounts(ctx, tenantID, versionID, cfg, asOf, dueBefore, planner, session, createdAtHWM, parkCandidateIDs)
	if err != nil {
		return res, err
	}
	res.ParkBatches = parkRes.ParkBatches
	res.ParkObligations = parkRes.ParkObligations
	res.Batches += parkRes.ParkBatches
	res.Obligations += parkRes.ParkObligations

	fallbackRes, err := s.batchRemainingShedObligationsWithVisitCounts(ctx, tenantID, versionID, cfg, asOf, dueBefore, planner, session, createdAtHWM, parkCandidateIDs)
	if err != nil {
		return res, err
	}
	res.Batches += fallbackRes.Batches
	res.Obligations += fallbackRes.Obligations

	// BUG #6 / R2-04: when finalizeNow is false (kernel stage / one-shot composing this sweep with
	// AlignComboDrives across multiple versions), do NOT finalize stock or SOP tasks here at all --
	// every batch this call created stays planned-only until the caller runs AlignComboDrives and
	// then calls FinalizePlannedBatches once per swept plan.
	if finalizeNow {
		if err := s.finalizePlannedBatches(ctx, tenantID, versionID, cfg); err != nil {
			return res, err
		}
	}
	return res, nil
}

// batchDueGroup plans a drive date and shot-cap-selects one dueGroup's obligations, then creates
// the resulting batch(es) (split by MaxGoatsPerDrive). It seeds session with the persisted,
// cross-pass shot count for every target in the group before selecting (VAX-REV-01), and -- when
// the repo supports it -- holds a per-visit advisory lock across the select+create sequence so a
// concurrent sweeper worker cannot commit a conflicting claim for the same (target, date) visit in
// between. If every row rejects on the first candidate date purely because it is already at cap
// (not a priority tie), the group retries once against the next feasible overflow date, re-seeding
// and re-locking for that new date.
func (s *SweeperService) batchDueGroup(ctx context.Context, tenantID, versionID string, cfg SweepConfig, planner domain.DrivePlannerSettings, asOf time.Time, session *SweepSession, g *dueGroup) (batched bool, obligations int64, err error) {
	plannedDate := batchPlannedDate(g.rows[0].DueAt)
	if planner.Enabled {
		if picked := pickBestDriveDateWithHold(asOf, driveCandidatesFromUnbatched(g.rows), planner); picked != nil {
			plannedDate = picked
		}
	}
	targetIDs := distinctUnbatchedTargetIDs(g.rows)
	release, err := s.lockAndRefreshVisitShots(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session)
	if err != nil {
		return false, 0, err
	}
	// BUG #1: use per-rule vaccine identity instead of version-level wrapper.
	ruleVaccineID := cfg.getRuleVaccineIdentity(g.ruleID)
	selectedIDs, err := selectIDsWithinVisitShotCapForSession(g.rows, plannedDate, planner.MaxShotsPerAnimalPerDrive, ruleVaccineID.VaccineCode, ruleVaccineID.VaccinePriority, session)
	if err != nil {
		_ = release(ctx)
		return false, 0, err
	}
	if len(selectedIDs) == 0 && plannedDate != nil && planner.MaxShotsPerAnimalPerDrive > 0 {
		if overflowDate := nextFeasibleUnbatchedDriveDateAfter(*plannedDate, g.rows); overflowDate != nil {
			if err := release(ctx); err != nil {
				return false, 0, err
			}
			plannedDate = overflowDate
			release, err = s.lockAndRefreshVisitShots(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session)
			if err != nil {
				return false, 0, err
			}
			selectedIDs, err = selectIDsWithinVisitShotCapForSession(g.rows, plannedDate, planner.MaxShotsPerAnimalPerDrive, ruleVaccineID.VaccineCode, ruleVaccineID.VaccinePriority, session)
			if err != nil {
				_ = release(ctx)
				return false, 0, err
			}
		}
	}
	defer func() {
		if relErr := release(ctx); relErr != nil && err == nil {
			err = relErr
		}
	}()

	idChunks := splitObligationIDs(selectedIDs, planner.MaxGoatsPerDrive)
	for _, chunk := range idChunks {
		if len(chunk) == 0 {
			continue
		}
		_, n, createErr := s.repo.CreateBatchWithObligations(ctx, domain.NewBatch{
			TenantID:          tenantID,
			ProtocolVersionID: versionID,
			ScopeType:         g.scopeType,
			ScopeID:           g.scopeID,
			Session:           batchSession(g.ruleID, ruleVaccineID.VaccineCode),
			PlannedDate:       plannedDate,
			WindowStart:       g.windowStart,
			WindowEnd:         g.windowEnd,
			Status:            "planned",
			EstimatedTargets:  int32(len(chunk)),
			PlannedQuantity:   strconv.FormatInt(int64(len(chunk))*int64(normalizedDosesPerGoat(cfg.forRule(g.ruleID).DosesPerGoat)), 10),
			QuantityUnit:      "dose",
		}, chunk)
		if createErr != nil {
			return batched, obligations, createErr
		}
		if n == 0 {
			continue
		}
		batched = true
		if holdErr := s.recordBatchingHoldIfNeeded(ctx, tenantID, chunk, selectedUnbatchedRows(g.rows, chunk), plannedDate, asOf); holdErr != nil {
			return batched, obligations, holdErr
		}
		obligations += n
	}
	return batched, obligations, nil
}

func (s *SweeperService) deferBlockedSweepCandidates(ctx context.Context, tenantID, versionID string, dueBefore time.Time) error {
	deferrer, ok := s.repo.(clinicalSweepDeferrer)
	if !ok {
		return nil
	}
	for {
		// india-date-guard:ignore: owner=ravi issue=GH-india-date scope=sweep-deferral-absolute-instant-storage expiry=2026-12-31
		n, err := deferrer.DeferBlockedVaccinationSweepCandidates(ctx, tenantID, versionID, dueBefore, time.Now().UTC(), s.page)
		if err != nil {
			return err
		}
		if n == 0 || int32(n) < s.page {
			return nil
		}
	}
}

func (s *SweeperService) recordBatchingHoldIfNeeded(ctx context.Context, tenantID string, ids []string, rows []domain.UnbatchedDue, plannedDate *time.Time, occurredAt time.Time) error {
	if plannedDate == nil || len(ids) == 0 || !driveDateUsesBatchingHold(*plannedDate, driveCandidatesFromUnbatched(rows)) {
		return nil
	}
	recorder, ok := s.repo.(batchingHoldRecorder)
	if !ok {
		return nil
	}
	_, err := recorder.RecordBatchingHoldForObligations(ctx, tenantID, ids, *plannedDate, occurredAt)
	return err
}

func selectedUnbatchedRows(rows []domain.UnbatchedDue, selected []string) []domain.UnbatchedDue {
	if len(selected) == 0 {
		return nil
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	out := make([]domain.UnbatchedDue, 0, len(selected))
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; ok {
			out = append(out, row)
		}
	}
	return out
}

func normalizedDosesPerGoat(v int32) int32 {
	if v <= 0 {
		return 1
	}
	return v
}

// deferShedGroupToPark leaves small shed groups unbatched in layer 1 so layer 2 can merge
// singleton leftovers across sheds in the same park.
func deferShedGroupToPark(cfg SweepConfig, scopeType string, obligationCount int) bool {
	if !cfg.ParkConsolidation.Enabled {
		return false
	}
	if scopeType != "shed" {
		return false
	}
	min := cfg.ParkConsolidation.MinShedDriveTargets
	if min <= 0 {
		min = domain.DefaultParkConsolidationSettings().MinShedDriveTargets
	}
	return int32(obligationCount) < min
}

// batchRemainingShedObligations creates shed drives for every still-unbatched shed obligation,
// including missed singletons, so coverage is never left behind after the park merge pass.
func (s *SweeperService) batchRemainingShedObligations(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time) (domain.SweepResult, error) {
	planner := normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode)
	return s.batchRemainingShedObligationsWithVisitCounts(ctx, tenantID, versionID, cfg, dueBefore, dueBefore, planner, NewSweepSession(), time.Time{}, nil)
}

func (s *SweeperService) batchRemainingShedObligationsWithVisitCounts(ctx context.Context, tenantID, versionID string, cfg SweepConfig, asOf, dueBefore time.Time, planner domain.DrivePlannerSettings, session *SweepSession, createdAtHWM time.Time, candidateIDs []string) (domain.SweepResult, error) {
	var res domain.SweepResult
	if !cfg.ParkConsolidation.Enabled {
		return res, nil
	}
	touchedScopes := make(map[string]bool)
	if candidateIDs != nil {
		var snapshotRows []domain.UnbatchedDue
		for _, chunk := range snapshotIDChunks(candidateIDs, s.page) {
			rows, err := s.listUnbatchedDueForVersionBounded(ctx, tenantID, versionID, dueBefore, s.page, createdAtHWM, chunk)
			if err != nil {
				return res, err
			}
			snapshotRows = append(snapshotRows, rows...)
		}
		order, groups := groupUnbatchedDue(filterShedRows(snapshotRows), planner.SpeciesGroupingPolicy)
		order = orderDueGroupsByVaccinePriority(order, groups, cfg)
		for _, k := range order {
			g := groups[k]
			batched, n, err := s.batchDueGroup(ctx, tenantID, versionID, cfg, planner, asOf, session, g)
			if err != nil {
				return res, err
			}
			if batched {
				if !touchedScopes[k] {
					res.Batches++
					touchedScopes[k] = true
				}
				res.Obligations += int(n)
			}
		}
		return res, nil
	}
	for {
		rows, err := s.listUnbatchedDueForVersionBounded(ctx, tenantID, versionID, dueBefore, s.page, createdAtHWM, candidateIDs)
		if err != nil {
			return res, err
		}
		if len(rows) == 0 {
			break
		}
		order, groups := groupUnbatchedDue(filterShedRows(rows), planner.SpeciesGroupingPolicy)
		order = orderDueGroupsByVaccinePriority(order, groups, cfg)

		var progressed int64
		for _, k := range order {
			g := groups[k]
			batched, n, err := s.batchDueGroup(ctx, tenantID, versionID, cfg, planner, asOf, session, g)
			if err != nil {
				return res, err
			}
			if batched {
				if !touchedScopes[k] {
					res.Batches++
					touchedScopes[k] = true
				}
				res.Obligations += int(n)
				progressed += n
			}
		}
		if progressed == 0 || int32(len(rows)) < s.page {
			break
		}
	}
	return res, nil
}

// filterShedRows returns only the shed-scoped rows of rows, preserving order.
func filterShedRows(rows []domain.UnbatchedDue) []domain.UnbatchedDue {
	out := make([]domain.UnbatchedDue, 0, len(rows))
	for _, r := range rows {
		if r.ScopeType == "shed" {
			out = append(out, r)
		}
	}
	return out
}

func (cfg SweepConfig) forRule(ruleID string) SweepRuleConfig {
	out := SweepRuleConfig{
		SOPVersionID:  cfg.SOPVersionID,
		VaccineItemID: cfg.VaccineItemID,
		DosesPerGoat:  cfg.DosesPerGoat,
	}
	if cfg.RuleConfigs == nil {
		return out
	}
	ruleCfg, ok := cfg.RuleConfigs[ruleID]
	if !ok {
		return out
	}
	if ruleCfg.SOPVersionID != "" {
		out.SOPVersionID = ruleCfg.SOPVersionID
	}
	if ruleCfg.VaccineItemID != "" {
		out.VaccineItemID = ruleCfg.VaccineItemID
	}
	if ruleCfg.DosesPerGoat > 0 {
		out.DosesPerGoat = ruleCfg.DosesPerGoat
	}
	return out
}

func (cfg SweepConfig) needsTask() bool {
	if cfg.SOPVersionID != "" {
		return true
	}
	for _, ruleCfg := range cfg.RuleConfigs {
		if ruleCfg.SOPVersionID != "" {
			return true
		}
	}
	return false
}

func (cfg SweepConfig) needsStock() bool {
	if cfg.VaccineItemID != "" {
		return true
	}
	for _, ruleCfg := range cfg.RuleConfigs {
		if ruleCfg.VaccineItemID != "" {
			return true
		}
	}
	return false
}

func (s *SweeperService) finalizePlannedBatches(ctx context.Context, tenantID, versionID string, cfg SweepConfig) error {
	needsTask := s.tasks != nil && cfg.needsTask()
	needsStock := s.reserver != nil && cfg.needsStock()
	if !needsTask && !needsStock {
		return nil
	}
	var after *domain.PlannedBatchFinalizationCursor
	for pages := 0; pages < maxPlannedFinalizationPagesPerSweep; pages++ {
		batches, err := s.repo.ListPlannedBatchesNeedingFinalization(ctx, tenantID, versionID, needsTask, needsStock, after, s.page)
		if err != nil {
			return err
		}
		if len(batches) == 0 {
			return nil
		}
		if err := s.finalizePlannedBatchTasks(ctx, tenantID, batches, cfg); err != nil {
			return err
		}
		if err := s.finalizePlannedBatchStock(ctx, tenantID, batches, cfg); err != nil {
			return err
		}
		if int32(len(batches)) < s.page {
			return nil
		}
		last := batches[len(batches)-1]
		after = &domain.PlannedBatchFinalizationCursor{CreatedAt: last.CreatedAt, BatchID: last.BatchID}
	}
	return fmt.Errorf("obligation: planned batch finalization exceeded %d pages without draining", maxPlannedFinalizationPagesPerSweep)
}

// FinalizePlannedBatches finalizes BOTH SOP-task creation/linking AND stock reservation for every
// planned batch of a version that still needs them -- fresh batches created by this run's sweep AND
// any pre-existing/retry batch left over from an interrupted prior run alike. Callers that deferred
// finalization via SweepVersionWithSessionNoFinalize (R2-04 fix) MUST call this once per swept plan
// AFTER AlignComboDrives has run for the pass, so a combo batch's stock is reserved and its SOP task
// is created against its FINAL (aligned) planned_date rather than the pre-alignment date its own
// per-version sweep initially picked. Direct SweepVersion/SweepVersionWithSession callers do not
// need to call this: they already finalize fully inline.
func (s *SweeperService) FinalizePlannedBatches(ctx context.Context, tenantID, versionID string, cfg SweepConfig) error {
	return s.finalizePlannedBatches(ctx, tenantID, versionID, cfg)
}

func (s *SweeperService) finalizePlannedBatchTasks(ctx context.Context, tenantID string, batches []domain.PlannedBatchFinalization, cfg SweepConfig) error {
	if s.tasks == nil {
		return nil
	}
	requests := make([]BatchTaskCreate, 0, len(batches))
	for _, b := range batches {
		batchCfg := cfg.forRule(b.RuleID)
		if batchCfg.SOPVersionID == "" || b.HasSOPTask {
			continue
		}
		requests = append(requests, BatchTaskCreate{
			BatchID:      b.BatchID,
			SOPVersionID: batchCfg.SOPVersionID,
			TaskType:     "vaccination",
			Title:        "Vaccination drive " + b.ScopeID,
			ScopeType:    b.ScopeType,
			ScopeID:      b.ScopeID,
		})
	}
	if len(requests) == 0 {
		return nil
	}
	if creator, ok := s.tasks.(BatchTaskCreator); ok {
		taskIDs, err := creator.CreateTasksForBatches(ctx, tenantID, requests)
		if err != nil {
			return err
		}
		return s.linkPlannedBatchTasks(ctx, tenantID, requests, taskIDs)
	}
	taskIDs := make(map[string]string, len(requests))
	for _, req := range requests {
		// scale-guard:ignore: fallback for non-production task adapters; production bridge implements page-level task creation.
		taskID, err := s.tasks.CreateTaskForBatch(ctx, tenantID, req.BatchID, req.SOPVersionID, req.TaskType, req.Title, req.ScopeType, req.ScopeID)
		if err != nil {
			return err
		}
		taskIDs[req.BatchID] = taskID
	}
	return s.linkPlannedBatchTasks(ctx, tenantID, requests, taskIDs)
}

func (s *SweeperService) linkPlannedBatchTasks(ctx context.Context, tenantID string, requests []BatchTaskCreate, taskIDs map[string]string) error {
	links := make(map[string]string, len(requests))
	for _, req := range requests {
		taskID := taskIDs[req.BatchID]
		if taskID == "" {
			return fmt.Errorf("obligation: task creator returned no task for batch %s", req.BatchID)
		}
		links[req.BatchID] = taskID
	}
	if linker, ok := s.repo.(plannedBatchTaskLinker); ok {
		return linker.SetBatchSOPTasks(ctx, tenantID, links)
	}
	for _, req := range requests {
		// scale-guard:ignore: fallback for non-production repos; production repo links tasks in one set-based update.
		if err := s.repo.SetBatchSOPTask(ctx, tenantID, req.BatchID, links[req.BatchID]); err != nil {
			return err
		}
	}
	return nil
}

func (s *SweeperService) finalizePlannedBatchStock(ctx context.Context, tenantID string, batches []domain.PlannedBatchFinalization, cfg SweepConfig) error {
	if s.reserver == nil {
		return nil
	}
	stockBatches := make([]domain.PlannedBatchFinalization, 0, len(batches))
	for _, b := range batches {
		if !b.HasStockReservation || b.StockBlocked {
			stockBatches = append(stockBatches, b)
		}
	}
	if len(stockBatches) == 0 {
		return nil
	}
	ruleCountsByBatch, err := s.ruleCountsForPlannedBatches(ctx, tenantID, stockBatches)
	if err != nil {
		return err
	}
	requests := make([]BatchStockReservation, 0, len(stockBatches))
	blocked := make(map[string]bool)
	for _, b := range stockBatches {
		ruleCounts := ruleCountsByBatch[b.BatchID]
		if len(ruleCounts) == 0 && b.AttachedObligations > 0 {
			ruleCounts = []domain.RuleAttachmentCount{{RuleID: b.RuleID, Count: b.AttachedObligations}}
		}
		for _, rc := range ruleCounts {
			batchCfg := cfg.forRule(rc.RuleID)
			if batchCfg.VaccineItemID == "" || rc.Count <= 0 {
				continue
			}
			if b.StockBlockItemID != "" && batchCfg.VaccineItemID != b.StockBlockItemID {
				continue
			}
			dosesPer := batchCfg.DosesPerGoat
			if dosesPer < 1 {
				dosesPer = 1
			}
			requests = append(requests, BatchStockReservation{
				BatchID:    b.BatchID,
				LocationID: b.ScopeID,
				ItemID:     batchCfg.VaccineItemID,
				Qty:        rc.Count * int64(dosesPer),
				ValidOn:    batchStockValidOn(b),
			})
		}
		blocked[b.BatchID] = b.StockBlocked
	}
	if len(requests) == 0 {
		return nil
	}

	var failures map[string]error
	if reserver, ok := s.reserver.(BatchStockReserver); ok {
		failures, err = reserver.ReserveForBatches(ctx, tenantID, requests)
		if err != nil {
			return err
		}
	} else {
		failures = make(map[string]error)
		for _, req := range requests {
			// scale-guard:ignore: fallback for non-production reservers; production inventory service implements page-level reservation.
			if err := s.reserver.ReserveForBatch(ctx, tenantID, req.BatchID, req.LocationID, req.ItemID, req.Qty, req.ValidOn); err != nil {
				failures[StockReservationKey(req)] = err
			}
		}
	}

	blocks := make([]BatchStockBlock, 0)
	reservedByBatch := make(map[string]bool)
	failedByBatch := make(map[string]bool)
	for _, req := range requests {
		if reserveErr := failures[StockReservationKey(req)]; reserveErr != nil {
			if !failedByBatch[req.BatchID] {
				blocks = append(blocks, BatchStockBlock{BatchID: req.BatchID, ItemID: req.ItemID, RequiredQty: req.Qty, Reason: reserveErr.Error()})
			}
			failedByBatch[req.BatchID] = true
			continue
		}
		reservedByBatch[req.BatchID] = true
	}
	if err := s.markBatchStockBlocks(ctx, tenantID, blocks); err != nil {
		return err
	}
	clearIDs := make([]string, 0)
	for batchID := range reservedByBatch {
		if blocked[batchID] && !failedByBatch[batchID] {
			clearIDs = append(clearIDs, batchID)
		}
	}
	return s.clearBatchStockBlocks(ctx, tenantID, clearIDs)
}

func (s *SweeperService) ruleCountsForPlannedBatches(ctx context.Context, tenantID string, batches []domain.PlannedBatchFinalization) (map[string][]domain.RuleAttachmentCount, error) {
	out := make(map[string][]domain.RuleAttachmentCount, len(batches))
	if len(batches) == 0 {
		return out, nil
	}
	batchIDs := make([]string, 0, len(batches))
	seen := make(map[string]bool)
	for _, b := range batches {
		if seen[b.BatchID] {
			continue
		}
		seen[b.BatchID] = true
		batchIDs = append(batchIDs, b.BatchID)
		out[b.BatchID] = nil
	}
	if counter, ok := s.repo.(plannedBatchRuleCounter); ok {
		return counter.CountAttachedObligationsByRuleForBatches(ctx, tenantID, batchIDs)
	}
	for _, batchID := range batchIDs {
		// scale-guard:ignore: fallback for non-production repos; production repo counts all page batches in one grouped query.
		counts, err := s.repo.CountAttachedObligationsByRule(ctx, tenantID, batchID)
		if err != nil {
			return nil, err
		}
		out[batchID] = counts
	}
	return out, nil
}

func (s *SweeperService) markBatchStockBlocks(ctx context.Context, tenantID string, blocks []BatchStockBlock) error {
	if len(blocks) == 0 {
		return nil
	}
	if writer, ok := s.repo.(plannedBatchStockStateWriter); ok {
		return writer.MarkBatchStockBlocks(ctx, tenantID, blocks)
	}
	for _, block := range blocks {
		// scale-guard:ignore: fallback for non-production repos; production repo marks stock blocks in one set-based update.
		if err := s.repo.MarkBatchStockBlocked(ctx, tenantID, block.BatchID, block.ItemID, block.RequiredQty, block.Reason); err != nil {
			return err
		}
	}
	return nil
}

func (s *SweeperService) clearBatchStockBlocks(ctx context.Context, tenantID string, batchIDs []string) error {
	if len(batchIDs) == 0 {
		return nil
	}
	if writer, ok := s.repo.(plannedBatchStockStateWriter); ok {
		return writer.ClearBatchStockBlocks(ctx, tenantID, batchIDs)
	}
	for _, batchID := range batchIDs {
		// scale-guard:ignore: fallback for non-production repos; production repo clears stock blocks in one set-based update.
		if err := s.repo.ClearBatchStockBlock(ctx, tenantID, batchID); err != nil {
			return err
		}
	}
	return nil
}

func batchPlannedDate(dueAt time.Time) *time.Time {
	if dueAt.IsZero() {
		return nil
	}
	planned := biztime.BusinessDayStart(dueAt)
	return &planned
}

func batchStockValidOn(b domain.PlannedBatchFinalization) time.Time {
	if b.PlannedDate != nil && !b.PlannedDate.IsZero() {
		return *b.PlannedDate
	}
	return biztime.BusinessDayStart(time.Now())
}

func timeKey(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// MarkMissed materializes the terminal missed state for obligations whose due window/deadline has
// already crossed. This keeps the canonical obligation table aligned with Calendar/Action Center
// late-state projections.
func (s *SweeperService) MarkMissed(ctx context.Context, tenantID string, missedBefore time.Time) (int, error) {
	total := 0
	for {
		n, err := s.repo.MarkMissedBefore(ctx, tenantID, missedBefore, s.page)
		if err != nil {
			return total, err
		}
		total += n
		if int32(n) < s.page {
			return total, nil
		}
	}
}

// ExtractRuleVaccineIdentity extracts vaccine identity (code, priority, compatibility group, stock item)
// from a rule's eligibility_json, which is populated during matrix publish from the matrix row.
// BUG #1 fix: thread per-vaccine identity through sweeper instead of using version-level wrapper.
func ExtractRuleVaccineIdentity(eligibilityJSON []byte) RuleVaccineIdentity {
	if len(eligibilityJSON) == 0 {
		return RuleVaccineIdentity{}
	}
	var payload struct {
		Vaccine json.RawMessage `json:"vaccine"`
	}
	if err := json.Unmarshal(eligibilityJSON, &payload); err != nil {
		return RuleVaccineIdentity{}
	}
	if len(payload.Vaccine) == 0 {
		return RuleVaccineIdentity{}
	}
	var vaccine struct {
		Code             string `json:"code"`
		CompatibilityGrp string `json:"compatibility_group"`
		InventoryItemID  string `json:"inventory_item_id"`
	}
	if err := json.Unmarshal(payload.Vaccine, &vaccine); err != nil {
		return RuleVaccineIdentity{}
	}
	vaccineCode := strings.TrimSpace(vaccine.Code)
	return RuleVaccineIdentity{
		VaccineCode:      vaccineCode,
		VaccinePriority:  VaccineMatrixPriority(vaccineCode),
		CompatibilityGrp: strings.TrimSpace(vaccine.CompatibilityGrp),
		VaccineItemID:    strings.TrimSpace(vaccine.InventoryItemID),
	}
}

// getRuleVaccineIdentity returns the vaccine identity for a rule, either from the cache
// (RuleVaccineIDs) or by falling back to the version-level identity. Handles non-matrix rules
// gracefully.
//
// R2-05(a) fix: RuleVaccineIDs must only ever hold COMPLETE identities (see buildSweepConfig's
// caching contract) -- a cached-but-EMPTY entry (VaccineCode == "", e.g. a rule whose
// eligibility_json carried no vaccine, or a matrix extraction failure) is treated as a cache MISS
// here too, so a legacy non-matrix rule always falls through to the version-level identity
// instead of comparing/priority-ranking as a blank-code, zero-priority vaccine that silently
// defeats cap/tie detection for every other vaccine it happens to compete with.
func (cfg SweepConfig) getRuleVaccineIdentity(ruleID string) RuleVaccineIdentity {
	if id, ok := cfg.RuleVaccineIDs[ruleID]; ok && id.VaccineCode != "" {
		return id
	}
	// Fallback: return the version-level identity (for non-matrix rules, missing cache entries,
	// or a cached-empty entry). In a properly initialized SweepConfig, RuleVaccineIDs holds a
	// complete identity for every matrix rule, so this fallback is only for legacy non-matrix
	// rules or initialization gaps.
	return RuleVaccineIdentity{
		VaccineCode:     cfg.VaccineCode,
		VaccinePriority: normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode).VaccinePriority,
	}
}
