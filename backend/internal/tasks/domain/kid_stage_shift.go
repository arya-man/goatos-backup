package domain

import (
	"errors"
	"strings"
	"time"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
)

// KID STAGE SHIFT TASKS (maintainer decision 2026-09-30, docs/decisions/kid-stage-shift-tasks.md).
//
// A birth owes the park head two moves: the litter from K0 to K1 24 hours after it was born, and
// from K1 to K2 seven days after it reached K1. Both are STEPS of ONE workflow per litter, opened
// from the published Birth SOP's `birth_litter` track -- so the timing, the owner and the stage
// each step moves the kids to are authored on /counts/sops, and a later rule (K2 -> K3 after N
// days) is a new step, never a code change.
//
// A shift step is ENGINE-COMPLETED, never a tap. It opens Raise shifting (growth) for the kids
// still waiting, and closes itself the moment no live kid of the litter is left on a stage BEFORE
// its target on the growth ladder -- read from the herd register when a kid's stage changes, so a
// task cannot read "done" while a kid still sits in K0. Its completion instant is the moment the
// LAST kid reached the target, which is what the next step's "7 days after" counts from.
const (
	// TemplateKeyBirthLitter is the litter's workflow, keyed on the birth event
	// (subject_ref_id = goat_births.birth_event_id) and carrying no single animal.
	TemplateKeyBirthLitter = "birth_litter"
	// EngineHookShiftKidsStage is the hook of a litter shift step: its authored target_stage says
	// which stage the kids must reach.
	EngineHookShiftKidsStage = "shift_kids_stage"
)

// ErrKidShiftPending refuses a by-hand completion of a litter shift step: the kids are moved on
// Raise shifting and the step completes when the herd register shows them there.
var ErrKidShiftPending = errors.New("tasks: shift the kids on Raise shifting; this step completes when every kid has reached the stage")

// LitterKid is one kid of a litter as the shift steps judge it: its current stage and whether it
// is still alive on the farm (an exited kid owes no move).
type LitterKid struct {
	GoatID string
	Stage  string
	Alive  bool
	// StageSince is when the kid entered its CURRENT stage, from the herd register's own history
	// (the latest goat.stage_changed naming that stage); nil when the register holds none (a kid
	// seeded on its stage). It is what a completed step is stamped with, so the next step's
	// "N days after" counts from the day the litter really got there -- for a litter backfilled
	// long after it moved, and for an event processed late, alike.
	StageSince *time.Time
}

// LitterShiftOutcome is what one shift step's condition reads off the litter.
type LitterShiftOutcome struct {
	// Waiting are the live kids still on a stage before the target -- the work.
	Waiting int
	// Reached are the live kids on the target or past it on the ladder.
	Reached int
	// Live is every live kid of the litter.
	Live int
}

// Done reports the step's work finished: at least one live kid reached the target and none is
// still waiting. A litter whose live kids are all off the ladder (every one in ICU) is NOT done --
// nobody reached the stage -- and stays owed until they return or leave the farm.
func (o LitterShiftOutcome) Done() bool { return o.Reached > 0 && o.Waiting == 0 }

// JudgeLitterShift classifies a litter against one target stage using the growth ladder the
// growth raise itself obeys (counts/domain.GrowthStagesBefore), so the task and the raise can
// never disagree about which stage comes next. A kid on a stage off the ladder (a clinical pen tag
// such as ICU-Kid) is live but neither waiting nor reached.
func JudgeLitterShift(kids []LitterKid, target string) LitterShiftOutcome {
	var out LitterShiftOutcome
	target = strings.TrimSpace(target)
	before := countsdomain.GrowthStagesBefore(target)
	for _, k := range kids {
		if !k.Alive {
			continue
		}
		out.Live++
		stage := strings.TrimSpace(k.Stage)
		switch {
		case stage == "":
		case containsFold(before, stage):
			out.Waiting++
		case strings.EqualFold(stage, target), containsFold(countsdomain.GrowthStagesBefore(stage), target):
			out.Reached++
		}
	}
	return out
}

// KidsWaitingForShift lists the live kids still on a stage before target -- the animals the
// Raise shifting screen is opened with.
func KidsWaitingForShift(kids []LitterKid, target string) []LitterKid {
	before := countsdomain.GrowthStagesBefore(strings.TrimSpace(target))
	var out []LitterKid
	for _, k := range kids {
		if k.Alive && containsFold(before, strings.TrimSpace(k.Stage)) {
			out = append(out, k)
		}
	}
	return out
}

