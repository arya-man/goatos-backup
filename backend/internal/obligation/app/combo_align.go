package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// ComboDriveAligner aligns planned combo-session batches across protocol versions onto one shed visit date.
type ComboDriveAligner interface {
	ListPlannedComboBatches(ctx context.Context, tenantID string, dueBefore time.Time, limit int32) ([]domain.ComboDriveBatch, error)
	// ListPlannedComboBatchesKeyset pages through combo batches using keyset pagination.
	// after == nil means start from the beginning. Returns batches ordered by
	// (scope_type, scope_id, session, planned_date, batch_id).
	ListPlannedComboBatchesKeyset(ctx context.Context, tenantID string, dueBefore time.Time, after *domain.ComboBatchCursor, limit int32) ([]domain.ComboDriveBatch, error)
	UpdateBatchPlannedDate(ctx context.Context, tenantID, batchID string, plannedDate time.Time) error
}

// comboGroupKey identifies one (scope_type, scope_id, session) combo-alignment group. Rows sharing
// a key are always contiguous in ListPlannedComboBatchesKeyset's ORDER BY, regardless of how many
// pages the group spans (see R2-06 fix note below).
type comboGroupKey struct {
	scopeType string
	scopeID   string
	session   string
}

func comboRowKey(row domain.ComboDriveBatch) comboGroupKey {
	return comboGroupKey{scopeType: row.ScopeType, scopeID: row.ScopeID, session: row.Session}
}

// AlignComboDrives harmonizes planned dates for approved combo bundles (FMD+HS, etc.) after
// per-version sweeps. maxShotsPerAnimalPerDrive (0 = no cap) and session together guard against
// co-locating combo batches onto one shared date pushing a member animal past
// MaxShotsPerAnimalPerDrive: a batch that would push any of its animals over the cap on the
// target date is left on its own (already-safe) planned date instead of being aligned. Pass the
// same SweepSession used for the preceding SweepVersionWithSession calls so the check also
// counts shots already claimed by those sweeps, not just by the combo batches being aligned.
//
// Pages through all candidate combo batches using keyset pagination ordered by (b.scope_type,
// b.scope_id, b.session, b.batch_id) -- see R2-06 fix: the cursor and ORDER BY deliberately
// EXCLUDE planned_date, because this very loop mutates it via UpdateBatchPlannedDate; keying the
// cursor on a column it writes could let an aligned row's sort position shift between page reads
// and cause a later page to skip or re-read it. Because the grouping columns (scope_type,
// scope_id, session) are an ORDER BY prefix, one group's rows stay contiguous no matter how many
// pages they span, so a group is assembled INCREMENTALLY across page boundaries below (flushed --
// i.e. aligned -- only once its key changes or the query is exhausted) instead of being aligned as
// page-local fragments that are each too small to see the rest of their own group.
func (s *SweeperService) AlignComboDrives(ctx context.Context, tenantID string, alignWindowDays int32, dueBefore time.Time, maxShotsPerAnimalPerDrive int32, maxDriveCells int32, session *SweepSession) (int, error) {
	return s.AlignComboDrivesAsOf(ctx, tenantID, alignWindowDays, dueBefore, dueBefore, maxShotsPerAnimalPerDrive, maxDriveCells, session)
}

