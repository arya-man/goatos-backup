package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	vaccexecdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
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

type batchCreatorReturningAttachedIDs interface {
	CreateBatchWithObligationsReturningAttachedIDs(ctx context.Context, in domain.NewBatch, obligationIDs []string) (batchID string, attachedIDs []string, err error)
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
	VaccineType      string
	PathogenClass    string
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

// batchCellsCreator is the exact-cell-accounting create path (VAXCAP-003): the repo recomputes
// the batch's planned_quantity from the ACTUALLY-attached rows using each obligation's own cell
// count, on both the create and merge-into-existing-batch paths.
type batchCellsCreator interface {
	CreateBatchWithObligationCells(ctx context.Context, in domain.NewBatch, obligationIDs []string, cellsByObligation map[string]int32) (batchID string, attachedIDs []string, err error)
}

type vaccinationOperatorLister interface {
	AvailableVaccinationOperatorsForDrive(ctx context.Context, tenantID, parkID string, plannedDate time.Time, capPerOperator int32) ([]domain.DriveOperatorCapacity, error)
}

type vaccinationDriveAssignmentWriter interface {
	UpsertVaccinationDriveAssignments(ctx context.Context, tenantID string, assignments []domain.DriveAssignment) error
}

// vaccinationDriveAssignmentReplacer replaces the ENTIRE drive-assignment row set for one
// (tenant, batch) atomically: delete existing rows for that batch, then insert exactly the
// supplied set. Used after a batch attach/merge so a row for an obligation that was SELECTED
// but did NOT actually attach (different shed/partition/operator key than the attached set)
// can never survive as a stale row -- an upsert alone only ever adds/updates keys present in
// the new set and can never remove a key that has disappeared from it.
type vaccinationDriveAssignmentReplacer interface {
	ReplaceVaccinationDriveAssignmentsForBatch(ctx context.Context, tenantID, batchID string, assignments []domain.DriveAssignment) error
}

type vaccinationDriveDateOverrideReader interface {
	ActiveVaccinationDriveDateOverride(ctx context.Context, tenantID, parkID, vaccineCode string, originalDate time.Time) (*domain.VaccineDriveDateOverride, error)
}

func (s *SweeperService) overriddenDriveDate(ctx context.Context, tenantID, parkID string, plannedDate *time.Time, vaccineCode string) (*time.Time, error) {
	if plannedDate == nil || strings.TrimSpace(parkID) == "" || strings.TrimSpace(vaccineCode) == "" {
		return plannedDate, nil
	}
	reader, ok := s.repo.(vaccinationDriveDateOverrideReader)
	if !ok {
		return plannedDate, nil
	}
	override, err := reader.ActiveVaccinationDriveDateOverride(ctx, tenantID, parkID, vaccineCode, *plannedDate)
	if err != nil {
		return nil, err
	}
	if override == nil || override.OverrideDate.IsZero() {
		return plannedDate, nil
	}
	day := businessDate(override.OverrideDate)
	return &day, nil
}

func (s *SweeperService) createBatchWithAttachedIDs(ctx context.Context, in domain.NewBatch, obligationIDs []string, cellsByObligation map[string]int32) (string, []string, error) {
	if creator, ok := s.repo.(batchCellsCreator); ok && cellsByObligation != nil {
		return creator.CreateBatchWithObligationCells(ctx, in, obligationIDs, cellsByObligation)
	}
	if creator, ok := s.repo.(batchCreatorReturningAttachedIDs); ok {
		return creator.CreateBatchWithObligationsReturningAttachedIDs(ctx, in, obligationIDs)
	}
	batchID, attached, err := s.repo.CreateBatchWithObligations(ctx, in, obligationIDs)
	if err != nil || attached <= 0 {
		return batchID, nil, err
	}
	if int(attached) >= len(obligationIDs) {
		return batchID, append([]string(nil), obligationIDs...), nil
	}
	return batchID, nil, fmt.Errorf("obligation: repository attached %d/%d rows but did not return exact attached IDs; refusing to guess sweep claims", attached, len(obligationIDs))
}

func (s *SweeperService) availableVaccinationOperatorsForDrive(ctx context.Context, tenantID, parkID string, plannedDate time.Time, capPerOperator int32, session *SweepSession) ([]domain.DriveOperatorCapacity, error) {
	if strings.TrimSpace(parkID) == "" {
		return nil, nil
	}
	if cached, ok := session.cachedVaccinationOperators(tenantID, parkID, plannedDate, capPerOperator); ok {
		return cached, nil
	}
	lister, ok := s.repo.(vaccinationOperatorLister)
	if !ok {
		session.rememberVaccinationOperators(tenantID, parkID, plannedDate, capPerOperator, nil)
		return nil, nil
	}
	operators, err := lister.AvailableVaccinationOperatorsForDrive(ctx, tenantID, parkID, plannedDate, capPerOperator)
	if err != nil {
		return nil, err
	}
	session.rememberVaccinationOperators(tenantID, parkID, plannedDate, capPerOperator, operators)
	return cloneDriveOperatorCapacities(operators), nil
}

func (s *SweeperService) assignVaccinationOperator(ctx context.Context, in *domain.NewBatch, capPerOperator int32, session *SweepSession) error {
	if in == nil || in.PlannedDate == nil || in.ConductedBy != nil || strings.TrimSpace(in.ScopeType) != "park" || strings.TrimSpace(in.ScopeID) == "" {
		return nil
	}
	operators, err := s.availableVaccinationOperatorsForDrive(ctx, in.TenantID, in.ScopeID, *in.PlannedDate, capPerOperator, session)
	if err != nil {
		return err
	}
	if len(operators) > 0 && strings.TrimSpace(operators[0].OperatorID) != "" {
		operatorID := strings.TrimSpace(operators[0].OperatorID)
		in.ConductedBy = &operatorID
	}
	return nil
}

func (s *SweeperService) writeVaccinationDriveAssignments(ctx context.Context, tenantID string, assignments []domain.DriveAssignment) error {
	if len(assignments) == 0 {
		return nil
	}
	writer, ok := s.repo.(vaccinationDriveAssignmentWriter)
	if !ok {
		return nil
	}
	return writer.UpsertVaccinationDriveAssignments(ctx, tenantID, assignments)
}

// replaceVaccinationDriveAssignmentsForBatch persists the FINAL, attached-only assignment set for
// one batch, replacing anything previously written for it. This is the post-attach write: it must
// run AFTER attachedIDs is known and must never be seeded with the pre-attach (all-selected-rows)
// assignment set (see batchDueGroup), or a row for a non-attached obligation would already be
// persisted by the create path and this replace would just re-affirm it as "current".
func (s *SweeperService) replaceVaccinationDriveAssignmentsForBatch(ctx context.Context, tenantID, batchID string, assignments []domain.DriveAssignment) error {
	if strings.TrimSpace(batchID) == "" {
		return nil
	}
	if replacer, ok := s.repo.(vaccinationDriveAssignmentReplacer); ok {
		return replacer.ReplaceVaccinationDriveAssignmentsForBatch(ctx, tenantID, batchID, assignments)
	}
	// Fallback for repos that only implement the legacy upsert-only writer (e.g. test fakes):
	// upsert is not a full replace (it cannot remove a stale key that disappeared from the new
	// set), but it is still strictly better than doing nothing.
	return s.writeVaccinationDriveAssignments(ctx, tenantID, assignments)
}

func (s *SweeperService) distributeVaccinationDriveAssignments(ctx context.Context, tenantID string, batch domain.NewBatch, capPerOperator int32, assignments []domain.DriveAssignment, session *SweepSession) ([]domain.DriveAssignment, error) {
	if len(assignments) == 0 || batch.PlannedDate == nil || strings.TrimSpace(batch.ScopeID) == "" {
		return assignments, nil
	}
	operators, err := s.availableVaccinationOperatorsForDrive(ctx, tenantID, batch.ScopeID, *batch.PlannedDate, capPerOperator, session)
	if err != nil {
		return nil, err
	}
	if len(operators) == 0 && batch.ConductedBy != nil && strings.TrimSpace(*batch.ConductedBy) != "" {
		operators = append(operators, domain.DriveOperatorCapacity{OperatorID: strings.TrimSpace(*batch.ConductedBy), Cap: capPerOperator})
	}
	if len(operators) == 0 {
		return assignments, nil
	}
	planned, err := planVaccinationDriveAssignments(tenantID, batch.ScopeID, *batch.PlannedDate, capPerOperator, operators, assignments, session)
	if err != nil {
		return nil, err
	}
	rememberVaccinationDriveAssignmentLoads(tenantID, batch.ScopeID, *batch.PlannedDate, planned, session)
	return planned, nil
}

func (s *SweeperService) operatorCapacityPlanner(ctx context.Context, tenantID, parkID string, date *time.Time, planner domain.DrivePlannerSettings, session *SweepSession) (domain.DrivePlannerSettings, error) {
	if planner.MaxGoatsPerDrive <= 0 || date == nil || strings.TrimSpace(parkID) == "" {
		return planner, nil
	}
	operators, err := s.availableVaccinationOperatorsForDrive(ctx, tenantID, parkID, *date, planner.MaxGoatsPerDrive, session)
	if err != nil {
		if errors.Is(err, domain.ErrOperatorAssignmentConfigPresentButEmpty) {
			// Config present but no executable operator: fail closed. Scale to 0 so
			// driveOperatorCapacityExhausted fires and the sweeper defers this day,
			// instead of planning at base cap. (This runs before
			// limitUnbatchedSelectionByDriveAnimals, so the F1 "cap<=0 == unbounded"
			// path is never reached.)
			scaled := planner
			scaled.MaxGoatsPerDrive = 0
			return scaled, nil
		}
		return planner, err
	}
	if len(operators) == 0 {
		return planner, nil
	}
	scaled := planner
	scaled.MaxGoatsPerDrive = totalVaccinationOperatorCap(tenantID, parkID, *date, operators, planner.MaxGoatsPerDrive, session)
	return scaled, nil
}

func (s *SweeperService) effectiveOperatorAnimalCap(ctx context.Context, tenantID, parkID string, date *time.Time, capPerOperator int32, session *SweepSession) (int32, error) {
	if capPerOperator <= 0 || date == nil || strings.TrimSpace(parkID) == "" {
		return capPerOperator, nil
	}
	operators, err := s.availableVaccinationOperatorsForDrive(ctx, tenantID, parkID, *date, capPerOperator, session)
	if err != nil {
		if errors.Is(err, domain.ErrOperatorAssignmentConfigPresentButEmpty) {
			// Config present but no executable operator: fail closed with zero usable cap.
			return 0, nil
		}
		return capPerOperator, err
	}
	if len(operators) == 0 {
		return capPerOperator, nil
	}
	return totalVaccinationOperatorCap(tenantID, parkID, *date, operators, capPerOperator, session), nil
}

// totalVaccinationOperatorCap sums each operator's REMAINING usable capacity for (tenantID,
// parkID, plannedDate): the DB-queried Cap (already net of persisted/committed load from prior
// sweeper runs) minus whatever THIS sweep session has already reserved for that operator on that
// exact date via rememberVaccinationOperatorLoad. Without the session subtraction, a due-group
// processed later in the same sweep (a different protocol version/rule -- e.g. sheep_pox after
// blue_tongue) re-reads the SAME cached pre-session DB snapshot (see
// SweepSession.cachedVaccinationOperators) and sees the operator's full un-reserved capacity
// again, letting it select up to that amount on top of what an earlier due-group in this same
// sweep already committed -- overshooting the true per-operator/day cap. This mirrors the
// subtraction planVaccinationDriveAssignments already does at the assignment-split layer; this is
// the same fix at the earlier SELECTION-limiting layer (operatorCapacityPlanner /
// effectiveOperatorAnimalCap), which is what actually bounds how many obligations a due-group may
// attach to a batch. See TestOperatorCapacityPlannerHonorsCrossVersionOperatorDayLoad.
func totalVaccinationOperatorCap(tenantID, parkID string, plannedDate time.Time, operators []domain.DriveOperatorCapacity, fallbackCap int32, session *SweepSession) int32 {
	// fallbackCap only applies when NO operators were found at all (callers already
	// early-return on len(operators) == 0, but keep this defensive for direct callers).
	// When operators WERE found but every one has 0 remaining capacity, the correct
	// answer is 0 -- NOT the fallback batch cap. Falling back here would let the
	// planner create/lock a drive onto operators who are all already at capacity.
	if len(operators) == 0 {
		return fallbackCap
	}
	var total int32
	for _, operator := range operators {
		operatorID := strings.TrimSpace(operator.OperatorID)
		// Cap is remaining usable capacity (after persisted load from a PRIOR sweep run). Subtract
		// what this sweep session has already reserved for this operator/date so a later
		// due-group/version in the same sweep sees the true remaining room, not the stale
		// pre-session snapshot.
		remaining := operator.Cap
		if operatorID != "" {
			remaining -= session.vaccinationOperatorLoad(tenantID, parkID, plannedDate, operatorID)
		}
		if remaining > 0 {
			total += remaining
		}
	}
	return total
}

// driveOperatorCapacityExhausted reports whether capPlanner is the F1-fixed "operators were found
// but every one has 0 remaining capacity" result, as opposed to a genuinely uncapped/unconfigured
// drive planner. Both cases leave MaxGoatsPerDrive == 0, which limitUnbatchedSelectionByDriveAnimals
// (and its many other callers/tests) intentionally treat as "no cap configured -- do not limit".
// operatorCapacityPlanner only ever returns MaxGoatsPerDrive == 0 for an ORIGINALLY-capped planner
// (original.MaxGoatsPerDrive > 0) when totalVaccinationOperatorCap found operators and they are all
// exhausted (see totalVaccinationOperatorCap); an originally-uncapped planner is returned unchanged
// by that function's own top guard and never becomes exactly 0 through this path. This lets every
// call site short-circuit to "admit nothing" instead of falling into the ambiguous-zero unlimited
// branch, closing F1 on the real production caller instead of only inside the isolated function.
func driveOperatorCapacityExhausted(original, scaled domain.DrivePlannerSettings) bool {
	return original.MaxGoatsPerDrive > 0 && scaled.MaxGoatsPerDrive <= 0
}

func leastLoadedDriveOperator(operators []domain.DriveOperatorCapacity, loads map[string]int32) string {
	chosen := operators[0].OperatorID
	for _, operator := range operators[1:] {
		operatorID := strings.TrimSpace(operator.OperatorID)
		if operatorID != "" && loads[operatorID] < loads[chosen] {
			chosen = operatorID
		}
	}
	return chosen
}

func planVaccinationDriveAssignments(tenantID, parkID string, plannedDate time.Time, capPerOperator int32, operators []domain.DriveOperatorCapacity, assignments []domain.DriveAssignment, session *SweepSession) ([]domain.DriveAssignment, error) {
	if len(assignments) == 0 || len(operators) == 0 {
		return assignments, nil
	}
	blocks := make([]vaccexecapp.DriveWorkBlock, 0, len(assignments))
	byID := make(map[string]domain.DriveAssignment, len(assignments))
	for i, assignment := range assignments {
		id := strconv.Itoa(i)
		physicalShed := strings.TrimSpace(assignment.PhysicalShed)
		if physicalShed == "" {
			physicalShed = "park"
		}
		partition := strings.TrimSpace(assignment.PartitionLabel)
		if partition == "" {
			partition = "whole"
		}
		byID[id] = assignment
		blocks = append(blocks, vaccexecapp.DriveWorkBlock{
			ID:             id,
			Park:           strings.TrimSpace(assignment.ParkID),
			PhysicalShed:   physicalShed,
			Partition:      partition,
			Animals:        int(assignment.AnimalCount),
			DueDate:        plannedDate,
			LatestSafeDate: plannedDate,
		})
	}
	ops := make([]vaccexecapp.DriveOperator, 0, len(operators))
	for _, operator := range operators {
		operatorID := strings.TrimSpace(operator.OperatorID)
		if operatorID == "" {
			continue
		}
		// Cap is now remaining usable capacity (after persisted load from AvailableVaccinationOperatorsForDrive).
		// Skip operators with no remaining capacity from database; don't fall back to capPerOperator.
		capacity := int(operator.Cap)
		if capacity <= 0 {
			continue
		}
		remaining := capacity
		remaining -= int(session.vaccinationOperatorLoad(tenantID, parkID, plannedDate, operatorID))
		if remaining <= 0 {
			continue
		}
		ops = append(ops, vaccexecapp.DriveOperator{
			ID:        operatorID,
			Name:      operatorID,
			Cap:       remaining,
			Available: true,
		})
	}
	if len(ops) == 0 {
		return assignments, nil
	}
	plan, err := (vaccexecapp.OperatorDrivePlanner{}).Plan(vaccexecapp.DrivePlanRequest{
		StartDate: plannedDate,
		Availability: []vaccexecapp.DriveDateAvailability{{
			Date:      plannedDate,
			Operators: ops,
		}},
		WorkBlocks: blocks,
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.DriveAssignment, 0, len(assignments)+len(blocks))
	for _, day := range plan.Days {
		for _, planned := range day.Assignments {
			operatorID := strings.TrimSpace(planned.OperatorID)
			uniqueBlockIDs := uniqueStrings(planned.BlockIDs)
			for _, blockID := range uniqueBlockIDs {
				base, ok := byID[blockID]
				if !ok {
					continue
				}
				next := base
				next.PlannedDate = plannedDate
				if operatorID != "" {
					next.OperatorID = &operatorID
				}
				if len(uniqueBlockIDs) == 1 && planned.Animals > 0 && int32(planned.Animals) < next.AnimalCount {
					next.TotalDoses = proportionalDoseCount(next.TotalDoses, next.AnimalCount, int32(planned.Animals))
					next.AnimalCount = int32(planned.Animals)
				}
				if hasDrivePlanWarning(planned.Warnings, "over_cap_required_latest_safe") && next.CapacityStatus == "within_cap" {
					next.CapacityStatus = "over_cap_required"
					next.Warnings = append(next.Warnings, "operator animal cap exceeded to keep latest safe vaccination window")
				}
				if hasDrivePlanWarning(planned.Warnings, "forced_partition_split") {
					next.Warnings = append(next.Warnings, "partition split because one partition exceeded available operator capacity")
				}
				out = append(out, next)
			}
		}
	}
	for _, block := range plan.Unassigned {
		if base, ok := byID[block.ID]; ok {
			base.AnimalCount = int32(block.Animals)
			out = append(out, base)
		}
	}
	if len(out) == 0 {
		return assignments, nil
	}
	return out, nil
}

func proportionalDoseCount(totalDoses int32, originalAnimals int32, plannedAnimals int32) int32 {
	if totalDoses <= 0 || originalAnimals <= 0 || plannedAnimals <= 0 {
		return 0
	}
	value := (int64(totalDoses)*int64(plannedAnimals) + int64(originalAnimals) - 1) / int64(originalAnimals)
	if value > int64(totalDoses) {
		value = int64(totalDoses)
	}
	return int32(value)
}

func rememberVaccinationDriveAssignmentLoads(tenantID, parkID string, plannedDate time.Time, assignments []domain.DriveAssignment, session *SweepSession) {
	if session == nil {
		return
	}
	for _, assignment := range assignments {
		if assignment.OperatorID == nil {
			continue
		}
		session.rememberVaccinationOperatorLoad(tenantID, parkID, plannedDate, *assignment.OperatorID, assignment.AnimalCount)
	}
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func hasDrivePlanWarning(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if warning == needle {
			return true
		}
	}
	return false
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
	out := make([]domain.UnbatchedDue, 0, len(rows))
	for _, row := range rows {
		if _, ok := allowed[row.ObligationID]; ok {
			out = append(out, row)
		}
	}
	return out
}

func filterParkConsolidationSnapshot(rows []domain.ParkConsolidationCandidate, candidateIDs []string) []domain.ParkConsolidationCandidate {
	allowed := snapshotIDSet(candidateIDs)
	out := make([]domain.ParkConsolidationCandidate, 0, len(rows))
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
	return s.SweepVersionAsOf(ctx, tenantID, versionID, cfg, time.Time{}, dueBefore)
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
	return s.SweepVersionWithSessionAsOf(ctx, tenantID, versionID, cfg, time.Time{}, dueBefore, session)
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
	return s.SweepVersionWithSessionNoFinalizeAsOf(ctx, tenantID, versionID, cfg, time.Time{}, dueBefore, session)
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
	return s.SweepVersionWithSessionNoFinalizeHWMAsOf(ctx, tenantID, versionID, cfg, time.Time{}, dueBefore, session, createdAtHWM)
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
	return s.SweepVersionWithSessionNoFinalizeSnapshotAsOf(ctx, tenantID, versionID, cfg, time.Time{}, dueBefore, session, createdAtHWM, snapshot)
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
		for {
			var snapshotRows []domain.UnbatchedDue
			for _, chunk := range snapshotIDChunks(candidateIDs, s.page) {
				rows, err := s.listUnbatchedDueForVersionBounded(ctx, tenantID, versionID, dueBefore, s.page, createdAtHWM, chunk)
				if err != nil {
					return res, err
				}
				snapshotRows = append(snapshotRows, rows...)
			}
			if len(snapshotRows) == 0 {
				break
			}

			var progressed int64
			if cfg.ParkConsolidation.Enabled {
				if err := rejectNonShedUnbatchedDue(snapshotRows); err != nil {
					return res, err
				}
				// Max-output rule: the park planner gets the whole preflight candidate set first.
				// If shed batches are created before this pass, tiny rows can no longer club into
				// a nearby larger drive because the larger drive has already left the candidate pool.
				parkRes, err := s.consolidateParkDrivesWithVisitCounts(ctx, tenantID, versionID, cfg, asOf, dueBefore, planner, session, createdAtHWM, candidateIDs)
				if err != nil {
					return res, err
				}
				res.ParkBatches += parkRes.ParkBatches
				res.ParkObligations += parkRes.ParkObligations
				res.Batches += parkRes.ParkBatches
				res.Obligations += parkRes.ParkObligations
				progressed += int64(parkRes.ParkObligations)

				fallbackRes, err := s.batchRemainingShedObligationsWithVisitCounts(ctx, tenantID, versionID, cfg, asOf, dueBefore, planner, session, createdAtHWM, candidateIDs)
				if err != nil {
					return res, err
				}
				res.Batches += fallbackRes.Batches
				res.Obligations += fallbackRes.Obligations
				progressed += int64(fallbackRes.Obligations)
			} else {
				order, groups := groupUnbatchedDue(snapshotRows, planner.SpeciesGroupingPolicy)
				order = orderDueGroupsByVaccinePriority(order, groups, cfg)
				for _, k := range order {
					g := groups[k]
					batched, n, err := s.batchDueGroup(ctx, tenantID, versionID, cfg, planner, asOf, dueBefore, session, g)
					if err != nil {
						return res, err
					}
					if !batched {
						continue
					}
					if !touchedScopes[k] {
						res.Batches++
						touchedScopes[k] = true
					}
					res.Obligations += int(n)
					progressed += n
				}
			}

			if progressed == 0 || progressed >= int64(len(snapshotRows)) {
				break
			}
		}
		if finalizeNow {
			if err := s.finalizePlannedBatches(ctx, tenantID, versionID, cfg); err != nil {
				return res, err
			}
		}
		return res, nil
	} else {
		if cfg.ParkConsolidation.Enabled {
			parkRes, err := s.consolidateParkDrivesWithVisitCounts(ctx, tenantID, versionID, cfg, asOf, dueBefore, planner, session, createdAtHWM, nil)
			if err != nil {
				return res, err
			}
			res.ParkBatches = parkRes.ParkBatches
			res.ParkObligations = parkRes.ParkObligations
			res.Batches += parkRes.ParkBatches
			res.Obligations += parkRes.ParkObligations

			fallbackRes, err := s.batchRemainingShedObligationsWithVisitCounts(ctx, tenantID, versionID, cfg, asOf, dueBefore, planner, session, createdAtHWM, nil)
			if err != nil {
				return res, err
			}
			res.Batches += fallbackRes.Batches
			res.Obligations += fallbackRes.Obligations

			if finalizeNow {
				if err := s.finalizePlannedBatches(ctx, tenantID, versionID, cfg); err != nil {
					return res, err
				}
			}
			return res, nil
		}
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
				batched, n, err := s.batchDueGroup(ctx, tenantID, versionID, cfg, planner, asOf, dueBefore, session, g)
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

// batchDueGroup plans a drive date and selects one dueGroup's obligations under the shared visit
// shot cap and park/date animal-slot cap, then creates the resulting park drive batch. It seeds
// session with persisted cross-pass claims before selecting (VAX-REV-01), and -- when the repo
// supports it -- holds advisory locks across the select+create sequence so a concurrent sweeper
// worker cannot commit a conflicting claim in between. If every row rejects on a candidate date, the
// group walks later feasible dates in the safe window, re-seeding and re-locking for each date.
func (s *SweeperService) batchDueGroup(ctx context.Context, tenantID, versionID string, cfg SweepConfig, planner domain.DrivePlannerSettings, asOf, dueBefore time.Time, session *SweepSession, g *dueGroup) (batched bool, obligations int64, err error) {
	operationalAsOf := dueGroupOperationalAsOf(asOf, dueBefore, g)
	plannedDate := batchPlannedDate(g.rows[0].DueAt)
	if planner.Enabled {
		if picked := pickBestDriveDateWithHold(operationalAsOf, driveCandidatesFromUnbatched(g.rows), planner); picked != nil {
			plannedDate = picked
		} else {
			return false, 0, fmt.Errorf("obligation: vaccination planner found no feasible date for eligible shed group %s/%s rule %s; generation/recovery must provide a due/ready anchor with a 7-day schedulable window", g.scopeType, g.scopeID, g.ruleID)
		}
	}
	targetIDs := distinctUnbatchedTargetIDs(g.rows)
	// BUG #1: use per-rule vaccine identity instead of version-level wrapper.
	ruleVaccineID := cfg.getRuleVaccineIdentity(g.ruleID)
	if overridden, overrideErr := s.overriddenDriveDate(ctx, tenantID, firstUnbatchedParkID(g.rows), plannedDate, ruleVaccineID.VaccineCode); overrideErr != nil {
		return false, 0, overrideErr
	} else {
		plannedDate = overridden
	}
	cellsPerObligation := normalizedDosesPerGoat(cfg.forRule(g.ruleID).DosesPerGoat)
	plannedDate, selectedIDs, shotClaims, release, err := s.selectBestUnbatchedDriveDateWithVisitCap(ctx, tenantID, operationalAsOf, g.rows, targetIDs, plannedDate, planner, ruleVaccineID, cellsPerObligation, session)
	if err != nil {
		return false, 0, err
	}
	if len(selectedIDs) == 0 {
		if err := release(ctx); err != nil {
			return false, 0, err
		}
		return false, 0, nil
	}
	capPlanner, err := s.operatorCapacityPlanner(ctx, tenantID, firstUnbatchedParkID(g.rows), plannedDate, planner, session)
	if err != nil {
		_ = release(ctx)
		return false, 0, err
	}
	if driveOperatorCapacityExhausted(planner, capPlanner) {
		// F1: operators were found but every one has 0 remaining capacity. Do NOT fall through to
		// limitUnbatchedSelectionByDriveAnimals, which treats MaxGoatsPerDrive == 0 as "no cap
		// configured" and would admit the full selection onto already-exhausted operators.
		session.releaseClaims(shotClaims)
		if err := release(ctx); err != nil {
			return false, 0, err
		}
		return false, 0, nil
	}
	cappedIDs := limitUnbatchedSelectionByDriveAnimals(operationalAsOf, g.rows, selectedIDs, plannedDate, capPlanner, session)
	if len(cappedIDs) < len(selectedIDs) {
		session.releaseClaims(claimsOutsideSelection(shotClaims, cappedIDs))
		shotClaims = claimsOutsideSelection(shotClaims, differenceIDs(selectedIDs, cappedIDs))
		selectedIDs = cappedIDs
		if len(selectedIDs) == 0 {
			if err := release(ctx); err != nil {
				return false, 0, err
			}
			return false, 0, nil
		}
	}
	defer func() {
		if relErr := release(ctx); relErr != nil && err == nil {
			err = relErr
		}
	}()

	// MaxGoatsPerDrive carries the published park/date animal-slot cap in the vaccination planner.
	// Selection above has already limited by animals and allowed only last-safe-day overflow; splitting
	// again by row count would turn one valid park drive into fake micro-drives.
	idChunks := [][]string{selectedIDs}
	claimChunks := splitShotCapReservations(shotClaims, selectedIDs, idChunks)
	for _, chunk := range idChunks {
		claimChunk := claimChunks[0]
		claimChunks = claimChunks[1:]
		if len(chunk) == 0 {
			continue
		}
		selectedRows := selectedUnbatchedRows(g.rows, chunk)
		batchScopeType, batchScopeID, scopeErr := vaccinationDriveBatchScope(g, selectedRows)
		if scopeErr != nil {
			session.releaseClaims(claimChunk)
			return batched, obligations, scopeErr
		}
		animalCount := uniqueUnbatchedTargetCount(selectedRows)
		windowStart, windowEnd := unbatchedDriveWindow(selectedRows)
		// VAXCAP-003: one rule per unbatched group, so every row carries the same per-rule cell
		// count -- but the persisted total must still come from the rows ACTUALLY attached.
		cellsPerRow := normalizedDosesPerGoat(cfg.forRule(g.ruleID).DosesPerGoat)
		cellsByObligation := make(map[string]int32, len(chunk))
		for _, id := range chunk {
			cellsByObligation[id] = cellsPerRow
		}
		newBatch := domain.NewBatch{
			TenantID:          tenantID,
			ProtocolVersionID: versionID,
			ScopeType:         batchScopeType,
			ScopeID:           batchScopeID,
			Session:           batchSession(g.ruleID, ruleVaccineID.VaccineCode),
			PlannedDate:       plannedDate,
			WindowStart:       windowStart,
			WindowEnd:         windowEnd,
			Status:            "planned",
			EstimatedTargets:  int32(animalCount),
			PlannedQuantity:   strconv.FormatInt(int64(len(chunk))*int64(normalizedDosesPerGoat(cfg.forRule(g.ruleID).DosesPerGoat)), 10),
			QuantityUnit:      "dose",
			BatchingHoldUntil: batchingHoldUntilForUnbatched(selectedRows, plannedDate),
		}
		if assignErr := s.assignVaccinationOperator(ctx, &newBatch, planner.MaxGoatsPerDrive, session); assignErr != nil {
			session.releaseClaims(claimChunk)
			return batched, obligations, assignErr
		}
		// BUG-001: the cap-aware DISTRIBUTED plan is what must be persisted. This path used to
		// compute it here on the PRE-attach candidate set and then throw the result away, and then
		// rebuild the persisted rows from driveAssignmentsForUnbatched -- which stamps the batch's
		// single ConductedBy operator on EVERY row, collapsing every operator split the planner
		// produced. Distribution now runs exactly ONCE, below, on the rows that ACTUALLY attached,
		// and its return value is the row set handed to the replace. Running it once also keeps the
		// session's per-operator/day load bookkeeping (rememberVaccinationDriveAssignmentLoads)
		// counted once, against real attached animal counts rather than the pre-attach candidates.
		// The park path preserves the distributed rows the same way
		// (park_consolidation.go: newBatch.DriveAssignments = distributed).
		//
		// F2 fix: do NOT set newBatch.DriveAssignments here. selectedRows is the PRE-attach
		// candidate set, not what actually attaches -- createBatchWithAttachedIDs only attaches
		// rows still eligible (batch_id IS NULL, status in scheduled/due/in_progress/missed) inside
		// its own transaction, so passing the unfiltered set would let the create path persist a
		// drive-assignment row for an obligation that never attached (different shed/partition/
		// operator key than what actually attached). Leave DriveAssignments empty on the create
		// call; the real, attached-only set is computed and persisted below via a replace scoped
		// to (tenant, batch) once attachedIDs is known.
		newBatch.DriveAssignments = nil
		batchID, attachedIDs, createErr := s.createBatchWithAttachedIDs(ctx, newBatch, chunk, cellsByObligation)
		if createErr != nil {
			session.releaseClaims(claimChunk)
			return batched, obligations, createErr
		}
		if len(attachedIDs) == 0 {
			session.releaseClaims(claimChunk)
			continue
		}
		// Scope drive assignments to only those obligations that actually attached, then REPLACE
		// (not upsert) the batch's whole drive-assignment row set with exactly that scoped set, so
		// no row for a selected-but-unattached obligation (a different shed/partition/operator key)
		// can survive -- idempotent/replay-safe: re-running the same attach recomputes and
		// re-replaces the identical set.
		attachedRows := selectedUnbatchedRows(selectedRows, attachedIDs)
		scopedAssignments := driveAssignmentsForUnbatched(batchID, newBatch, attachedRows)
		// projection-review: BUG-001 -- the persisted drive-assignment row set is the DISTRIBUTED
		// plan's grain, not the batch's single-operator grain.
		//
		//   Producer unique columns : distributeVaccinationDriveAssignments output ->
		//                             (batch_id, planned_date, park_id, shed_id, physical_shed,
		//                              partition_label, operator_id)
		//   Consumer match columns  : ReplaceVaccinationDriveAssignmentsForBatch -> DELETE by
		//                             (tenant_id, batch_id) then INSERT on the SAME seven columns
		//                             (unique index vaccination_drive_assignments_batch_shed_part_
		//                             operator_uq, migration 000022). Identical column lists, so no
		//                             two produced rows can collide and silently collapse.
		//   Row multiplicity        : attachedRows = 1 row per ATTACHED obligation (the many side);
		//                             driveAssignmentsForUnbatched PRE-AGGREGATES it to 1 row per
		//                             (park, shed, physical shed, partition) over a de-duplicated
		//                             animal-target set; distribution then fans ONE such bucket into
		//                             1..N operator rows carrying DISJOINT animal slices, so
		//                             sum(animal_count) over the output still equals the bucket's
		//                             unique animal count (asserted end-to-end in
		//                             TestShedFallbackPersistsDistributedOperatorSplit).
		//   Numerator/denominator   : the operator cap check ranges over ONE key set on both sides --
		//                             numerator = animals placed on operator O for
		//                             (tenant, park=batch.ScopeID, plannedDate, O); denominator =
		//                             that same operator's remaining cap read at the SAME
		//                             (tenant, park, plannedDate, O) key via
		//                             availableVaccinationOperatorsForDrive minus
		//                             session.vaccinationOperatorLoad at that key.
		//
		// BUG-001: persist the DISTRIBUTED (cap-aware, per-operator) plan, not the single-operator
		// rebuild. distributeVaccinationDriveAssignments returns the attached rows re-keyed by the
		// operator/animal-count split the planner produced; when no operator list is available it
		// returns the input unchanged, so the legacy single-operator shape still persists.
		distributedAssignments, distributeErr := s.distributeVaccinationDriveAssignments(ctx, tenantID, newBatch, planner.MaxGoatsPerDrive, scopedAssignments, session)
		if distributeErr != nil {
			session.releaseClaims(claimChunk)
			return batched, obligations, distributeErr
		}
		if err := s.replaceVaccinationDriveAssignmentsForBatch(ctx, tenantID, batchID, distributedAssignments); err != nil {
			session.releaseClaims(claimChunk)
			return batched, obligations, err
		}
		claimUnbatchedDriveAnimals(session, selectedUnbatchedRows(g.rows, attachedIDs), *plannedDate)
		session.releaseClaims(claimsOutsideSelection(claimChunk, attachedIDs))
		batched = true
		obligations += int64(len(attachedIDs))
	}
	return batched, obligations, nil
}

func (s *SweeperService) selectBestUnbatchedDriveDateWithVisitCap(ctx context.Context, tenantID string, now time.Time, rows []domain.UnbatchedDue, targetIDs []string, plannedDate *time.Time, planner domain.DrivePlannerSettings, ruleVaccineID RuleVaccineIdentity, cellsPerObligation int32, session *SweepSession) (*time.Time, []string, []shotCapReservation, func(context.Context) error, error) {
	if cellsPerObligation <= 0 {
		cellsPerObligation = 1
	}
	parkID := firstUnbatchedParkID(rows)
	if plannedDate == nil || (planner.MaxShotsPerAnimalPerDrive <= 0 && planner.MaxGoatsPerDrive <= 0) {
		release, err := s.lockAndRefreshVisitShots(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session)
		if err != nil {
			return plannedDate, nil, nil, noopRelease, err
		}
		selectedIDs, shotClaims, err := selectIDsWithinVisitShotCapForSession(now, rows, plannedDate, planner, ruleVaccineID, session)
		if err != nil {
			_ = release(ctx)
			return plannedDate, nil, nil, noopRelease, err
		}
		return plannedDate, selectedIDs, shotClaims, release, nil
	}

	latest := session.latestUnbatchedDriveDateWithOverflow(rows, ruleVaccineID)
	if latest.IsZero() {
		return plannedDate, nil, nil, noopRelease, nil
	}
	if plannedDate != nil {
		if proposed := businessDate(*plannedDate); proposed.After(latest) {
			latest = proposed
		}
	}

	var bestDate *time.Time
	var bestIDs []string
	bestAnimals := -1
	for probe := businessDate(*plannedDate); !probe.After(latest); {
		if len(session.unbatchedObligationsFeasibleOnDateForVaccine(now, probe, rows, planner, ruleVaccineID)) > 0 {
			day := probe
			visitRelease, err := s.lockAndRefreshVisitShots(ctx, tenantID, targetIDs, &day, planner.MaxShotsPerAnimalPerDrive, session)
			if err != nil {
				return plannedDate, nil, nil, noopRelease, err
			}
			capPlanner, err := s.operatorCapacityPlanner(ctx, tenantID, parkID, &day, planner, session)
			if err != nil {
				_ = visitRelease(ctx)
				return plannedDate, nil, nil, noopRelease, err
			}
			if driveOperatorCapacityExhausted(planner, capPlanner) {
				// F1: operators found but all exhausted for this candidate day -- it cannot admit
				// anything, so skip it exactly like the "no feasible ids" case rather than falling
				// into limitUnbatchedSelectionByDriveAnimals' ambiguous MaxGoatsPerDrive==0 branch.
				if err := visitRelease(ctx); err != nil {
					return plannedDate, nil, nil, noopRelease, err
				}
				probe = probe.AddDate(0, 0, 1)
				continue
			}
			driveRelease, err := s.lockAndRefreshDriveCapacity(ctx, tenantID, parkID, &day, capPlanner.MaxGoatsPerDrive, session)
			if err != nil {
				_ = visitRelease(ctx)
				return plannedDate, nil, nil, noopRelease, err
			}
			release := combineReleases(driveRelease, visitRelease)
			selectedIDs, shotClaims, err := selectIDsWithinVisitShotCapForSession(now, rows, &day, planner, ruleVaccineID, session)
			if err != nil {
				session.releaseClaims(shotClaims)
				_ = release(ctx)
				return plannedDate, nil, nil, noopRelease, err
			}
			cappedIDs := limitUnbatchedSelectionByDriveAnimals(now, rows, selectedIDs, &day, capPlanner, session)
			session.releaseClaims(claimsOutsideSelection(shotClaims, cappedIDs))
			shotClaims = claimsOutsideSelection(shotClaims, differenceIDs(selectedIDs, cappedIDs))
			animals := uniqueUnbatchedTargetCount(selectedUnbatchedRows(rows, cappedIDs))
			session.releaseClaims(shotClaims)
			if err := release(ctx); err != nil {
				return plannedDate, nil, nil, noopRelease, err
			}
			if animals > bestAnimals || (animals == bestAnimals && len(cappedIDs) > len(bestIDs)) {
				chosen := day
				bestDate = &chosen
				bestIDs = append(bestIDs[:0], cappedIDs...)
				bestAnimals = animals
			}
		}
		probe = probe.AddDate(0, 0, 1)
	}
	if bestDate == nil || len(bestIDs) == 0 {
		return plannedDate, nil, nil, noopRelease, nil
	}

	visitRelease, err := s.lockAndRefreshVisitShots(ctx, tenantID, targetIDs, bestDate, planner.MaxShotsPerAnimalPerDrive, session)
	if err != nil {
		return bestDate, nil, nil, noopRelease, err
	}
	capPlanner, err := s.operatorCapacityPlanner(ctx, tenantID, parkID, bestDate, planner, session)
	if err != nil {
		_ = visitRelease(ctx)
		return bestDate, nil, nil, noopRelease, err
	}
	if driveOperatorCapacityExhausted(planner, capPlanner) {
		// F1: operators found but all exhausted on the chosen best date -- admit nothing rather
		// than falling into limitUnbatchedSelectionByDriveAnimals' ambiguous MaxGoatsPerDrive==0
		// "no cap configured" branch.
		if err := visitRelease(ctx); err != nil {
			return bestDate, nil, nil, noopRelease, err
		}
		return bestDate, nil, nil, noopRelease, nil
	}
	driveRelease, err := s.lockAndRefreshDriveCapacity(ctx, tenantID, parkID, bestDate, capPlanner.MaxGoatsPerDrive, session)
	if err != nil {
		_ = visitRelease(ctx)
		return bestDate, nil, nil, noopRelease, err
	}
	release := combineReleases(driveRelease, visitRelease)
	selectedIDs, shotClaims, err := selectIDsWithinVisitShotCapForSession(now, rows, bestDate, planner, ruleVaccineID, session)
	if err != nil {
		_ = release(ctx)
		return bestDate, nil, nil, noopRelease, err
	}
	cappedIDs := limitUnbatchedSelectionByDriveAnimals(now, rows, selectedIDs, bestDate, capPlanner, session)
	if len(cappedIDs) < len(selectedIDs) {
		session.releaseClaims(claimsOutsideSelection(shotClaims, cappedIDs))
		shotClaims = claimsOutsideSelection(shotClaims, differenceIDs(selectedIDs, cappedIDs))
		selectedIDs = cappedIDs
	}
	return bestDate, selectedIDs, shotClaims, release, nil
}

func vaccinationDriveBatchScope(g *dueGroup, rows []domain.UnbatchedDue) (string, string, error) {
	if g == nil {
		return "", "", fmt.Errorf("obligation: missing due group for vaccination drive batch")
	}
	if g.scopeType != "shed" {
		return g.scopeType, g.scopeID, nil
	}
	parkID := strings.TrimSpace(g.parkID)
	for _, row := range rows {
		rowParkID := strings.TrimSpace(row.ParkID)
		if rowParkID == "" {
			return "", "", fmt.Errorf("obligation: vaccination shed obligation %s missing park_id; seed/import must assign every live goat to a real park and shed before batching", row.ObligationID)
		}
		if parkID == "" {
			parkID = rowParkID
		}
		if rowParkID != parkID {
			return "", "", fmt.Errorf("obligation: vaccination fallback batch crossed parks for shed group %s: %s vs %s", g.scopeID, parkID, rowParkID)
		}
	}
	if parkID == "" {
		return "", "", fmt.Errorf("obligation: vaccination shed group %s missing park_id; cannot create non-park drive batch", g.scopeID)
	}
	return "park", parkID, nil
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

func uniqueUnbatchedTargetCount(rows []domain.UnbatchedDue) int {
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		targetID := strings.TrimSpace(row.TargetID)
		if targetID == "" {
			targetID = strings.TrimSpace(row.ObligationID)
		}
		if targetID == "" {
			continue
		}
		seen[targetID] = struct{}{}
	}
	return len(seen)
}

// limitUnbatchedSelectionByDriveAnimals enforces the park/date animal-slot cap with two-pass
// admission (VAXCAP-006): pass 1 reserves slots for IMMOVABLE rows (no later feasible date inside
// the goat's own safe window / due+7 hold), which are admitted even beyond cap; pass 2 fills the
// remaining capacity with movable rows. Overflow beyond cap is legitimate only when the immovable
// animals alone exceed it -- a movable row can never displace a last-safe row into overflow.
func limitUnbatchedSelectionByDriveAnimals(now time.Time, rows []domain.UnbatchedDue, selected []string, plannedDate *time.Time, planner domain.DrivePlannerSettings, session *SweepSession) []string {
	if planner.MaxGoatsPerDrive <= 0 || plannedDate == nil || len(selected) == 0 {
		return selected
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	parkID := firstUnbatchedParkID(rows)
	used := session.driveCapacityUsed(parkID, *plannedDate)
	canMove := func(row domain.UnbatchedDue) bool {
		return driveCandidateCanMoveAfter(now, *plannedDate, driveCandidate{
			ObligationID:             row.ObligationID,
			TargetID:                 row.TargetID,
			TargetReproductiveStatus: row.TargetReproductiveStatus,
			DueAt:                    row.DueAt,
			WindowStart:              row.WindowStart,
			WindowEnd:                row.WindowEnd,
			BatchingHoldCount:        row.BatchingHoldCount,
			FirstBatchingHoldUntil:   row.FirstBatchingHoldUntil,
		}, planner)
	}
	admitted := make(map[string]struct{}, len(selected))
	admittedTargets := make(map[string]struct{}, len(selected))
	// Pass 1: immovable rows reserve capacity first (admitted unconditionally -- last-safe overflow).
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; !ok {
			continue
		}
		if canMove(row) {
			continue
		}
		targetKey := unbatchedTargetKey(row)
		if _, ok := admittedTargets[targetKey]; !ok {
			admittedTargets[targetKey] = struct{}{}
			used++
		}
		admitted[row.ObligationID] = struct{}{}
	}
	// Pass 2: movable rows fill only the remaining capacity, never pushing past the cap.
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; !ok {
			continue
		}
		if _, ok := admitted[row.ObligationID]; ok {
			continue
		}
		targetKey := unbatchedTargetKey(row)
		if _, ok := admittedTargets[targetKey]; !ok {
			if used+1 > planner.MaxGoatsPerDrive {
				continue
			}
			admittedTargets[targetKey] = struct{}{}
			used++
		}
		admitted[row.ObligationID] = struct{}{}
	}
	out := make([]string, 0, len(admitted))
	for _, row := range rows {
		if _, ok := admitted[row.ObligationID]; ok {
			out = append(out, row.ObligationID)
		}
	}
	return out
}

func unbatchedTargetKey(row domain.UnbatchedDue) string {
	targetID := strings.TrimSpace(row.TargetID)
	if targetID != "" {
		return targetID
	}
	return strings.TrimSpace(row.ObligationID)
}

func claimUnbatchedDriveAnimals(session *SweepSession, rows []domain.UnbatchedDue, plannedDate time.Time) {
	if session == nil {
		return
	}
	seenTargets := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		targetKey := unbatchedTargetKey(row)
		if _, ok := seenTargets[targetKey]; ok {
			continue
		}
		seenTargets[targetKey] = struct{}{}
		session.claimDriveCapacity(row.ParkID, plannedDate, row.ObligationID, 1)
	}
}

func driveAssignmentsForUnbatched(batchID string, batch domain.NewBatch, rows []domain.UnbatchedDue) []domain.DriveAssignment {
	if strings.TrimSpace(batchID) == "" || batch.PlannedDate == nil || strings.TrimSpace(batch.ScopeID) == "" || len(rows) == 0 {
		return nil
	}
	type assignmentBucket struct {
		parkID       string
		shedID       *string
		physicalShed string
		partition    string
		targets      map[string]struct{}
		ruleIDs      map[string]struct{}
		doseKeys     map[string]struct{}
	}
	buckets := make(map[string]*assignmentBucket)
	for _, row := range rows {
		parkID := strings.TrimSpace(row.ParkID)
		if parkID == "" {
			parkID = batch.ScopeID
		}
		var shedID *string
		if row.ScopeType == "shed" && strings.TrimSpace(row.ScopeID) != "" {
			s := strings.TrimSpace(row.ScopeID)
			shedID = &s
		}
		physicalShed, partition := normalizeAssignmentShed(row.ShedName)
		if physicalShed == "" {
			physicalShed = "park"
		}
		key := parkID + "\x00" + stringPtrValue(shedID) + "\x00" + physicalShed + "\x00" + partition
		bucket := buckets[key]
		if bucket == nil {
			bucket = &assignmentBucket{
				parkID:       parkID,
				shedID:       shedID,
				physicalShed: physicalShed,
				partition:    partition,
				targets:      map[string]struct{}{},
				ruleIDs:      map[string]struct{}{},
				doseKeys:     map[string]struct{}{},
			}
			buckets[key] = bucket
		}
		targetKey := unbatchedTargetKey(row)
		if targetKey != "" {
			bucket.targets[targetKey] = struct{}{}
			if ruleID := strings.TrimSpace(row.RuleID); ruleID != "" {
				bucket.ruleIDs[ruleID] = struct{}{}
				bucket.doseKeys[targetKey+"\x00"+ruleID] = struct{}{}
			}
		}
	}
	out := make([]domain.DriveAssignment, 0, len(buckets))
	for _, bucket := range buckets {
		out = append(out, domain.DriveAssignment{
			BatchID:        batchID,
			PlannedDate:    *batch.PlannedDate,
			OperatorID:     batch.ConductedBy,
			ParkID:         bucket.parkID,
			ShedID:         bucket.shedID,
			PhysicalShed:   bucket.physicalShed,
			PartitionLabel: bucket.partition,
			AnimalCount:    int32(len(bucket.targets)),
			VaccineRuleIDs: sortedStringSet(bucket.ruleIDs),
			TotalDoses:     int32(len(bucket.doseKeys)),
			CapacityStatus: "within_cap",
		})
	}
	return out
}

func sortedStringSet(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func normalizeAssignmentShed(raw string) (string, string) {
	physical, partition := vaccexecdomain.NormalizeDriveShed(raw)
	if physical == "" {
		return "", "whole"
	}
	if partition == "" {
		partition = "whole"
	}
	return physical, partition
}

func firstUnbatchedParkID(rows []domain.UnbatchedDue) string {
	for _, row := range rows {
		if strings.TrimSpace(row.ParkID) != "" {
			return row.ParkID
		}
	}
	return ""
}

func differenceIDs(all []string, keep []string) []string {
	keepSet := make(map[string]struct{}, len(keep))
	for _, id := range keep {
		keepSet[id] = struct{}{}
	}
	out := make([]string, 0, len(all))
	for _, id := range all {
		if _, ok := keepSet[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}

func unbatchedDriveWindow(rows []domain.UnbatchedDue) (*time.Time, *time.Time) {
	var windowStart *time.Time
	var windowEnd *time.Time
	for _, row := range rows {
		start := unbatchedEarliestDate(row)
		if windowStart == nil || start.After(*windowStart) {
			s := start
			windowStart = &s
		}
		if row.WindowEnd != nil && !row.WindowEnd.IsZero() {
			end := biztime.BusinessDayStart(*row.WindowEnd)
			if windowEnd == nil || end.Before(*windowEnd) {
				e := end
				windowEnd = &e
			}
		}
	}
	return windowStart, windowEnd
}

func unbatchedEarliestDate(row domain.UnbatchedDue) time.Time {
	if row.WindowStart != nil && !row.WindowStart.IsZero() {
		return biztime.BusinessDayStart(*row.WindowStart)
	}
	if !row.DueAt.IsZero() {
		return biztime.BusinessDayStart(row.DueAt)
	}
	return time.Time{}
}

func batchingHoldUntilForUnbatched(rows []domain.UnbatchedDue, plannedDate *time.Time) *time.Time {
	if plannedDate == nil || !driveDateUsesBatchingHold(*plannedDate, driveCandidatesFromUnbatched(rows)) {
		return nil
	}
	holdUntil := *plannedDate
	return &holdUntil
}

func splitShotCapReservations(claims []shotCapReservation, selectedIDs []string, chunks [][]string) [][]shotCapReservation {
	out := make([][]shotCapReservation, len(chunks))
	if len(claims) == 0 || len(selectedIDs) == 0 {
		return out
	}
	claimsByID := make(map[string][]shotCapReservation, len(claims))
	for _, claim := range claims {
		claimsByID[claim.obligationID] = append(claimsByID[claim.obligationID], claim)
	}
	for chunkIndex, chunk := range chunks {
		for _, id := range chunk {
			out[chunkIndex] = append(out[chunkIndex], claimsByID[id]...)
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
// tiny leftovers across sheds in the same park. The threshold is inclusive: with the default
// MinShedDriveTargets=2, both 1- and 2-animal groups get a park-clubbing chance before fallback.
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
	return int32(obligationCount) <= min
}

// batchRemainingShedObligations drains every still-unbatched shed obligation into vaccination drive
// batches. The obligation remains shed-scoped for animal roster/audit, but the created drive batch
// is park-scoped via vaccinationDriveBatchScope.
func (s *SweeperService) batchRemainingShedObligations(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time) (domain.SweepResult, error) {
	planner := normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode)
	return s.batchRemainingShedObligationsWithVisitCounts(ctx, tenantID, versionID, cfg, time.Time{}, dueBefore, planner, NewSweepSession(), time.Time{}, nil)
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
		if err := rejectNonShedUnbatchedDue(snapshotRows); err != nil {
			return res, err
		}
		order, groups := groupUnbatchedDue(filterShedRows(snapshotRows), planner.SpeciesGroupingPolicy)
		order = orderDueGroupsByVaccinePriority(order, groups, cfg)
		for _, k := range order {
			g := groups[k]
			batched, n, err := s.batchDueGroup(ctx, tenantID, versionID, cfg, planner, asOf, dueBefore, session, g)
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
		if err := rejectNonShedUnbatchedDue(rows); err != nil {
			return res, err
		}
		order, groups := groupUnbatchedDue(filterShedRows(rows), planner.SpeciesGroupingPolicy)
		order = orderDueGroupsByVaccinePriority(order, groups, cfg)

		var progressed int64
		for _, k := range order {
			g := groups[k]
			batched, n, err := s.batchDueGroup(ctx, tenantID, versionID, cfg, planner, asOf, dueBefore, session, g)
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

func rejectNonShedUnbatchedDue(rows []domain.UnbatchedDue) error {
	for _, r := range rows {
		if r.ScopeType != "shed" {
			return fmt.Errorf("obligation: vaccination sweep saw non-shed goat obligation %s scoped to %s/%s; active animals must have a shed before generation", r.ObligationID, r.ScopeType, r.ScopeID)
		}
	}
	return nil
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
			// Never persist an internal scope UUID inside user-visible task copy. App clients render
			// the locale-aware SOP task presentation contract; this generic title is legacy-safe.
			Title:     "Vaccination drive",
			ScopeType: b.ScopeType,
			ScopeID:   b.ScopeID,
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
		Type             string `json:"type"`
		PathogenClass    string `json:"pathogen_class"`
		CompatibilityGrp string `json:"compatibility_group"`
		InventoryItemID  string `json:"inventory_item_id"`
		Priority         int32  `json:"priority"`
	}
	if err := json.Unmarshal(payload.Vaccine, &vaccine); err != nil {
		return RuleVaccineIdentity{}
	}
	vaccineCode := strings.TrimSpace(vaccine.Code)
	priority := vaccine.Priority
	if priority <= 0 {
		priority = VaccineMatrixPriority(vaccineCode)
	}
	return RuleVaccineIdentity{
		VaccineCode:      vaccineCode,
		VaccinePriority:  priority,
		CompatibilityGrp: strings.TrimSpace(vaccine.CompatibilityGrp),
		VaccineItemID:    strings.TrimSpace(vaccine.InventoryItemID),
		VaccineType:      strings.TrimSpace(vaccine.Type),
		PathogenClass:    strings.TrimSpace(vaccine.PathogenClass),
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
