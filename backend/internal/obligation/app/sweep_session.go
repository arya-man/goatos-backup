package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// SweepSession shares per-visit shot-cap accounting across every SweepVersion call made during
// one sweeper run, so MaxShotsPerAnimalPerDrive spans vaccines/protocol versions instead of
// resetting for each version. Without a shared session, an animal due 3 different vaccines
// (3 protocol versions) on the same planned business date could be scheduled 3 shots that day,
// because each version's own SweepVersion call started counting from zero.
//
// A single SweepVersion call still allocates its own private session internally, so isolated
// single-version sweeps keep their existing behavior. The production caller
// (cmd/obligation-sweeper) shares one SweepSession across every published version it sweeps in
// one run, plus the subsequent AlignComboDrives pass, via SweepVersionWithSession.
type SweepSession struct {
	visitShotCounts map[string]int32
	visitClaims     map[string]shotCapClaim
	// loaded marks every visitShotCountKey whose starting count has already been resolved once
	// in this session -- either seeded from the persisted, cross-pass/cross-worker committed
	// shot count (see seedResolved/SweeperService.seedAndLockVisitShots, VAX-REV-01) or, for a
	// repo that doesn't support that seed (e.g. a pure in-memory test fake), explicitly marked
	// resolved-at-zero so every group only pays the seed cost once per key per session, not once
	// per group that happens to share a (date, target) visit.
	loaded map[string]struct{}
}

// shotCapClaim records the vaccine/priority that most recently filled a visit's LAST cap slot,
// so a later obligation competing for the same (date, animal) visit can be told apart as either
// a resolvable lower-priority overflow or an ambiguous same-priority tie.
type shotCapClaim struct {
	vaccineCode string
	priority    int32
}

// NewSweepSession starts a fresh cross-version sweep session with empty shot-cap state.
func NewSweepSession() *SweepSession {
	return &SweepSession{
		visitShotCounts: make(map[string]int32),
		visitClaims:     make(map[string]shotCapClaim),
		loaded:          make(map[string]struct{}),
	}
}

// unresolvedTargets returns the distinct, non-blank target IDs in targetIDs whose (date, target)
// visit-shot-count key has NOT already been resolved in this session (seeded from a persisted
// count, or explicitly marked zero -- see seedResolved), preserving first-seen order. Callers use
// this to seed only the keys that actually need a DB round trip, so a session shared across many
// due-obligation groups on the same visit date pays the seed cost once per key, not once per
// group.
func (s *SweepSession) unresolvedTargets(date time.Time, targetIDs []string) []string {
	out := make([]string, 0, len(targetIDs))
	seen := make(map[string]struct{}, len(targetIDs))
	for _, targetID := range targetIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			continue
		}
		if _, dup := seen[targetID]; dup {
			continue
		}
		seen[targetID] = struct{}{}
		key := visitShotCountKey(date, targetID)
		if _, ok := s.loaded[key]; ok {
			continue
		}
		out = append(out, targetID)
	}
	return out
}

// seedResolved merges a persisted per-target shot count (already committed by a prior sweep pass
// or a concurrent worker, read fresh from Postgres -- see SweeperService.seedAndLockVisitShots)
// into this session's in-memory counts, for every target in targetIDs not already resolved. A
// target with no entry in persisted is seeded at zero. Idempotent per (date, target) key: calling
// this twice for the same key is a no-op the second time, so an already-claimed in-memory count
// from earlier in this SAME pass is never clobbered.
func (s *SweepSession) seedResolved(date time.Time, targetIDs []string, persisted map[string]int32) {
	for _, targetID := range targetIDs {
		key := visitShotCountKey(date, targetID)
		if _, ok := s.loaded[key]; ok {
			continue
		}
		s.loaded[key] = struct{}{}
		s.visitShotCounts[key] += persisted[targetID]
	}
}

