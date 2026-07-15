package app

import (
	"context"
	"fmt"
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

// AlignComboDrives harmonizes planned dates for approved combo bundles (FMD+HS, etc.) after
// per-version sweeps. maxShotsPerAnimalPerDrive (0 = no cap) and session together guard against
// co-locating combo batches onto one shared date pushing a member animal past
// MaxShotsPerAnimalPerDrive: a batch that would push any of its animals over the cap on the
// target date is left on its own (already-safe) planned date instead of being aligned. Pass the
// same SweepSession used for the preceding SweepVersionWithSession calls so the check also
// counts shots already claimed by those sweeps, not just by the combo batches being aligned.
//
// Pages through all candidate combo batches using keyset pagination (ORDER BY b.scope_type,
// b.scope_id, b.session, b.planned_date, b.batch_id) so no combo group is truncated at the
// page boundary.
func (s *SweeperService) AlignComboDrives(ctx context.Context, tenantID string, alignWindowDays int32, dueBefore time.Time, maxShotsPerAnimalPerDrive int32, session *SweepSession) (int, error) {
	aligner, ok := s.repo.(ComboDriveAligner)
	if !ok || alignWindowDays <= 0 {
		return 0, nil
	}
	session = sessionOrNew(session)
	aligned := 0
	window := time.Duration(alignWindowDays) * 24 * time.Hour
	// Keyset pagination ordered by (scope_type, scope_id, session, planned_date, batch_id), so a
	// group's rows are contiguous in the stream. But a single group can still STRADDLE a page
	// boundary (a group of 5 batches over pages of 2), so we cannot rebuild groups per-page and
	// process them there -- that fragments the straddling group and aligns each page-fragment to
	// its own local target date instead of the group's single true target. Instead we carry the
	// last (tail) group across pages and only process a group once we have seen a row with a
	// DIFFERENT group key (proving the group is complete) or reached EOF.
	var after *domain.ComboBatchCursor
	var pending []domain.ComboDriveBatch // accumulated rows of the group currently being read
	var pendingKey comboGroupKey
	havePending := false
	for pages := 0; pages < maxComboAlignPagesPerRun; pages++ {
		rows, err := aligner.ListPlannedComboBatchesKeyset(ctx, tenantID, dueBefore, after, s.page)
		if err != nil {
			return aligned, err
		}
		if len(rows) == 0 {
			break
		}

		for _, row := range rows {
			if !strings.HasPrefix(row.Session, "combo:") {
				continue
			}
			key := comboGroupKey{scopeType: row.ScopeType, scopeID: row.ScopeID, session: row.Session}
			if havePending && key != pendingKey {
				// The previous group is now fully read (this row belongs to a different group):
				// flush it. The flush is synchronous, so reusing pending's backing array after it
				// returns is safe.
				n, err := s.alignComboGroup(ctx, tenantID, aligner, pending, dueBefore, window, maxShotsPerAnimalPerDrive, session)
				if err != nil {
					return aligned, err
				}
				aligned += n
				pending = pending[:0]
			}
			pending = append(pending, row)
			pendingKey = key
			havePending = true
		}

		if int32(len(rows)) < s.page {
			break
		}
		last := rows[len(rows)-1]
		after = &domain.ComboBatchCursor{
			ScopeType:   last.ScopeType,
			ScopeID:     last.ScopeID,
			Session:     last.Session,
			PlannedDate: last.PlannedDate,
			BatchID:     last.BatchID,
		}
		if pages == maxComboAlignPagesPerRun-1 {
			return aligned, fmt.Errorf("obligation: combo batch alignment exceeded %d pages without draining", maxComboAlignPagesPerRun)
		}
	}
	// Flush the final group accumulated at EOF.
	if havePending && len(pending) > 0 {
		n, err := s.alignComboGroup(ctx, tenantID, aligner, pending, dueBefore, window, maxShotsPerAnimalPerDrive, session)
		if err != nil {
			return aligned, err
		}
		aligned += n
	}
	return aligned, nil
}

// maxComboAlignPagesPerRun bounds the keyset pagination in AlignComboDrives so a repo bug that
// fails to advance the cursor cannot loop forever.
const maxComboAlignPagesPerRun = 10000

// comboGroupKey identifies one combo-alignment group: batches sharing a scope and combo session
// are aligned onto a single shared drive date.
type comboGroupKey struct {
	scopeType string
	scopeID   string
	session   string
}

// alignComboGroup aligns one complete combo group (all its batches, possibly gathered across
// several pagination pages) onto a single shared target date, honoring the per-animal shot cap.
// Returns the number of batches actually moved.
func (s *SweeperService) alignComboGroup(ctx context.Context, tenantID string, aligner ComboDriveAligner, batches []domain.ComboDriveBatch, dueBefore time.Time, window time.Duration, maxShotsPerAnimalPerDrive int32, session *SweepSession) (int, error) {
	if len(batches) < 2 {
		return 0, nil
	}
	target := pickComboAlignDate(batches, dueBefore, window)
	if target == nil {
		return 0, nil
	}
	aligned := 0
	for _, batch := range batches {
		if batch.PlannedDate != nil && businessDate(*batch.PlannedDate).Equal(*target) {
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
				return aligned, err
			}
			release = rel
		}
		if maxShotsPerAnimalPerDrive > 0 && comboBatchExceedsShotCapAtDate(batch, *target, maxShotsPerAnimalPerDrive, session) {
			// Aligning would push a member animal past the shot cap on the target date;
			// leave this batch on its own already-safe planned date (overflow).
			if err := release(ctx); err != nil {
				return aligned, err
			}
			continue
		}
		if err := aligner.UpdateBatchPlannedDate(ctx, tenantID, batch.BatchID, *target); err != nil {
			_ = release(ctx)
			return aligned, err
		}
		session.claimComboBatchTargets(batch.TargetIDs, *target)
		aligned++
		if err := release(ctx); err != nil {
			return aligned, err
		}
	}
	return aligned, nil
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
