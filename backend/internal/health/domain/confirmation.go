package domain

import (
	"errors"
	"fmt"
	"sort"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
)

// Confirmation errors. These are the advisory boundary expressed as failures.
var (
	// ErrConfirmNotProposed: the Director named a diagnosis this run did not
	// propose. Diagnosing off the register is a real power they hold, and it is
	// deliberately not this path -- accepting it here would open a course for a
	// disease no evidence supports and no rule named.
	ErrConfirmNotProposed = errors.New("health: diagnosis was not proposed by this run")
	// ErrConfirmNotDiagnosable: the form was rejected, or the animal is outside
	// the register's scope. Neither produces anything confirmable.
	ErrConfirmNotDiagnosable = errors.New("health: run produced no confirmable diagnosis")
)

// ConfirmationPlan is what a confirmation will actually do, resolved before
// anything is written.
type ConfirmationPlan struct {
	// Confirmed are the diagnoses that will open a course, in stable order.
	Confirmed []ConfirmableProblem
	// Declined are proposed diagnoses the Director did not confirm. They are
	// carried through to the response rather than dropped: an override is a
	// decision worth showing the manager, and every override is a rule defect
	// worth reviewing.
	Declined []string
}

// PlanConfirmation resolves the Director's decision against what the engine
// actually proposed.
//
// It is pure, and it is where the confirmation gate lives, so the gate can be
// proven without a database. The rules:
//
//  1. A run that produced no confirmable diagnosis cannot be confirmed at all.
//  2. Every confirmed id must have been proposed. Not a subset check afterwards
//     -- an unproposed id is an error, not something to quietly ignore, because
//     ignoring it would report success for a course that never opened.
//  3. Confirming NOTHING is legal. The Director may reject the whole proposal;
//     that is an override, and the run is still decided.
//
// It deliberately does NOT check whether a treatment card exists. That lookup
// needs the database, and it fails closed at the point of opening the course.
func PlanConfirmation(proposal diagnosis.Proposal, confirmable []ConfirmableProblem, confirmed []string) (ConfirmationPlan, error) {
	if !proposal.Valid || proposal.Scope != diagnosis.ScopeAdult {
		return ConfirmationPlan{}, ErrConfirmNotDiagnosable
	}

	byID := make(map[string]ConfirmableProblem, len(confirmable))
	for _, c := range confirmable {
		byID[c.ID] = c
	}

	chosen := map[string]bool{}
	plan := ConfirmationPlan{}
	for _, id := range confirmed {
		problem, ok := byID[id]
		if !ok {
			return ConfirmationPlan{}, fmt.Errorf("%w: %s", ErrConfirmNotProposed, id)
		}
		if chosen[id] {
			continue
		}
		chosen[id] = true
		plan.Confirmed = append(plan.Confirmed, problem)
	}

	for _, c := range confirmable {
		if !chosen[c.ID] {
			plan.Declined = append(plan.Declined, c.ID)
		}
	}

	// Stable order so a replay writes identical rows. The engine ranks problems
	// by severity for DISPLAY; persistence sorts by id.
	sort.Slice(plan.Confirmed, func(i, j int) bool { return plan.Confirmed[i].ID < plan.Confirmed[j].ID })
	sort.Strings(plan.Declined)
	return plan, nil
}

// CourseShapeFor decides how a confirmed diagnosis is stored and scheduled.
//
// protocolDays is how many days of steps the treatment card actually carries.
// The returned duration is the CLOSURE day count, which is a different question:
// only a Type F course closes on a calendar, so only Type F carries one.
//
// A Type V wound course still snapshots the card's steps -- it just never
// acquires an end date from them.
func CourseShapeFor(exitType string, protocolDays int) (durationDays *int, horizonDays int, err error) {
	if !ValidExitType(exitType) {
		return nil, 0, fmt.Errorf("health: unknown exit type %q", exitType)
	}
	if protocolDays < 1 {
		return nil, 0, fmt.Errorf("health: treatment card carries no days")
	}
	if ClosesOnDayCount(exitType) {
		d := protocolDays
		return &d, protocolDays, nil
	}
	return nil, protocolDays, nil
}