func containsFold(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}

// ApplyLitterShift reconciles a litter workflow's shift steps with the herd register, in step
// order, and returns the rows it changed. It is the ONLY writer of those steps' completion.
//
//   - A pending shift step whose prerequisites are complete and whose litter has reached its
//     target (JudgeLitterShift(...).Done) completes AT `at` -- the moment the herd register moved
//     the last kid -- and every sibling timed "after" it gets its due time from that instant, so
//     "7 days after K1" counts from the day the litter actually reached K1.
//   - A litter with NO live kid (every kid died, was sold or culled) owes no move: every
//     unfinished shift step is canceled and the workflow with it (cancelWorkflow = true).
//   - A step already completed stays completed: a kid moving on (or back) later does not reopen a
//     move the park head already made.
//
// The completion's idempotency key is the action id, so a redelivered stage event replays as a
// no-op.
func ApplyLitterShift(actions []WorkflowAction, kids []LitterKid, at time.Time) (changed []WorkflowAction, cancelWorkflow bool) {
	live := 0
	for _, k := range kids {
		if k.Alive {
			live++
		}
	}
	if len(kids) > 0 && live == 0 {
		for i := range actions {
			a := actions[i]
			if !a.HasHook(EngineHookShiftKidsStage) || !stepUnfinished(a.Status) {
				continue
			}
			a.Status = ActionStatusCanceled
			a.RowVersion++
			actions[i] = a
			changed = append(changed, a)
		}
		return changed, true
	}
	for i := range actions {
		a := actions[i]
		if !a.HasHook(EngineHookShiftKidsStage) || a.Status != ActionStatusPending {
			continue
		}
		if !prerequisitesComplete(a, actions) {
			continue
		}
		if !JudgeLitterShift(kids, a.TargetStage).Done() {
			continue
		}
		doneAt := litterReachedAt(kids, a.TargetStage, at)
		key := "litter-shift:" + a.ActionID
		updated, replay, err := ApplyComplete(a, CompleteActionCommand{
			TenantID: a.TenantID, WorkflowID: a.WorkflowID, ActionID: a.ActionID,
			CompletedAt: doneAt, IdempotencyKey: key, RequestFingerprint: key,
		})
		if err != nil || replay {
			continue
		}
		actions[i] = updated
		changed = append(changed, updated)
		for j := range actions {
			dep := actions[j]
			if dep.AfterActionKey != updated.ActionKey || dep.Status != ActionStatusPending {
				continue
			}
			due := doneAt.Add(time.Duration(dep.AfterOffsetSeconds) * time.Second)
			dep.DueAt = &due
			dep.RowVersion++
			actions[j] = dep
			changed = appendOrReplace(changed, dep)
		}
	}
	return changed, false
}

// litterReachedAt is the moment the litter reached target: the LATEST stage entry among the live
// kids that reached it, read from the herd register. When any of them has no recorded entry, the
// caller's instant stands (the event being handled, or the backfill's run) -- never a guess.
func litterReachedAt(kids []LitterKid, target string, fallback time.Time) time.Time {
	var latest time.Time
	before := countsdomain.GrowthStagesBefore(strings.TrimSpace(target))
	for _, k := range kids {
		if !k.Alive || k.Stage == "" || containsFold(before, k.Stage) {
			continue
		}
		reached := strings.EqualFold(k.Stage, target) || containsFold(countsdomain.GrowthStagesBefore(k.Stage), target)
		if !reached {
			continue
		}
		if k.StageSince == nil {
			return fallback
		}
		if k.StageSince.After(latest) {
			latest = *k.StageSince
		}
	}
	if latest.IsZero() {
		return fallback
	}
	return latest
}

func stepUnfinished(status string) bool {
	return status == ActionStatusPending || status == ActionStatusInReview || status == ActionStatusRework
}

func prerequisitesComplete(a WorkflowAction, actions []WorkflowAction) bool {
	for _, req := range a.RequiresKeys {
		for _, b := range actions {
			if b.ActionKey == req && b.Status != ActionStatusCompleted {
				return false
			}
		}
	}
	return true
}

func appendOrReplace(list []WorkflowAction, a WorkflowAction) []WorkflowAction {
	for i := range list {
		if list[i].ActionID == a.ActionID {
			list[i] = a
			return list
		}
	}
	return append(list, a)
}
