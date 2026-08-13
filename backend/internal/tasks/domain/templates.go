// Package domain holds the birth/death follow-up workflow engine's pure types and rules
// (docs/decisions/birth-death-workflows.md). Templates are CODE-DEFINED here: a workflow instance
// is stamped out of a template when the tasks module consumes goat.created / goat.exited, and only
// row status/answer/proof state mutates afterwards.
package domain

import (
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Template keys and the module each maps to (module is the mobile list's top-level split).
const (
	TemplateKeyBirthKid    = "birth_kid"
	TemplateKeyBirthMother = "birth_mother"
	TemplateKeyDeath       = "death"

	ModuleBirth = "birth"
	ModuleDeath = "death"
)

// Workflow states.
const (
	WorkflowStateOpen      = "open"
	WorkflowStateCompleted = "completed"
	WorkflowStateCanceled  = "canceled"
)

// Action sections. Both sections are operator-visible and count toward actions_total.
const (
	SectionMain             = "main"
	SectionColostrumSession = "colostrum_session"
)

// Action types.
const (
	ActionTypeQuestion       = "question"
	ActionTypeQuestionSelect = "question_select"
	ActionTypeAction         = "action"
	ActionTypeApproval       = "approval"
)

// Action statuses.
const (
	ActionStatusPending   = "pending"
	ActionStatusInReview  = "in_review"
	ActionStatusCompleted = "completed"
	ActionStatusRework    = "rework"
	ActionStatusCanceled  = "canceled"
)

// Verification wiring constants for the death evidence trail. One item (category death_evidence)
// carries BOTH death videos; an authorized verifier reviews it in the generic Verify queue and the verdict
// events route back on (module, ref_type) exactly like shifting/feed.
const (
	VerificationVerticalCounts        = "counts"
	VerificationModuleCounts          = "counts"
	VerificationCategoryDeathEvidence = "death_evidence"
	VerificationRefTypeDeathSignoff   = "workflow_death_signoff"
	VerificationCategoryBirthEvidence = "birth_evidence"
	VerificationRefTypeBirthSignoff   = "workflow_birth_signoff"
)

// Action keys (stable identifiers; titles/details are the operator-facing copy).
const (
	ActionKeyKidClean       = "kid_clean"
	ActionKeyIodineDipping  = "iodine_dipping"
	ActionKeyFrontTeeth     = "front_teeth_check"
	ActionKeySuckReflex     = "suck_reflex"
	ActionKeyFirstColostrum = "first_colostrum"
	ActionKeyTakeWeight     = "take_weight"
	ActionKeyKidStanding    = "kid_standing"
	ActionKeyTagTheKid      = "tag_the_kid"

	ActionKeyBabiesStillInside = "babies_still_inside"
	ActionKeyMotherLicking     = "mother_licking"
	ActionKeyMothersMedicine   = "mothers_medicine"
	ActionKeyORSWater1         = "ors_water_1"
	ActionKeyMotherEating      = "mother_eating"
	ActionKeyORSWater2         = "ors_water_2"

	ActionKeyDeathVideo      = "death_video"
	ActionKeyPostMortemVideo = "post_mortem_video"
	ActionKeyParkHeadSignoff = "park_head_signoff"
)

// Schedule states when an action is due, anchored to the birth/death moment. Two shapes exist:
//
//   - offset-from-event (EVENT+0 / EVENT+1H / EVENT+6H): due = event moment + Offset;
//   - fixed IST wall-clock time on a business day relative to the event date (the colostrum session
//     strip and "Tag the kid" at EVENT+2D 07:00 IST).
//
// Business-day/IST anchoring is mandatory (AGENTS.md): the fixed shape resolves the event's
// Asia/Kolkata business date first and then applies the wall-clock time in that calendar.
type Schedule struct {
	// Offset applies when AtFixedTime is false: due = eventAt + Offset.
	Offset time.Duration
	// AtFixedTime selects the business-day wall-clock shape below.
	AtFixedTime bool
	// DayOffset is the number of business days after the event date (0 = the event's own date).
	DayOffset int
	// Hour/Minute are the IST wall-clock time on that business day.
	Hour   int
	Minute int
}

// DueAt resolves the schedule against the event moment.
func (s Schedule) DueAt(eventAt time.Time) time.Time {
	if !s.AtFixedTime {
		return eventAt.Add(s.Offset)
	}
	day := biztime.BusinessDayStart(eventAt).AddDate(0, 0, s.DayOffset)
	return time.Date(day.Year(), day.Month(), day.Day(), s.Hour, s.Minute, 0, 0, biztime.DefaultLocation())
}

// ActionTemplate is one step of a template. Title/Detail are operator-facing human copy — never raw
// config tokens.
type ActionTemplate struct {
	Key           string
	Seq           int
	Section       string
	Type          string
	Title         string
	Detail        string
	RequiresVideo bool
	// Options carries the question_select bands (nil for other types).
	Options  []string
	Schedule Schedule
}

// Template is one code-defined workflow shape.
type Template struct {
	Key     string
	Module  string
	Actions []ActionTemplate
}

// OperatorActionCount is the operator card's actions_total: every visible operator step across
// main and scheduled-colostrum sections. Internal approval rows never count.
func (t Template) OperatorActionCount() int {
	n := 0
	for _, a := range t.Actions {
		if a.Type != ActionTypeApproval {
			n++
		}
	}
	return n
}

// colostrumSessionTimes is the five-session IST wall-clock schedule. A birth receives every slot
// whose 15-minute pre-notification window has not started on the birth day, plus all five slots on
// the following day. The immediate 1st Colostrum action is separate from this scheduled series.
var colostrumSessionTimes = []struct {
	Hour, Minute int
	Label        string
}{
	{7, 0, "07:00"},
	{11, 0, "11:00"},
	{15, 0, "15:00"},
	{18, 30, "18:30"},
	{22, 0, "22:00"},
}

const colostrumPreNotify = 15 * time.Minute

type scheduledColostrumSession struct {
	DayOffset int
	Hour      int
	Minute    int
	Label     string
}

// birthColostrumSessions derives the scheduled feeds from the recorded birth moment in IST. A slot
// is eligible only when the kid was born strictly before its pre-notification cutoff. Equality is
// deliberately excluded, matching the legacy Birth/Colostrum workflow.
func birthColostrumSessions(eventAt time.Time) []scheduledColostrumSession {
	birthAt := eventAt.In(biztime.DefaultLocation())
	birthDay := biztime.BusinessDayStart(birthAt)
	sessions := make([]scheduledColostrumSession, 0, len(colostrumSessionTimes)*2)
	for _, session := range colostrumSessionTimes {
		slot := time.Date(
			birthDay.Year(), birthDay.Month(), birthDay.Day(),
			session.Hour, session.Minute, 0, 0, biztime.DefaultLocation(),
		)
		if birthAt.Before(slot.Add(-colostrumPreNotify)) {
			sessions = append(sessions, scheduledColostrumSession{
				DayOffset: 0, Hour: session.Hour, Minute: session.Minute, Label: session.Label,
			})
		}
	}
	for _, session := range colostrumSessionTimes {
		sessions = append(sessions, scheduledColostrumSession{
			DayOffset: 1, Hour: session.Hour, Minute: session.Minute, Label: session.Label,
		})
	}
	return sessions
}

func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

// TemplateBirthKidAt is the kid track of the Delivery Template: seven immediate main steps, the
// birth-time-derived colostrum series, then Tag the kid as the final operator step. The legacy
// TRIGGER_EVENT shifting steps are dropped (shifting is its own gated module).
func TemplateBirthKidAt(eventAt time.Time) Template {
	actions := []ActionTemplate{
		{Key: ActionKeyKidClean, Seq: 1, Section: SectionMain, Type: ActionTypeQuestion,
			Title:  "Is the kid clean?",
			Detail: "Confirm the kid has been cleaned and dried after delivery.", RequiresVideo: true},
		{Key: ActionKeyIodineDipping, Seq: 2, Section: SectionMain, Type: ActionTypeAction,
			Title:  "Iodine dipping of umbilical cord",
			Detail: "Dip the kid's umbilical cord in iodine solution to prevent infection.", RequiresVideo: true},
		{Key: ActionKeyFrontTeeth, Seq: 3, Section: SectionMain, Type: ActionTypeQuestion,
			Title:  "Are the front teeth outside the lower gum?",
			Detail: "Check the kid's mouth: the front teeth should be visible outside the lower gum.", RequiresVideo: true},
		{Key: ActionKeySuckReflex, Seq: 4, Section: SectionMain, Type: ActionTypeQuestion,
			Title:  "Does the kid have a suck reflex?",
			Detail: "Place a clean finger in the kid's mouth and confirm it starts sucking.", RequiresVideo: true},
		{Key: ActionKeyFirstColostrum, Seq: 5, Section: SectionMain, Type: ActionTypeAction,
			Title:         "1st Colostrum",
			Detail:        "Feed the first colostrum and record a video using the in-app camera.",
			RequiresVideo: true},
		{Key: ActionKeyTakeWeight, Seq: 6, Section: SectionMain, Type: ActionTypeQuestion,
			Title:         "Take Weight of Kid",
			Detail:        "Weigh the kid and enter the exact weight in kilograms (kg).",
			RequiresVideo: true},
		{Key: ActionKeyKidStanding, Seq: 7, Section: SectionMain, Type: ActionTypeQuestion,
			Title:    "Is the kid standing?",
			Detail:   "One hour after birth, confirm the kid is standing on its own.",
			Schedule: Schedule{Offset: time.Hour}, RequiresVideo: true},
	}
	for i, session := range birthColostrumSessions(eventAt) {
		dayLabel := "birth day"
		if session.DayOffset == 1 {
			dayLabel = "day after birth"
		}
		actions = append(actions, ActionTemplate{
			Key:           fmt.Sprintf("colostrum_day_%d_%02d%02d", session.DayOffset+1, session.Hour, session.Minute),
			Seq:           8 + i,
			Section:       SectionColostrumSession,
			Type:          ActionTypeAction,
			Title:         ordinal(i+2) + " Colostrum",
			Detail:        "Feed colostrum at the " + session.Label + " session on the " + dayLabel + ".",
			RequiresVideo: true,
			Schedule: Schedule{
				AtFixedTime: true, DayOffset: session.DayOffset, Hour: session.Hour, Minute: session.Minute,
			},
		})
	}
	actions = append(actions, ActionTemplate{
		Key: ActionKeyTagTheKid, Seq: len(actions) + 1, Section: SectionMain, Type: ActionTypeAction,
		Title:         "Tag the kid",
		Detail:        "Scan or enter the permanent RFID, then record one tagging video. The same goat record is retained and its temporary identifier is retired.",
		RequiresVideo: true,
		Schedule:      Schedule{AtFixedTime: true, DayOffset: 2, Hour: 7, Minute: 0},
	})
	return Template{Key: TemplateKeyBirthKid, Module: ModuleBirth, Actions: actions}
}

// TemplateBirthMother is the mother track of the Delivery Template (6 steps), opened once per dam
// and shared by twins through the workflow natural key. The 2nd ORS round is not event-anchored:
// its due time is set to exactly 50 minutes after ORS round 1 is actually completed.
func TemplateBirthMother() Template {
	return Template{Key: TemplateKeyBirthMother, Module: ModuleBirth, Actions: []ActionTemplate{
		{Key: ActionKeyBabiesStillInside, Seq: 1, Section: SectionMain, Type: ActionTypeQuestion,
			Title:         "Are any babies still inside?",
			Detail:        "Check whether the mother is still in labour with another kid inside.",
			RequiresVideo: true},
		{Key: ActionKeyMotherLicking, Seq: 2, Section: SectionMain, Type: ActionTypeQuestion,
			Title:         "Is the mother licking her babies?",
			Detail:        "Confirm the mother has accepted the kids and is licking them clean.",
			RequiresVideo: true},
		{Key: ActionKeyMothersMedicine, Seq: 3, Section: SectionMain, Type: ActionTypeAction,
			Title: "Mother's Medicine",
			Detail: "Chocolate Injection at 1.5 ml SQ\n" +
				"Meloxicam Paracetamol at 4 ml IM\n" +
				"Exapar at 20 ml\n" +
				"Glucoboost at 100 ml mix with 150gms Concentrate",
			RequiresVideo: true},
		{Key: ActionKeyORSWater1, Seq: 4, Section: SectionMain, Type: ActionTypeAction,
			Title:         "ORS water",
			Detail:        "Give the mother ORS water to drink after delivery.",
			RequiresVideo: true},
		{Key: ActionKeyMotherEating, Seq: 5, Section: SectionMain, Type: ActionTypeQuestion,
			Title:         "Is the mother eating?",
			Detail:        "Confirm the mother has started eating after delivery.",
			RequiresVideo: true},
		{Key: ActionKeyORSWater2, Seq: 6, Section: SectionMain, Type: ActionTypeAction,
			Title:         "ORS water (2nd round)",
			Detail:        "Give the mother a second round of ORS water exactly 50 minutes after the first round was given.",
			RequiresVideo: true},
	}}
}

// TemplateDeath is the operator's death evidence trail: exactly two mandatory videos. Admin
// approval and verifier review are workflow state, never additional operator actions.
func TemplateDeath() Template {
	return Template{Key: TemplateKeyDeath, Module: ModuleDeath, Actions: []ActionTemplate{
		{Key: ActionKeyDeathVideo, Seq: 1, Section: SectionMain, Type: ActionTypeAction,
			Title:         "Record death video",
			Detail:        "Record the dead animal with its ear tag clearly visible using the in-app camera.",
			RequiresVideo: true},
		{Key: ActionKeyPostMortemVideo, Seq: 2, Section: SectionMain, Type: ActionTypeAction,
			Title:         "Record post-mortem video",
			Detail:        "Record with the timestamp visible. Show the carcass and the post-mortem site in one continuous take using the in-app camera.",
			RequiresVideo: true},
	}}
}

// TemplateByKeyAt resolves a template key to its code-defined template using the event moment for
// birth-time-derived scheduling.
func TemplateByKeyAt(key string, eventAt time.Time) (Template, bool) {
	switch key {
	case TemplateKeyBirthKid:
		return TemplateBirthKidAt(eventAt), true
	case TemplateKeyBirthMother:
		return TemplateBirthMother(), true
	case TemplateKeyDeath:
		return TemplateDeath(), true
	}
	return Template{}, false
}

// ModuleForTemplate derives the list module for a template key ("" for unknown keys).
func ModuleForTemplate(templateKey string) string {
	switch templateKey {
	case TemplateKeyBirthKid, TemplateKeyBirthMother:
		return ModuleBirth
	case TemplateKeyDeath:
		return ModuleDeath
	}
	return ""
}
