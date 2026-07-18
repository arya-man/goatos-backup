package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// visitShotCounter is satisfied by a repo that can report the persisted (already-committed, from
// a prior sweep pass or a concurrent worker) shot count per target for one planned visit date --
// the read-only half of the VAX-REV-01 fix. It never blocks/locks, so it is safe to call for a
// write-free preflight (see PreflightVisitShotCapTies) as well as for the real sweep's seed step.
type visitShotCounter interface {
	CountVisitShotsForTargets(ctx context.Context, tenantID string, targetIDs []string, date time.Time) (map[string]int32, error)
}

// visitShotLocker is satisfied by a repo that can additionally hold a per-visit
// (tenant, target, date) lock across the caller's read-count -> decide -> write sequence, so two
// sweeper workers racing for the same animal/date visit cannot both observe a stale committed
// count and jointly exceed MaxShotsPerAnimalPerDrive. Only the production Postgres adapter
// implements this; test fakes fall back to visitShotCounter (or plain in-memory session
// accounting when neither is implemented), which is correct for the existing single-process,
// single-pass unit tests but not for cross-worker safety -- production always wires Postgres.
type visitShotLocker interface {
	visitShotCounter
	// LockVisitShots takes a session-level Postgres advisory lock keyed on hash(tenant, target,
	// date) for every distinct target in targetIDs, in a stable sort order (so two callers
	// racing for overlapping target sets cannot deadlock each other), then reads the FRESH
	// persisted shot count for each locked target under that lock. The caller MUST invoke the
	// returned release func exactly once, after its own writes for this (targetIDs, date) claim
	// are durably committed (success or failure) -- releasing early reopens the race window this
	// exists to close.
	LockVisitShots(ctx context.Context, tenantID string, targetIDs []string, date time.Time) (counts map[string]int32, release func(context.Context) error, err error)
}

// noopRelease is returned by seedAndLockVisitShots whenever there is nothing to lock (cap
// disabled, no planned date, no unresolved targets, or the repo does not support visitShotLocker):
// callers can unconditionally defer/call the returned release without a nil check.
func noopRelease(context.Context) error { return nil }

// seedAndLockVisitShots is the write-path half of the VAX-REV-01 fix: it seeds session with the
// persisted, already-committed shot count for every (date, target) visit key in targetIDs that
// this session has not already resolved, then -- when the repo supports it -- holds a per-visit
// advisory lock across the caller's subsequent selectIDsWithinVisitShotCapForSession +
// CreateBatchWithObligations sequence, so a concurrent worker cannot commit a conflicting claim
// for the same visit in between the seed read and this call's write. The caller MUST call the
// returned release func after its writes for this key set are durably committed.
//
// When maxShots<=0 or date is nil the shot cap is not enforced at all (mirrors
// selectIDsWithinVisitShotCapForSession's own bypass), so this is a cheap no-op: no DB round trip,
// no lock.
func (s *SweeperService) seedAndLockVisitShots(ctx context.Context, tenantID string, targetIDs []string, date *time.Time, maxShots int32, session *SweepSession) (func(context.Context) error, error) {
	if maxShots <= 0 || date == nil {
		return noopRelease, nil
	}
	unresolved := session.unresolvedTargets(*date, targetIDs)
	if len(unresolved) == 0 {
		return noopRelease, nil
	}
	if locker, ok := s.repo.(visitShotLocker); ok {
		counts, release, err := locker.LockVisitShots(ctx, tenantID, unresolved, *date)
		if err != nil {
			return noopRelease, err
		}
		session.seedResolved(*date, unresolved, counts)
		return release, nil
	}
	if counter, ok := s.repo.(visitShotCounter); ok {
		counts, err := counter.CountVisitShotsForTargets(ctx, tenantID, unresolved, *date)
		if err != nil {
			return noopRelease, err
		}
		session.seedResolved(*date, unresolved, counts)
		return noopRelease, nil
	}
	// Repo supports neither: mark these keys resolved-at-zero so we don't re-attempt the type
	// assertion for every group touching this visit, matching the pre-fix in-memory-only
	// behavior (correct for a single, uncontested, single-pass sweep).
	session.seedResolved(*date, unresolved, nil)
	return noopRelease, nil
}

// lockAndRefreshVisitShots is the WRITE-path shot-cap primitive (RV-02). Unlike
// seedAndLockVisitShots it does NOT skip keys this session already resolved: for every group that
// is about to write, it re-acquires the per-visit advisory lock for ALL of the group's targets and
// re-reads the FRESH persisted shot count under that lock, then RESETS the session's in-memory
// counts for those keys to that authoritative value (see SweepSession.resetResolved). This closes
// the stale-count race the loaded/unresolvedTargets short-circuit opened: a second group touching a
// visit an earlier group already resolved used to reuse the earlier in-memory count with NO lock at
// all, so a concurrent worker could fill the remaining slot in between and this worker would then
// commit an over-cap shot.
//
// Only the production Postgres repo (visitShotLocker) gets this lock-and-reset treatment; an
// in-memory test fake (no locker) falls back to seedAndLockVisitShots' additive, single-process
// accounting, which is correct for the uncontested single-process sweeps those fakes model. The
// caller MUST hold the returned release until its writes for this (targetIDs, date) claim are
// durably committed.
func (s *SweeperService) lockAndRefreshVisitShots(ctx context.Context, tenantID string, targetIDs []string, date *time.Time, maxShots int32, session *SweepSession) (func(context.Context) error, error) {
	if maxShots <= 0 || date == nil {
		return noopRelease, nil
	}
	locker, ok := s.repo.(visitShotLocker)
	if !ok {
		return s.seedAndLockVisitShots(ctx, tenantID, targetIDs, date, maxShots, session)
	}
	targets := distinctNonBlankTargets(targetIDs)
	if len(targets) == 0 {
		return noopRelease, nil
	}
	counts, release, err := locker.LockVisitShots(ctx, tenantID, targets, *date)
	if err != nil {
		return noopRelease, err
	}
	session.resetResolved(*date, targets, counts)
	return release, nil
}

// distinctNonBlankTargets returns the distinct, trimmed, non-blank target IDs in targetIDs,
// preserving first-seen order.
func distinctNonBlankTargets(targetIDs []string) []string {
	seen := make(map[string]struct{}, len(targetIDs))
	out := make([]string, 0, len(targetIDs))
	for _, t := range targetIDs {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// seedVisitShotCounts is the read-only sibling of seedAndLockVisitShots, used by the write-free
// tie preflight (PreflightVisitShotCapTies), where there is no subsequent write to protect with a
// lock.
func (s *SweeperService) seedVisitShotCounts(ctx context.Context, tenantID string, targetIDs []string, date *time.Time, maxShots int32, session *SweepSession) error {
	release, err := s.seedAndLockVisitShots(ctx, tenantID, targetIDs, date, maxShots, session)
	if err != nil {
		return err
	}
	return release(ctx)
}

// distinctUnbatchedTargetIDs returns the distinct, non-blank target IDs referenced by rows.
func distinctUnbatchedTargetIDs(rows []domain.UnbatchedDue) []string {
	seen := make(map[string]struct{}, len(rows))
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		t := strings.TrimSpace(row.TargetID)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// distinctParkTargetIDs returns the distinct, non-blank target IDs referenced by rows.
func distinctParkTargetIDs(rows []domain.ParkConsolidationCandidate) []string {
	seen := make(map[string]struct{}, len(rows))
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		t := strings.TrimSpace(row.TargetID)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}
