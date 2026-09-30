package app

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// PC CARE REPEAT (maintainer instruction 2026-09-30, docs/decisions/pc-care-repeat.md).
//
// A Preventive Care SOP card may say "repeat every N days". This plans the NEXT task of that work
// for the same pen(s) and the same operator(s):
//
//   - counted from the last task's PLANNED date (a steady calendar rhythm, never shifted by a
//     late approval);
//   - created whether or not the last task is finished (the pen never drops off the cycle; the
//     old one stays as overdue work);
//   - made RepeatLeadDays ahead of the date, so the operator sees it on their list and a
//     tablet-in-feed deworming's removal crew still has its evening;
//   - the interval is read from the PUBLISHED document when the next task is made -- repeating is
//     a planning act, so a change from 30 to 45, or clearing the field, governs the next one;
//   - if none of the last operators can still do the work (left, inactive, moved park) the pen is
//     SKIPPED and the person who planned the last task is alerted once. Nobody is substituted:
//     the repo's no-fallback rule for who does work.

// RepeatLeadDays is how far ahead of its date a repeated task is made.
const RepeatLeadDays = 2

// Repeat skip reasons (pc_care_repeat_skips.reason).
const (
	RepeatSkipNoOperator          = "no_operator_available"
	RepeatSkipRemovalWindowClosed = "removal_window_closed"
)

// RepeatSkip is one alert the planner is owed: the pens of their last task that could not be
// repeated, and why.
type RepeatSkip struct {
	PlannerUserID string
	Category      string
	ParkID        string
	ParkName      string
	PenLabels     []string
	DueDate       time.Time
	Reason        string
}

// RepeatAlerter tells the planner their work was not repeated. Production: the notification
// bridge's push; nil logs nothing and alerts nobody (the skip row is still written).
type RepeatAlerter interface {
	NotifyRepeatSkipped(ctx context.Context, tenantID string, skip RepeatSkip) error
}

// RepeatResult reports one tick.
type RepeatResult struct {
	RoundsCreated int
	PensCreated   int
	PensSkipped   int
	Conflicts     int
}

type repeatGroup struct {
	key        string
	category   string
	parkID     string
	parkName   string
	createdBy  string
	next       time.Time
	assignees  []string
	hadRemoval bool
	removalOps []string
	candidates []ports.RepeatCandidate
}

// RunRepeat performs one bounded repeat tick for a tenant.
func (s *Service) RunRepeat(ctx context.Context, tenantID string, store ports.RepeatStore, alerter RepeatAlerter, limit int) (RepeatResult, error) {
	var result RepeatResult
	if store == nil || s.rounds == nil {
		return result, ports.ErrStoreUnavailable
	}
	rules, err := s.publishedRules(ctx, tenantID)
	if err != nil {
		return result, err
	}
	cfg := []ports.RepeatConfig{}
	for _, category := range domain.PlannerCategories {
		if every := rules.RepeatEveryDays(category); every > 0 {
			cfg = append(cfg, ports.RepeatConfig{Category: category, EveryDays: every})
		}
	}
	if len(cfg) == 0 {
		return result, nil
	}
	now := s.now()
	today := biztime.BusinessDayStart(now)
	through := today.AddDate(0, 0, RepeatLeadDays)
	candidates, err := store.ListRepeatCandidates(ctx, tenantID, cfg, through, limit)
	if err != nil {
		return result, err
	}
	// One tick groups the candidates by the task set they came from, so a round of four pens is
	// repeated as one round of four pens with one crew, not four single-pen tasks.
	for _, g := range groupRepeatCandidates(candidates, today) {
		// scale-guard:ignore: bounded by the per-tick candidate LIMIT; one small set-based availability read and one round create per source round, never per animal.
		created, skipped, conflict, err := s.repeatGroup(ctx, tenantID, rules, store, alerter, g, now)
		if err != nil {
			return result, err
		}
		if created > 0 {
			result.RoundsCreated++
			result.PensCreated += created
		}
		result.PensSkipped += skipped
		if conflict {
			result.Conflicts++
		}
	}
	return result, nil
}

func groupRepeatCandidates(candidates []ports.RepeatCandidate, today time.Time) []*repeatGroup {
	byKey := map[string]*repeatGroup{}
	order := []string{}
	for _, c := range candidates {
		next := c.PlannedBusinessDate.AddDate(0, 0, c.EveryDays)
		next = time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, biztime.DefaultLocation())
		if next.Before(today) {
			// A worker that was down past the date plans for today, never for a day already gone.
			next = today
		}
		assignees := append([]string(nil), c.AssigneeUserIDs...)
		sort.Strings(assignees)
		removalOps := append([]string(nil), c.RemovalOperatorUserIDs...)
		sort.Strings(removalOps)
		source := c.SourceRoundID
		if source == "" {
			source = c.SourceTaskID
		}
		key := strings.Join([]string{c.Category, c.ParkID, source, next.Format("2006-01-02"),
			strings.Join(assignees, ","), fmt.Sprint(c.HadRemoval), strings.Join(removalOps, ","), c.CreatedBy}, "|")
		g := byKey[key]
		if g == nil {
			g = &repeatGroup{key: key, category: c.Category, parkID: c.ParkID, parkName: c.ParkName, createdBy: c.CreatedBy, next: next,
				assignees: assignees, hadRemoval: c.HadRemoval, removalOps: removalOps}
			byKey[key] = g
			order = append(order, key)
		}
		g.candidates = append(g.candidates, c)
	}
	out := make([]*repeatGroup, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out
}

