package app

import (
	"context"
	"strconv"
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

// StockReserver reserves doses for a batch (FEFO). Satisfied by the inventory app service; nil
// disables reserve. Best-effort: no stock is a no-op.
type StockReserver interface {
	ReserveForBatch(ctx context.Context, tenantID, batchID, locationID, itemID string, qty int64, validOn time.Time) error
}

type batchingHoldRecorder interface {
	RecordBatchingHoldForObligations(ctx context.Context, tenantID string, obligationIDs []string, holdUntil time.Time, occurredAt time.Time) (int64, error)
}

type clinicalSweepDeferrer interface {
	DeferBlockedVaccinationSweepCandidates(ctx context.Context, tenantID, versionID string, dueBefore, occurredAt time.Time, limit int32) (int, error)
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

// NewSweeperService constructs the sweeper. tasks/reserver may be nil to skip spawn/reserve.
func NewSweeperService(repo ports.Repository, tasks TaskCreator, reserver StockReserver) *SweeperService {
	return &SweeperService{repo: repo, tasks: tasks, reserver: reserver, page: 1000}
}

// SweepVersion batches all currently-unbatched due obligations for a version (due_at <= dueBefore).
func (s *SweeperService) SweepVersion(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time) (domain.SweepResult, error) {
	var res domain.SweepResult
	if err := s.finalizePlannedBatches(ctx, tenantID, versionID, cfg); err != nil {
		return res, err
	}
	if err := s.deferBlockedSweepCandidates(ctx, tenantID, versionID, dueBefore); err != nil {
		return res, err
	}
	touchedScopes := make(map[string]bool)
	planner := normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode)
	visitShotCounts := make(map[string]int32)
	for {
		rows, err := s.repo.ListUnbatchedDueForVersion(ctx, tenantID, versionID, dueBefore, s.page)
		if err != nil {
			return res, err
		}
		if len(rows) == 0 {
			break
		}

		type group struct {
			scopeType   string
			scopeID     string
			ruleID      string
			windowStart *time.Time
			windowEnd   *time.Time
			rows        []domain.UnbatchedDue
			ids         []string
		}
		order := make([]string, 0)
		groups := make(map[string]*group)
		for _, r := range rows {
			k := sweepWindowGroupKey(r, planner.SpeciesGroupingPolicy)
			g := groups[k]
			if g == nil {
				g = &group{scopeType: r.ScopeType, scopeID: r.ScopeID, ruleID: r.RuleID, windowStart: r.WindowStart, windowEnd: r.WindowEnd}
				groups[k] = g
				order = append(order, k)
			}
			g.rows = append(g.rows, r)
			g.ids = append(g.ids, r.ObligationID)
		}

		var progressed int64
		for _, k := range order {
			g := groups[k]
			if deferShedGroupToPark(cfg, g.scopeType, len(g.ids)) {
				continue
			}
			plannedDate := batchPlannedDate(g.rows[0].DueAt)
			if planner.Enabled {
				if picked := pickBestDriveDateWithHold(dueBefore, driveCandidatesFromUnbatched(g.rows), planner); picked != nil {
					plannedDate = picked
				}
			}
			selectedIDs := selectIDsWithinVisitShotCap(g.rows, plannedDate, planner.MaxShotsPerAnimalPerDrive, visitShotCounts)
			if len(selectedIDs) == 0 && plannedDate != nil && planner.MaxShotsPerAnimalPerDrive > 0 {
				if overflowDate := nextFeasibleUnbatchedDriveDateAfter(*plannedDate, g.rows); overflowDate != nil {
					plannedDate = overflowDate
					selectedIDs = selectIDsWithinVisitShotCap(g.rows, plannedDate, planner.MaxShotsPerAnimalPerDrive, visitShotCounts)
				}
			}
			idChunks := splitObligationIDs(selectedIDs, planner.MaxGoatsPerDrive)
			for _, chunk := range idChunks {
				if len(chunk) == 0 {
					continue
				}
				_, n, err := s.repo.CreateBatchWithObligations(ctx, domain.NewBatch{
					TenantID:          tenantID,
					ProtocolVersionID: versionID,
					ScopeType:         g.scopeType,
					ScopeID:           g.scopeID,
					Session:           batchSession(g.ruleID, cfg.VaccineCode),
					PlannedDate:       plannedDate,
					WindowStart:       g.windowStart,
					WindowEnd:         g.windowEnd,
					Status:            "planned",
					EstimatedTargets:  int32(len(chunk)),
					PlannedQuantity:   strconv.FormatInt(int64(len(chunk))*int64(normalizedDosesPerGoat(cfg.forRule(g.ruleID).DosesPerGoat)), 10),
					QuantityUnit:      "dose",
				}, chunk)
				if err != nil {
					return res, err
				}
				if n == 0 {
					continue
				}
				if !touchedScopes[k] {
					res.Batches++
					touchedScopes[k] = true
				}
				if err := s.recordBatchingHoldIfNeeded(ctx, tenantID, chunk, selectedUnbatchedRows(g.rows, chunk), plannedDate, dueBefore); err != nil {
					return res, err
				}
				res.Obligations += int(n)
				progressed += n
			}
		}
		if progressed == 0 || int32(len(rows)) < s.page {
			break
		}
	}
	parkRes, err := s.consolidateParkDrivesWithVisitCounts(ctx, tenantID, versionID, cfg, dueBefore, planner, visitShotCounts)
	if err != nil {
		return res, err
	}
	res.ParkBatches = parkRes.ParkBatches
	res.ParkObligations = parkRes.ParkObligations
	res.Batches += parkRes.ParkBatches
	res.Obligations += parkRes.ParkObligations

	fallbackRes, err := s.batchRemainingShedObligationsWithVisitCounts(ctx, tenantID, versionID, cfg, dueBefore, planner, visitShotCounts)
	if err != nil {
		return res, err
	}
	res.Batches += fallbackRes.Batches
	res.Obligations += fallbackRes.Obligations

	if err := s.finalizePlannedBatches(ctx, tenantID, versionID, cfg); err != nil {
		return res, err
	}
	return res, nil
}

func (s *SweeperService) deferBlockedSweepCandidates(ctx context.Context, tenantID, versionID string, dueBefore time.Time) error {
	deferrer, ok := s.repo.(clinicalSweepDeferrer)
	if !ok {
		return nil
	}
	for {
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
	return s.batchRemainingShedObligationsWithVisitCounts(ctx, tenantID, versionID, cfg, dueBefore, planner, make(map[string]int32))
}

func (s *SweeperService) batchRemainingShedObligationsWithVisitCounts(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time, planner domain.DrivePlannerSettings, visitShotCounts map[string]int32) (domain.SweepResult, error) {
	var res domain.SweepResult
	if !cfg.ParkConsolidation.Enabled {
		return res, nil
	}
	touchedScopes := make(map[string]bool)
	for {
		rows, err := s.repo.ListUnbatchedDueForVersion(ctx, tenantID, versionID, dueBefore, s.page)
		if err != nil {
			return res, err
		}
		if len(rows) == 0 {
			break
		}
		type group struct {
			scopeType   string
			scopeID     string
			ruleID      string
			windowStart *time.Time
			windowEnd   *time.Time
			rows        []domain.UnbatchedDue
			ids         []string
		}
		order := make([]string, 0)
		groups := make(map[string]*group)
		for _, r := range rows {
			if r.ScopeType != "shed" {
				continue
			}
			k := sweepWindowGroupKey(r, planner.SpeciesGroupingPolicy)
			g := groups[k]
			if g == nil {
				g = &group{scopeType: r.ScopeType, scopeID: r.ScopeID, ruleID: r.RuleID, windowStart: r.WindowStart, windowEnd: r.WindowEnd}
				groups[k] = g
				order = append(order, k)
			}
			g.rows = append(g.rows, r)
			g.ids = append(g.ids, r.ObligationID)
		}
		var progressed int64
		for _, k := range order {
			g := groups[k]
			plannedDate := batchPlannedDate(g.rows[0].DueAt)
			if planner.Enabled {
				if picked := pickBestDriveDateWithHold(dueBefore, driveCandidatesFromUnbatched(g.rows), planner); picked != nil {
					plannedDate = picked
				}
			}
			selectedIDs := selectIDsWithinVisitShotCap(g.rows, plannedDate, planner.MaxShotsPerAnimalPerDrive, visitShotCounts)
			if len(selectedIDs) == 0 && plannedDate != nil && planner.MaxShotsPerAnimalPerDrive > 0 {
				if overflowDate := nextFeasibleUnbatchedDriveDateAfter(*plannedDate, g.rows); overflowDate != nil {
					plannedDate = overflowDate
					selectedIDs = selectIDsWithinVisitShotCap(g.rows, plannedDate, planner.MaxShotsPerAnimalPerDrive, visitShotCounts)
				}
			}
			idChunks := splitObligationIDs(selectedIDs, planner.MaxGoatsPerDrive)
			for _, chunk := range idChunks {
				if len(chunk) == 0 {
					continue
				}
				_, n, err := s.repo.CreateBatchWithObligations(ctx, domain.NewBatch{
					TenantID:          tenantID,
					ProtocolVersionID: versionID,
					ScopeType:         g.scopeType,
					ScopeID:           g.scopeID,
					Session:           batchSession(g.ruleID, cfg.VaccineCode),
					PlannedDate:       plannedDate,
					WindowStart:       g.windowStart,
					WindowEnd:         g.windowEnd,
					Status:            "planned",
					EstimatedTargets:  int32(len(chunk)),
					PlannedQuantity:   strconv.FormatInt(int64(len(chunk))*int64(normalizedDosesPerGoat(cfg.forRule(g.ruleID).DosesPerGoat)), 10),
					QuantityUnit:      "dose",
				}, chunk)
				if err != nil {
					return res, err
				}
				if n == 0 {
					continue
				}
				if !touchedScopes[k] {
					res.Batches++
					touchedScopes[k] = true
				}
				if err := s.recordBatchingHoldIfNeeded(ctx, tenantID, chunk, selectedUnbatchedRows(g.rows, chunk), plannedDate, dueBefore); err != nil {
					return res, err
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
	for {
		batches, err := s.repo.ListPlannedBatchesNeedingFinalization(ctx, tenantID, versionID, needsTask, needsStock, s.page)
		if err != nil {
			return err
		}
		if len(batches) == 0 {
			return nil
		}
		// progressed counts batches whose finalization state we actually advanced
		// this page. The "needing finalization" query is config-unaware, so a batch
		// whose rule lacks an SOP version (task path) or a vaccine item (stock path)
		// is returned but never mutated out. Without this guard, once such stuck
		// batches fill a page the `len < s.page` break never fires and the sweep
		// loops forever. Zero progress on a full page means the rest are stuck for
		// config reasons this sweep cannot resolve, so stop rather than hang.
		var progressed int
		for _, b := range batches {
			batchCfg := cfg.forRule(b.RuleID)
			if s.tasks != nil && batchCfg.SOPVersionID != "" && !b.HasSOPTask {
				taskID, err := s.tasks.CreateTaskForBatch(ctx, tenantID, b.BatchID, batchCfg.SOPVersionID, "vaccination", "Vaccination drive "+b.ScopeID, b.ScopeType, b.ScopeID)
				if err != nil {
					return err
				}
				if err := s.repo.SetBatchSOPTask(ctx, tenantID, b.BatchID, taskID); err != nil {
					return err
				}
				progressed++
			}
			if s.reserver != nil && !b.HasStockReservation {
				did, err := s.reservePlannedBatchStock(ctx, tenantID, b, cfg)
				if err != nil {
					return err
				}
				if did {
					progressed++
				}
			}
		}
		if progressed == 0 || int32(len(batches)) < s.page {
			return nil
		}
	}
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

// reservePlannedBatchStock returns whether it changed the batch's stock state
// (created a reserve movement, recorded a stock block, or cleared one). A false
// return means nothing mutated — the batch stays "needing finalization" and the
// caller must not count it as progress, else the sweep can loop forever.
func (s *SweeperService) reservePlannedBatchStock(ctx context.Context, tenantID string, b domain.PlannedBatchFinalization, cfg SweepConfig) (bool, error) {
	ruleCounts, err := s.repo.CountAttachedObligationsByRule(ctx, tenantID, b.BatchID)
	if err != nil {
		return false, err
	}
	if len(ruleCounts) == 0 && b.AttachedObligations > 0 {
		ruleCounts = []domain.RuleAttachmentCount{{RuleID: b.RuleID, Count: b.AttachedObligations}}
	}
	if len(ruleCounts) == 0 {
		return false, nil
	}
	validOn := batchStockValidOn(b)
	reservedAny := false
	for _, rc := range ruleCounts {
		batchCfg := cfg.forRule(rc.RuleID)
		if batchCfg.VaccineItemID == "" || rc.Count <= 0 {
			continue
		}
		dosesPer := batchCfg.DosesPerGoat
		if dosesPer < 1 {
			dosesPer = 1
		}
		qty := rc.Count * int64(dosesPer)
		if err := s.reserver.ReserveForBatch(ctx, tenantID, b.BatchID, b.ScopeID, batchCfg.VaccineItemID, qty, validOn); err != nil {
			if markErr := s.repo.MarkBatchStockBlocked(ctx, tenantID, b.BatchID, batchCfg.VaccineItemID, qty, err.Error()); markErr != nil {
				return false, markErr
			}
			// A recorded stock block moves the retry window forward, so the batch
			// drops out of the finalization query until it is due again: progress.
			return true, nil
		}
		reservedAny = true
	}
	if reservedAny && b.StockBlocked {
		return true, s.repo.ClearBatchStockBlock(ctx, tenantID, b.BatchID)
	}
	return reservedAny, nil
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
