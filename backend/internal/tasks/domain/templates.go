// Package domain holds the birth/death follow-up workflow engine's pure types and rules
// (docs/decisions/birth-death-workflows.md). Templates are CODE-DEFINED here: a workflow instance
// is stamped out of a template when the tasks module consumes goat.created / goat.exited, and only
// row status/answer/proof state mutates afterwards.
package domain

import (
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

// Action sections. Colostrum session rows deliberately do NOT count toward actions_total.
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

// MainActionCount is the operator card's actions_total: MAIN-section operator steps only. The
// colostrum session strip and internal approval rows deliberately do not count.
func (t Template) MainActionCount() int {
	n := 0
	for _, a := range t.Actions {
		if a.Section == SectionMain && a.Type != ActionTypeApproval {
			n++
		}
	}
	return n
}

// takeWeightBands are the question_select weight bands for "Take Weight of Kid".
var takeWeightBands = []string{
	"Below 2.0 kg",
	"2.0 – 2.5 kg",
	"2.5 – 3.0 kg",
	"3.0 – 3.5 kg",
	"Above 3.5 kg",
}

// colostrumSessionTimes is the fixed day-one 5-session strip (IST wall-clock on the birth date).
// The decaying multi-day series / per-farm session config is explicitly out of scope for this slice.
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

// TemplateBirthKid is the kid track of the Delivery Template: 8 main steps plus the 5-session
// colostrum strip. The legacy TRIGGER_EVENT shifting steps are dropped (shifting is its own gated
// module).
func TemplateBirthKid() Template {
	actions := []ActionTemplate{
		{Key: ActionKeyKidClean, Seq: 1, Section: SectionMain, Type: ActionTypeQuestion,
			Title:  "Is the kid clean?",
			Detail: "Confirm the kid has been cleaned and dried after delivery."},
		{Key: ActionKeyIodineDipping, Seq: 2, Section: SectionMain, Type: ActionTypeAction,
			Title:  "Iodine dipping of umbilical cord",
			Detail: "Dip the kid's umbilical cord in iodine solution to prevent infection."},
		{Key: ActionKeyFrontTeeth, Seq: 3, Section: SectionMain, Type: ActionTypeQuestion,
			Title:  "Are the front teeth outside the lower gum?",
			Detail: "Check the kid's mouth: the front teeth should be visible outside the lower gum."},
		{Key: ActionKeySuckReflex, Seq: 4, Section: SectionMain, Type: ActionTypeQuestion,
			Title:  "Does the kid have a suck reflex?",
			Detail: "Place a clean finger in the kid's mouth and confirm it starts sucking."},
		{Key: ActionKeyFirstColostrum, Seq: 5, Section: SectionMain, Type: ActionTypeAction,
			Title:         "1st Colostrum",
			Detail:        "Feed the first colostrum and record a video using the in-app camera.",
			RequiresVideo: true},
		{Key: ActionKeyTakeWeight, Seq: 6, Section: SectionMain, Type: ActionTypeQuestionSelect,
			Title:   "Take Weight of Kid",
			Detail:  "Weigh the kid and select the weight band.",
			Options: takeWeightBands},
		{Key: ActionKeyKidStanding, Seq: 7, Section: SectionMain, Type: ActionTypeQuestion,
			Title:    "Is the kid standing?",
			Detail:   "One hour after birth, confirm the kid is standing on its own.",
			Schedule: Schedule{Offset: time.Hour}},
		{Key: ActionKeyTagTheKid, Seq: 8, Section: SectionMain, Type: ActionTypeAction,
			Title:    "Tag the kid",
			Detail:   "Assign the permanent RFID from the Awaiting RFID list. This step completes automatically when the permanent tag is assigned.",
			Schedule: Schedule{AtFixedTime: true, DayOffset: 2, Hour: 7, Minute: 0}},
	}
	for i, session := range colostrumSessionTimes {
		actions = append(actions, ActionTemplate{
			Key:      "colostrum_session_" + string(rune('1'+i)),
			Seq:      9 + i,
			Section:  SectionColostrumSession,
			Type:     ActionTypeAction,
			Title:    "Colostrum session · " + session.Label,
			Detail:   "Feed colostrum at the " + session.Label + " session on the birth day.",
			Schedule: Schedule{AtFixedTime: true, DayOffset: 0, Hour: session.Hour, Minute: session.Minute},
		})
	}
	return Template{Key: TemplateKeyBirthKid, Module: ModuleBirth, Actions: actions}
}

// TemplateBirthMother is the mother track of the Delivery Template (6 steps), opened once per dam
// and shared by twins through the workflow natural key. The 2nd ORS round is scheduled EVENT+6H in
// this slice — the legacy FUNC_ORS_2 runtime-conditional scheduler is out of scope and this fixed
// offset is the recorded simplification.
func TemplateBirthMother() Template {
	return Template{Key: TemplateKeyBirthMother, Module: ModuleBirth, Actions: []ActionTemplate{
		{Key: ActionKeyBabiesStillInside, Seq: 1, Section: SectionMain, Type: ActionTypeQuestion,
			Title:  "Are any babies still inside?",
			Detail: "Check whether the mother is still in labour with another kid inside."},
		{Key: ActionKeyMotherLicking, Seq: 2, Section: SectionMain, Type: ActionTypeQuestion,
			Title:  "Is the mother licking her babies?",
			Detail: "Confirm the mother has accepted the kids and is licking them clean."},
		{Key: ActionKeyMothersMedicine, Seq: 3, Section: SectionMain, Type: ActionTypeAction,
			Title:  "Mother's Medicine",
			Detail: "Administer the prescribed post-delivery medicine course to the mother."},
		{Key: ActionKeyORSWater1, Seq: 4, Section: SectionMain, Type: ActionTypeAction,
			Title:  "ORS water",
			Detail: "Give the mother ORS water to drink after delivery."},
		{Key: ActionKeyMotherEating, Seq: 5, Section: SectionMain, Type: ActionTypeQuestion,
			Title:  "Is the mother eating?",
			Detail: "Confirm the mother has started eating after delivery."},
		{Key: ActionKeyORSWater2, Seq: 6, Section: SectionMain, Type: ActionTypeAction,
			Title:    "ORS water (2nd round)",
			Detail:   "Give the mother a second round of ORS water.",
			Schedule: Schedule{Offset: 6 * time.Hour}},
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

// TemplateByKey resolves a template key to its code-defined template.
func TemplateByKey(key string) (Template, bool) {
	switch key {
	case TemplateKeyBirthKid:
		return TemplateBirthKid(), true
	case TemplateKeyBirthMother:
		return TemplateBirthMother(), true
	case TemplateKeyDeath:
		return TemplateDeath(), true
	}
	return Template{}, false
}

// ModuleForTemplate derives the list module for a template key ("" for unknown keys).
func ModuleForTemplate(templateKey string) string {
	if t, ok := TemplateByKey(templateKey); ok {
		return t.Module
	}
	return ""
}