func (s *Service) repeatGroup(ctx context.Context, tenantID string, rules domain.Rules, store ports.RepeatStore, alerter RepeatAlerter, g *repeatGroup, now time.Time) (created, skipped int, conflict bool, err error) {
	available, err := store.OperatorsAvailableInPark(ctx, tenantID, g.parkID, append(append([]string(nil), g.assignees...), g.removalOps...))
	if err != nil {
		return 0, 0, false, err
	}
	stillAble := intersectIDs(g.assignees, available)
	if len(stillAble) == 0 {
		n, err := s.skipRepeat(ctx, tenantID, store, alerter, g, RepeatSkipNoOperator)
		return 0, n, false, err
	}
	// The removal follows the published card: required means it rides every repeat of a listed
	// category; optional repeats what the last task's planner chose. A card that no longer
	// applies it to this work plans none.
	hadRemoval := g.hadRemoval
	removal, rerr := rules.RemovalDecision(g.category, &hadRemoval)
	if rerr != nil {
		removal = false
	}
	var removalOps []string
	if removal {
		removalOps = intersectIDs(g.removalOps, available)
		if len(removalOps) == 0 {
			n, err := s.skipRepeat(ctx, tenantID, store, alerter, g, RepeatSkipNoOperator)
			return 0, n, false, err
		}
		cutoff, cerr := s.removalCutoff(ctx, tenantID)
		if cerr != nil {
			return 0, 0, false, cerr
		}
		if g.next.Before(domain.EarliestFeedRemovalDewormingDate(now, cutoff)) {
			n, err := s.skipRepeat(ctx, tenantID, store, alerter, g, RepeatSkipRemovalWindowClosed)
			return 0, n, false, err
		}
	}

	pens := make([]domain.RoundPen, 0, len(g.candidates))
	repeatOf := map[string]string{}
	sources := make([]string, 0, len(g.candidates))
	for _, c := range g.candidates {
		pen := domain.RoundPen{ShedID: c.ShedID, PartitionLabel: c.PartitionLabel}
		pens = append(pens, pen)
		repeatOf[pen.PenKey()] = c.SourceTaskID
		sources = append(sources, c.SourceTaskID)
	}
	sort.Strings(sources)
	sum := sha1.Sum([]byte(strings.Join(sources, ",") + "|" + g.next.Format("2006-01-02")))
	_, err = s.rounds.CreateRound(ctx, ports.CreateRoundParams{
		TenantID:                tenantID,
		Category:                g.category,
		ParkID:                  g.parkID,
		Pens:                    pens,
		PlannedBusinessDate:     g.next,
		AssigneeUserIDs:         stillAble,
		FeedRemovalRequired:     removal,
		RemovalOperatorUserIDs:  removalOps,
		SOPVersion:              rules.Version,
		SlotKeys:                allKeys(slotsAsAuthored(rules.CategorySlots(g.category))),
		RequiredSlotKeys:        rules.RequiredSlotKeys(g.category),
		RemovalSlotKeys:         allKeys(rules.RemovalProofs()),
		RemovalRequiredSlotKeys: requiredKeys(rules.RemovalProofs()),
		IdempotencyKey:          "pc-care-repeat:" + hex.EncodeToString(sum[:]),
		RepeatOf:                repeatOf,
		// The planner of the last task stays the owner of the next: they can end it, and they
		// are who is told when a repeat cannot be made.
		CreatedBy: g.createdBy,
		ActorID:   g.createdBy,
		ActorType: "system",
		TraceID:   "pc-care-repeat",
	})
	if errors.Is(err, domain.ErrTaskAlreadyPlanned) {
		// Someone already planned one of these pens for that day by hand (or a racing tick won).
		// Nothing to do: the next tick reads the latest task per pen again.
		return 0, 0, true, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("pccare repeat: create round: %w", err)
	}
	return len(pens), 0, false, nil
}

// skipRepeat records the skip for every source pen of the group and, when that is new, alerts
// the planner once for the group.
func (s *Service) skipRepeat(ctx context.Context, tenantID string, store ports.RepeatStore, alerter RepeatAlerter, g *repeatGroup, reason string) (int, error) {
	fresh := false
	labels := make([]string, 0, len(g.candidates))
	for _, c := range g.candidates {
		// scale-guard:ignore: bounded by one source round's pens (domain.MaxPensPerRound).
		inserted, err := store.RecordRepeatSkip(ctx, tenantID, c.SourceTaskID, reason, g.next, g.createdBy)
		if err != nil {
			return 0, err
		}
		fresh = fresh || inserted
		labels = append(labels, oploc.OperationalLocation{ShedName: c.ShedName, PartitionLabel: c.PartitionLabel}.Display())
	}
	if fresh && alerter != nil {
		if err := alerter.NotifyRepeatSkipped(ctx, tenantID, RepeatSkip{
			PlannerUserID: g.createdBy, Category: g.category, ParkID: g.parkID, ParkName: g.parkName,
			PenLabels: labels, DueDate: g.next, Reason: reason,
		}); err != nil {
			return 0, err
		}
	}
	return len(g.candidates), nil
}

func intersectIDs(want, have []string) []string {
	ok := map[string]bool{}
	for _, h := range have {
		ok[h] = true
	}
	out := []string{}
	seen := map[string]bool{}
	for _, w := range want {
		if ok[w] && !seen[w] {
			out = append(out, w)
			seen[w] = true
		}
	}
	return out
}