// resetResolved SETS this session's in-memory shot count for every (date, target) key to the
// fresh persisted count just read under the per-visit advisory lock (RV-02). Unlike seedResolved
// (additive, once-per-key), this is the WRITE-path primitive called before EACH group's select:
// by the time a group locks a shared visit, every earlier group in this same run has already
// durably committed its claim for that visit, so the fresh persisted count is the AUTHORITATIVE
// starting point -- overwriting (not adding to) the stale in-memory count carried over from the
// earlier group, and simultaneously picking up any shot a concurrent worker committed for the
// same visit in between. It (re)marks the keys loaded so the read-only additive seedResolved can
// never later double-count them. visitClaims is intentionally left intact: it records which
// vaccine last filled a slot so rejectOrTie can still tell a genuine same-priority tie from an
// ordinary overflow after the count is refreshed.
func (s *SweepSession) resetResolved(date time.Time, targetIDs []string, persisted map[string]int32) {
	for _, targetID := range targetIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			continue
		}
		key := visitShotCountKey(date, targetID)
		s.loaded[key] = struct{}{}
		s.visitShotCounts[key] = persisted[targetID]
	}
}

// sessionOrNew returns session, or a fresh private one when the caller passed nil.
func sessionOrNew(session *SweepSession) *SweepSession {
	if session != nil {
		return session
	}
	return NewSweepSession()
}

// ShotCapPriorityTieError is the explicit blocker surfaced when more than
// MaxShotsPerAnimalPerDrive vaccines compete for one animal's planned visit and two of the
// competing vaccines resolve to EQUAL vaccine priority. The sweeper has no configured tiebreak
// in that case, so rather than silently pick an arrival-order winner it refuses the drop and
// reports the conflict: fix drive_policy.priority (or the source vaccine matrix) for one of the
// named vaccines to disambiguate, then re-run the sweep.
type ShotCapPriorityTieError struct {
	Date     time.Time
	TargetID string
	VaccineA string
	VaccineB string
	Priority int32
}

func (e *ShotCapPriorityTieError) Error() string {
	return fmt.Sprintf(
		"obligation: shot-cap priority tie for animal %s on %s between vaccines %q and %q (both resolved priority %d) -- configure drive_policy.priority to disambiguate before re-sweeping",
		e.TargetID, e.Date.Format("2006-01-02"), e.VaccineA, e.VaccineB, e.Priority,
	)
}

// claim records a successful shot-cap claim for one visit key, keeping the last-claim
// vaccine/priority so a subsequently rejected competitor can be checked for a tie.
func (s *SweepSession) claim(key, vaccineCode string, priority int32) {
	s.visitShotCounts[key]++
	s.visitClaims[key] = shotCapClaim{vaccineCode: vaccineCode, priority: priority}
}

// rejectOrTie returns the explicit blocker when a same-priority, different-vaccine obligation
// competes for a visit key that is already at cap, or nil when the rejection is an ordinary,
// resolvable priority-ordered overflow (including a vaccine overflowing its own earlier claim,
// which is not a cross-vaccine conflict at all).
func (s *SweepSession) rejectOrTie(key, vaccineCode string, priority int32, targetID string, plannedDate time.Time) error {
	last, ok := s.visitClaims[key]
	if !ok {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(last.vaccineCode), strings.TrimSpace(vaccineCode)) {
		return nil
	}
	if last.priority != priority {
		return nil
	}
	return &ShotCapPriorityTieError{
		Date:     plannedDate,
		TargetID: targetID,
		VaccineA: last.vaccineCode,
		VaccineB: vaccineCode,
		Priority: priority,
	}
}

// claimComboBatchTargets credits each target's shot count at the aligned date once a combo
// batch is moved onto it, so a later batch processed in the same alignment pass sees the
// updated count for animals it shares with an earlier-aligned batch.
func (s *SweepSession) claimComboBatchTargets(targetIDs []string, date time.Time) {
	for _, targetID := range targetIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			continue
		}
		s.visitShotCounts[visitShotCountKey(date, targetID)]++
	}
}