// AlignComboDrivesAsOf aligns eligible combo batches loaded through dueBefore, while using asOf as
// the operational business-day floor for planned-date alignment.
func (s *SweeperService) AlignComboDrivesAsOf(ctx context.Context, tenantID string, alignWindowDays int32, asOf, dueBefore time.Time, maxShotsPerAnimalPerDrive int32, maxDriveCells int32, session *SweepSession) (int, error) {
	aligner, ok := s.repo.(ComboDriveAligner)
	if !ok || alignWindowDays <= 0 {
		return 0, nil
	}
	session = sessionOrNew(session)
	aligned := 0
	window := time.Duration(alignWindowDays) * 24 * time.Hour

	var pending []domain.ComboDriveBatch
	var pendingKey comboGroupKey
	havePending := false

	// alignComboCluster is forward-declared so flushPending (defined next) can call it; it is
	// assigned below.
	var alignComboCluster func([]domain.ComboDriveBatch) error

	// flushPending aligns the currently-accumulated group (if it has >=2 members) and clears it. It
	// must only be called once the caller has confirmed no further row in the ordered result can
	// still belong to this group -- i.e. on a group-key change, or once the query is exhausted.
	flushPending := func() error {
		if !havePending {
			return nil
		}
		batches := pending
		pending = nil
		havePending = false
		if len(batches) < 2 {
			return nil
		}
		// R2-06a: partition the park/session group into date-window clusters BEFORE choosing an
		// alignment date, so one far-outlier batch (e.g. Aug 1 alongside an Aug 20/22 pair under a
		// 7-day window) can no longer blow the whole group's span past the window and prevent the
		// otherwise-compatible nearby batches from aligning. Each cluster is aligned independently.
		for _, cluster := range clusterComboByWindow(batches, window) {
			if len(cluster) < 2 {
				continue
			}
			if err := alignComboCluster(cluster); err != nil {
				return err
			}
		}
		return nil
	}

	alignComboCluster = func(batches []domain.ComboDriveBatch) error {
		target := pickComboAlignDate(batches, asOf, window)
		if target == nil {
			return nil
		}
		for _, batch := range batches {
			if batch.PlannedDate != nil && businessDate(*batch.PlannedDate).Equal(*target) {
				continue
			}
			if !comboBatchFeasibleAtDate(batch, *target) {
				// The batch is already a real planned drive. Do not "club" it onto a later combo
				// date if any attached obligation would cross its medical safe window.
				continue
			}
			// Lock this batch's animals on the target date and refresh their persisted shot count
			// BEFORE the cap check, and HOLD the lock across UpdateBatchPlannedDate (RV-02): the old
			// read-only seed released immediately, leaving a read-then-write window in which a
			// concurrent worker (or an earlier-aligned batch that this session did not re-lock) could
			// fill the remaining slot, after which this align would push a member animal past
			// MaxShotsPerAnimalPerDrive. lockAndRefreshVisitShots also seeds a fresh session correctly
			// (a new sweep pass, or AlignComboDrives running standalone) instead of assuming 0 shots
			// already exist on the target date.
			release := noopRelease
			if maxShotsPerAnimalPerDrive > 0 {
				rel, err := s.lockAndRefreshVisitShots(ctx, tenantID, batch.TargetIDs, target, maxShotsPerAnimalPerDrive, session)
				if err != nil {
					return err
				}
				release = rel
			}
			if maxDriveCells > 0 && strings.TrimSpace(batch.ParkID) != "" {
				rel, err := s.lockAndRefreshDriveCapacity(ctx, tenantID, batch.ParkID, target, maxDriveCells, session)
				if err != nil {
					_ = release(ctx)
					return err
				}
				release = combineReleases(rel, release)
			}
			if maxShotsPerAnimalPerDrive > 0 && comboBatchExceedsShotCapAtDate(batch, *target, maxShotsPerAnimalPerDrive, session) {
				// Aligning would push a member animal past the shot cap on the target date;
				// leave this batch on its own already-safe planned date (overflow).
				if err := release(ctx); err != nil {
					return err
				}
				continue
			}
			if maxDriveCells > 0 && comboBatchExceedsDriveCapacityAtDate(batch, *target, maxDriveCells, session) {
				// Combo alignment is optional consolidation. If moving this already-safe batch would
				// overfill the whole park/date drive, leave it on its existing planned date.
				if err := release(ctx); err != nil {
					return err
				}
				continue
			}
			if err := aligner.UpdateBatchPlannedDate(ctx, tenantID, batch.BatchID, *target); err != nil {
				_ = release(ctx)
				return err
			}
			session.claimComboBatchTargets(batch.TargetIDs, *target)
			if maxDriveCells > 0 && strings.TrimSpace(batch.ParkID) != "" {
				session.claimDriveCapacity(batch.ParkID, *target, batch.BatchID, comboBatchCellCount(batch))
			}
			aligned++
			if err := release(ctx); err != nil {
				return err
			}
		}
		return nil
	}

	var after *domain.ComboBatchCursor
	for pages := 0; pages < 10000; pages++ {
		rows, err := aligner.ListPlannedComboBatchesKeyset(ctx, tenantID, dueBefore, after, s.page)
		if err != nil {
			return aligned, err
		}
		if len(rows) == 0 {
			if err := flushPending(); err != nil {
				return aligned, err
			}
			return aligned, nil
		}

		for _, row := range rows {
			if !strings.HasPrefix(row.Session, "combo:") {
				continue
			}
			key := comboRowKey(row)
			if havePending && key != pendingKey {
				// The ordered result can never return to pendingKey once it has moved past it, so
				// the accumulated group is now complete regardless of which page it spanned.
				if err := flushPending(); err != nil {
					return aligned, err
				}
			}
			pending = append(pending, row)
			pendingKey = key
			havePending = true
		}

		// Move to the next page using the last row's keyset cursor (immutable columns only).
		if int32(len(rows)) < s.page {
			if err := flushPending(); err != nil {
				return aligned, err
			}
			return aligned, nil
		}
		last := rows[len(rows)-1]
		after = &domain.ComboBatchCursor{
			ScopeType: last.ScopeType,
			ScopeID:   last.ScopeID,
			Session:   last.Session,
			BatchID:   last.BatchID,
		}
	}
	return aligned, fmt.Errorf("obligation: combo batch alignment exceeded 10000 pages without draining")
}

func comboBatchFeasibleAtDate(batch domain.ComboDriveBatch, target time.Time) bool {
	targetDay := businessDate(target)
	if batch.SafeStart != nil && targetDay.Before(businessDate(*batch.SafeStart)) {
		return false
	}
	if batch.SafeEnd != nil && targetDay.After(businessDate(*batch.SafeEnd)) {
		return false
	}
	if batch.HoldUntil != nil && targetDay.After(businessDate(*batch.HoldUntil)) {
		return false
	}
	return true
}

// comboBatchExceedsShotCapAtDate reports whether moving batch onto target would push any of its
// member animals past maxShots, given shots already claimed for that date in session (by prior
// sweeps and/or earlier-aligned batches in this same pass).
func comboBatchExceedsShotCapAtDate(batch domain.ComboDriveBatch, target time.Time, maxShots int32, session *SweepSession) bool {
	for _, targetID := range batch.TargetIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			continue
		}
		if session.visitShotCounts[visitShotCountKey(target, targetID)] >= maxShots {
			return true
		}
	}
	return false
}

func comboBatchExceedsDriveCapacityAtDate(batch domain.ComboDriveBatch, target time.Time, maxCells int32, session *SweepSession) bool {
	if maxCells <= 0 || strings.TrimSpace(batch.ParkID) == "" {
		return false
	}
	return session.driveCapacityUsed(batch.ParkID, target)+comboBatchCellCount(batch) > maxCells
}

func comboBatchCellCount(batch domain.ComboDriveBatch) int32 {
	if batch.CellCount > 0 {
		return batch.CellCount
	}
	if len(batch.TargetIDs) > 0 {
		return int32(len(batch.TargetIDs))
	}
	return 1
}

// clusterComboByWindow partitions batches into maximal clusters whose planned dates all fall within
// window of the cluster's EARLIEST date (greedy from earliest, deterministic), so a far-outlier
// batch cannot prevent an otherwise-compatible nearby pair from aligning (R2-06a). Each returned
// cluster independently satisfies the window and can be aligned to its own target date. Batches
// without a usable planned date are returned as their own singletons (never aligned).
func clusterComboByWindow(batches []domain.ComboDriveBatch, window time.Duration) [][]domain.ComboDriveBatch {
	dated := make([]domain.ComboDriveBatch, 0, len(batches))
	var singletons [][]domain.ComboDriveBatch
	for _, b := range batches {
		if b.PlannedDate == nil || b.PlannedDate.IsZero() {
			singletons = append(singletons, []domain.ComboDriveBatch{b})
			continue
		}
		dated = append(dated, b)
	}
	sort.SliceStable(dated, func(i, j int) bool {
		return businessDate(*dated[i].PlannedDate).Before(businessDate(*dated[j].PlannedDate))
	})
	var clusters [][]domain.ComboDriveBatch
	var cur []domain.ComboDriveBatch
	var clusterStart time.Time
	for _, b := range dated {
		day := businessDate(*b.PlannedDate)
		if len(cur) == 0 {
			cur = []domain.ComboDriveBatch{b}
			clusterStart = day
			continue
		}
		if day.Sub(clusterStart) > window {
			clusters = append(clusters, cur)
			cur = []domain.ComboDriveBatch{b}
			clusterStart = day
			continue
		}
		cur = append(cur, b)
	}
	if len(cur) > 0 {
		clusters = append(clusters, cur)
	}
	return append(clusters, singletons...)
}

func pickComboAlignDate(batches []domain.ComboDriveBatch, dueBefore time.Time, window time.Duration) *time.Time {
	if len(batches) == 0 {
		return nil
	}
	var minDate, maxDate *time.Time
	for _, batch := range batches {
		if batch.PlannedDate == nil || batch.PlannedDate.IsZero() {
			continue
		}
		day := businessDate(*batch.PlannedDate)
		if minDate == nil || day.Before(*minDate) {
			minDate = &day
		}
		if maxDate == nil || day.After(*maxDate) {
			maxDate = &day
		}
	}
	if minDate == nil || maxDate == nil {
		return nil
	}
	if maxDate.Sub(*minDate) > window {
		return nil
	}
	target := *maxDate
	nowDay := businessDate(dueBefore)
	if target.Before(nowDay) {
		target = nowDay
	}
	return &target
}