// selectIDsWithinVisitShotCapForSession is the cross-version-cap-aware sibling of
// selectIDsWithinVisitShotCap: it claims slots on the shared session (spanning every version
// swept in this run) instead of a private per-call map, and it surfaces
// ShotCapPriorityTieError when the cap would force a same-priority, different-vaccine drop
// instead of silently picking an arrival-order winner.
func selectIDsWithinVisitShotCapForSession(rows []domain.UnbatchedDue, plannedDate *time.Time, maxShots int32, vaccineCode string, priority int32, session *SweepSession) ([]string, error) {
	if maxShots <= 0 || plannedDate == nil {
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ObligationID)
		}
		return ids, nil
	}
	selected := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.TargetID) == "" {
			selected = append(selected, row.ObligationID)
			continue
		}
		key := visitShotCountKey(*plannedDate, row.TargetID)
		if session.visitShotCounts[key] >= maxShots {
			if err := session.rejectOrTie(key, vaccineCode, priority, row.TargetID, *plannedDate); err != nil {
				return nil, err
			}
			continue
		}
		session.claim(key, vaccineCode, priority)
		selected = append(selected, row.ObligationID)
	}
	return selected, nil
}

// selectParkIDsWithinVisitShotCapForSession is the park-consolidation sibling of
// selectIDsWithinVisitShotCapForSession (see its docs).
func selectParkIDsWithinVisitShotCapForSession(rows []domain.ParkConsolidationCandidate, selected []string, plannedDate *time.Time, maxShots int32, vaccineCode string, priority int32, session *SweepSession) ([]string, error) {
	if maxShots <= 0 || plannedDate == nil || len(selected) == 0 {
		return selected, nil
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	out := make([]string, 0, len(selected))
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; !ok {
			continue
		}
		if strings.TrimSpace(row.TargetID) == "" {
			out = append(out, row.ObligationID)
			continue
		}
		key := visitShotCountKey(*plannedDate, row.TargetID)
		if session.visitShotCounts[key] >= maxShots {
			if err := session.rejectOrTie(key, vaccineCode, priority, row.TargetID, *plannedDate); err != nil {
				return nil, err
			}
			continue
		}
		session.claim(key, vaccineCode, priority)
		out = append(out, row.ObligationID)
	}
	return out, nil
}

// SweepVersionPriority pairs a protocol version with its resolved sweep config, letting a
// caller that sweeps several versions in one run (multiple vaccines/protocols competing for the
// same animals) order them by disease priority before sweeping. See SortSweepVersionsByPriority.
type SweepVersionPriority struct {
	VersionID string
	Config    SweepConfig
}

// SortSweepVersionsByPriority orders versions ascending by resolved vaccine priority (lower =
// schedule first, matching VaccineMatrixPriority/drive_policy.priority semantics) so a shared
// SweepSession's per-animal shot cap is claimed by the higher-priority vaccine first,
// regardless of the arbitrary order in which the caller discovered/listed the versions
// (deterministic priority selection, not arrival order).
//
// Equal-priority versions are ordered by VersionID purely to give a stable, reproducible SWEEP
// ORDER across runs; that tiebreak does NOT resolve an actual same-priority visit conflict --
// the shared session itself still returns ShotCapPriorityTieError when two equal-priority
// vaccines genuinely compete for the same animal's over-cap visit (see SweepSession).
func SortSweepVersionsByPriority(versions []SweepVersionPriority) []SweepVersionPriority {
	out := make([]SweepVersionPriority, len(versions))
	copy(out, versions)
	sort.SliceStable(out, func(i, j int) bool {
		pi := resolvedVersionPriority(out[i].Config)
		pj := resolvedVersionPriority(out[j].Config)
		if pi != pj {
			return pi < pj
		}
		return out[i].VersionID < out[j].VersionID
	})
	return out
}

func resolvedVersionPriority(cfg SweepConfig) int32 {
	return normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode).VaccinePriority
}
