package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// SOP-DRIVEN FOLLOW-UP (maintainer decision 2026-09-13, docs/decisions/sop-driven-herd-operations.md).
//
// A workflow's steps -- which questions the operator answers, which proof each step needs, and
// WHEN each step is due -- are no longer Go constants. They are compiled from the `follow_up`
// section of the PUBLISHED `sop_versions.form_dsl` for the module's SOP code (`counts.birth`,
// `counts.death`, `counts.shifting`, `counts.reconcile`), pinned per workflow at open. The
// maintainer edits them on /counts/sops; the next workflow opened uses the new version and the
// phone renders the stamped rows verbatim, so a changed question needs no APK release.
//
// `TemplateBirthKidAt` / `TemplateBirthMother` / `TemplateDeath` remain in this package ONLY as
// the golden oracle for the seeded v1 documents (sop_followup_golden_test.go): the seeded DSL
// must compile byte-for-byte to what the code used to stamp, so day-one behaviour did not move.
// Nothing in the write path calls them any more.
//
// Engine semantics are NOT free text. A step that the server has to act on (numeric kilograms,
// the RFID promotion, the kid-pen fallback, the colostrum lens, the dependency-timed ORS round)
// names a registry TASK TYPE whose `engine_hook` selects that behaviour; the step KEY is still
// what the hook is matched on, so a renamed title never detaches a behaviour. The registry is
// seeded by migration and edited on /config in a later phase.

// FollowUpSchemaVersion is the schema tag every follow_up section must carry.
const FollowUpSchemaVersion = "goatos.sop-followup.v1"

// Schedule kinds accepted in a follow_up step.
const (
	ScheduleKindImmediately = "immediately"
	ScheduleKindAfterEvent  = "after_event"
	ScheduleKindAtFixedTime = "at_fixed_time"
	ScheduleKindSeries      = "series"
	ScheduleKindAfterStep   = "after_step"
)

// Answer kinds a step can ask for. Maps onto the persisted `answer_type` column and, for the
// phone's existing renderer, onto the legacy action_type (question / question_select / action).
const (
	AnswerKindNone        = "none"
	AnswerKindYesNo       = "yes_no"
	AnswerKindSelect      = "select"
	AnswerKindMultiSelect = "multiselect"
	AnswerKindNumber      = "number"
	AnswerKindText        = "text"
)

// Engine hooks a task type may declare. Matched on the STEP KEY at runtime so the hook survives a
// relabel; the hook name on the registry row is what the builder shows the author.
const (
	EngineHookNone         = ""
	EngineHookWeighKg      = "weigh_kg"       // take_weight: numeric kilograms answer
	EngineHookTagKid       = "tag_kid"        // tag_the_kid: RFID promotion before completion
	EngineHookRecordPen    = "record_pen"     // record_shed: sets the park's kid pen
	EngineHookColostrum    = "colostrum_feed" // feeds counted by the Colostrum lens
	EngineHookDeathVideo   = "death_evidence" // death videos released to Verify on approval
	EngineHookReturnAnimal = "return_to_pen"  // reconcile: the animal walked back
)

// Step condition tokens (a step included only when the opening context says so).
const (
	StepWhenAlways           = ""
	StepWhenKidPenUnresolved = "kid_pen_unresolved"
)

// Sentinel errors for a follow_up section that cannot be compiled. Publishing validates the same
// rules (sop/app validateFollowUp) so an unusable document never becomes the published version.
var (
	ErrFollowUpMissing      = errors.New("tasks: SOP version has no follow_up section")
	ErrFollowUpTrackMissing = errors.New("tasks: SOP follow_up has no track for this template")
	ErrFollowUpInvalid      = errors.New("tasks: SOP follow_up is invalid")
)

// FollowUpDSL is the typed form of `form_dsl.follow_up`.
type FollowUpDSL struct {
	SchemaVersion string           `json:"schema_version"`
	Tracks        []FollowUpTrack  `json:"tracks"`
	TaskTypes     []FollowUpTaskTy `json:"-"`
}

// FollowUpTrack is one operator track (kid, mother, death, reconcile, shifting completion). Its
// key is the workflow template_key the tasks module opens for it.
type FollowUpTrack struct {
	Key     string         `json:"key"`
	Module  string         `json:"module"`
	Label   string         `json:"label"`
	Subject string         `json:"subject"`
	Steps   []FollowUpStep `json:"steps"`
}

// FollowUpStep is one authored step. A `series` schedule expands into several rows.
type FollowUpStep struct {
	Key          string           `json:"key"`
	TaskType     string           `json:"task_type"`
	Title        string           `json:"title"`
	TitlePattern string           `json:"title_pattern,omitempty"`
	Detail       string           `json:"detail"`
	Section      string           `json:"section,omitempty"`
	Answer       string           `json:"answer,omitempty"`
	Options      []string         `json:"options,omitempty"`
	Proof        FollowUpProof    `json:"proof"`
	Schedule     FollowUpSchedule `json:"schedule"`
	HardTimeGate bool             `json:"hard_time_gate,omitempty"`
	WaitForAll   bool             `json:"wait_for_all,omitempty"`
	Requires     []string         `json:"requires,omitempty"`
	When         string           `json:"when,omitempty"`
	// WhenAnswer is an ANSWER-DRIVEN branch (maintainer decision 2026-09-18, SOP studio phase 2):
	// the step runs only when an earlier question's answer satisfies the condition, and is SKIPPED
	// -- off this path, never owed -- when it does not. Nil = the step always runs.
	WhenAnswer *AnswerCondition `json:"when_answer,omitempty"`
}

// AnswerCondition is one branch condition: the earlier question step, the comparison and the
// value(s). The condition is resolved the moment the question is answered, inside the answer's
// own transaction, so a workflow's steps are stamped in full at open (the operator sees what may
// come) and the branch not taken is marked skipped rather than left owed.
type AnswerCondition struct {
	Step  string   `json:"step"`
	Op    string   `json:"op"`
	Value []string `json:"value"`
}

// Answer-condition operators. eq/ne/in/not_in compare answer text (a pick-many answer is the
// `|`-joined list and matches when ANY picked value does); gt/gte/lt/lte compare numbers.
const (
	AnswerOpEq    = "eq"
	AnswerOpNe    = "ne"
	AnswerOpIn    = "in"
	AnswerOpNotIn = "not_in"
	AnswerOpGt    = "gt"
	AnswerOpGte   = "gte"
	AnswerOpLt    = "lt"
	AnswerOpLte   = "lte"
)

// Satisfied evaluates the condition against a recorded answer.
func (c AnswerCondition) Satisfied(answer string) bool {
	answer = strings.TrimSpace(answer)
	switch c.Op {
	case AnswerOpEq, AnswerOpIn:
		return answerMatchesAny(answer, c.Value)
	case AnswerOpNe, AnswerOpNotIn:
		return !answerMatchesAny(answer, c.Value)
	case AnswerOpGt, AnswerOpGte, AnswerOpLt, AnswerOpLte:
		if len(c.Value) == 0 {
			return false
		}
		got, err := strconv.ParseFloat(answer, 64)
		if err != nil {
			return false
		}
		want, err := strconv.ParseFloat(strings.TrimSpace(c.Value[0]), 64)
		if err != nil {
			return false
		}
		switch c.Op {
		case AnswerOpGt:
			return got > want
		case AnswerOpGte:
			return got >= want
		case AnswerOpLt:
			return got < want
		default:
			return got <= want
		}
	}
	return false
}

func answerMatchesAny(answer string, values []string) bool {
	picked := strings.Split(answer, "|")
	for _, v := range values {
		v = strings.TrimSpace(v)
		for _, p := range picked {
			if strings.EqualFold(strings.TrimSpace(p), v) {
				return true
			}
		}
	}
	return false
}

// FollowUpProof is what evidence a step needs before it counts as done.
type FollowUpProof struct {
	Video int `json:"video,omitempty"`
	Photo int `json:"photo,omitempty"`
}

// FollowUpSchedule is when a step is due, relative to the workflow's event moment.
type FollowUpSchedule struct {
	Kind            string   `json:"kind"`
	OffsetMinutes   int      `json:"offset_minutes,omitempty"`
	DayOffset       int      `json:"day_offset,omitempty"`
	Time            string   `json:"time,omitempty"`
	Times           []string `json:"times,omitempty"`
	Days            int      `json:"days,omitempty"`
	PreNotifyMinute int      `json:"pre_notify_minutes,omitempty"`
	Step            string   `json:"step,omitempty"`
	KeyPattern      string   `json:"key_pattern,omitempty"`
	OrdinalStart    int      `json:"ordinal_start,omitempty"`
	// Series basis (maintainer decision 2026-09-14): "fixed_times" (default) runs the rounds at
	// the authored wall-clock times for `days` days, skipping the event-day slots already past --
	// a 15:00 birth loses the morning rounds. "from_event" runs `count` rounds every
	// `interval_minutes` FROM THE EVENT TIME, so every animal gets the same number of rounds
	// however late in the day it was born -- but a round can land at 02:00. "next_sessions" is
	// the farm's answer to both: the authored wall-clock session times, and `count` rounds taken
	// from the NEXT session after the event onward, spilling into following days, so a 15:00
	// birth still gets ten feeds and none of them at midnight. Authored per step on /counts/sops.
	Basis           string `json:"basis,omitempty"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	Count           int    `json:"count,omitempty"`
}

const (
	SeriesBasisFixedTimes   = "fixed_times"
	SeriesBasisFromEvent    = "from_event"
	SeriesBasisNextSessions = "next_sessions"
	// seriesMaxRounds bounds an authored series so a typo cannot stamp thousands of rows.
	seriesMaxRounds = 100
)

// FollowUpTaskTy is one Task Type Registry row as the compiler needs it.
type FollowUpTaskTy struct {
	Key        string
	AnswerKind string
	EngineHook string
	// ActionKind is the legacy action_type the phone renders: question / question_select / action.
	ActionKind string
}

// CompileOptions carries the per-opening context a step condition can read.
type CompileOptions struct {
	EventAt time.Time
	// NeedsShedPlacement includes steps conditioned on `kid_pen_unresolved`.
	NeedsShedPlacement bool
}

// ParseFollowUp extracts and type-checks the follow_up section of a form_dsl document.
func ParseFollowUp(formDSL map[string]any) (FollowUpDSL, error) {
	raw, ok := formDSL["follow_up"]
	if !ok || raw == nil {
		return FollowUpDSL{}, ErrFollowUpMissing
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return FollowUpDSL{}, fmt.Errorf("%w: %v", ErrFollowUpInvalid, err)
	}
	var out FollowUpDSL
	if err := json.Unmarshal(encoded, &out); err != nil {
		return FollowUpDSL{}, fmt.Errorf("%w: %v", ErrFollowUpInvalid, err)
	}
	if out.SchemaVersion != FollowUpSchemaVersion {
		return FollowUpDSL{}, fmt.Errorf("%w: schema_version %q", ErrFollowUpInvalid, out.SchemaVersion)
	}
	return out, nil
}

// Track returns the track with the given key.
func (d FollowUpDSL) Track(key string) (FollowUpTrack, bool) {
	for _, t := range d.Tracks {
		if t.Key == key {
			return t, true
		}
	}
	return FollowUpTrack{}, false
}

// ValidateFollowUp is the publish-time contract: every error here is a document the engine
// would refuse to open a workflow from. The same rules run at open (CompileTrack) so a document
// published before a rule tightened still fails closed rather than stamping garbage.
func ValidateFollowUp(d FollowUpDSL, taskTypes map[string]FollowUpTaskTy) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if len(d.Tracks) == 0 {
		add("follow_up.tracks: at least one track is required")
	}
	seenTracks := map[string]struct{}{}
	for ti, t := range d.Tracks {
		p := fmt.Sprintf("follow_up.tracks.%d", ti)
		if strings.TrimSpace(t.Key) == "" {
			add("%s.key: required", p)
		}
		if _, dup := seenTracks[t.Key]; dup {
			add("%s.key: duplicate track %q", p, t.Key)
		}
		seenTracks[t.Key] = struct{}{}
		if strings.TrimSpace(t.Module) == "" {
			add("%s.module: required", p)
		}
		if len(t.Steps) == 0 {
			add("%s.steps: at least one step is required", p)
		}
		seenSteps := map[string]struct{}{}
		answerKinds := map[string]stepAnswer{}
		for si, s := range t.Steps {
			sp := fmt.Sprintf("%s.steps.%d", p, si)
			if strings.TrimSpace(s.Key) == "" {
				add("%s.key: required", sp)
			}
			if _, dup := seenSteps[s.Key]; dup {
				add("%s.key: duplicate step %q", sp, s.Key)
			}
			seenSteps[s.Key] = struct{}{}
			tt, ok := taskTypes[s.TaskType]
			if !ok {
				add("%s.task_type: %q is not a registered task type", sp, s.TaskType)
			}
			if strings.TrimSpace(s.Title) == "" && strings.TrimSpace(s.TitlePattern) == "" {
				add("%s.title: required", sp)
			}
			if s.Proof.Video < 0 || s.Proof.Photo < 0 {
				add("%s.proof: counts must not be negative", sp)
			}
			switch s.Section {
			case "", SectionMain, SectionColostrumSession:
			default:
				add("%s.section: %q is not a section (main or colostrum_session)", sp, s.Section)
			}
			answer := s.Answer
			if answer == "" && ok {
				answer = tt.AnswerKind
			}
			switch answer {
			case AnswerKindNone, AnswerKindYesNo, AnswerKindNumber, AnswerKindText:
			case AnswerKindSelect, AnswerKindMultiSelect:
				if len(s.Options) == 0 {
					add("%s.options: a %s step needs at least one option", sp, answer)
				}
			default:
				add("%s.answer: %q is not an answer kind", sp, answer)
			}
			switch s.Schedule.Kind {
			case ScheduleKindImmediately, "":
			case ScheduleKindAfterEvent:
				if s.Schedule.OffsetMinutes < 0 {
					add("%s.schedule.offset_minutes: must not be negative", sp)
				}
			case ScheduleKindAtFixedTime:
				if _, _, err := parseWallClock(s.Schedule.Time); err != nil {
					add("%s.schedule.time: %v", sp, err)
				}
				if s.Schedule.DayOffset < 0 {
					add("%s.schedule.day_offset: must not be negative", sp)
				}
			case ScheduleKindSeries:
				switch s.Schedule.Basis {
				case SeriesBasisFromEvent:
					if s.Schedule.IntervalMinutes <= 0 {
						add("%s.schedule.interval_minutes: must be positive for a series from the event", sp)
					}
					if s.Schedule.Count < 1 || s.Schedule.Count > seriesMaxRounds {
						add("%s.schedule.count: a series from the event needs 1..%d rounds", sp, seriesMaxRounds)
					}
					if !strings.Contains(s.Schedule.KeyPattern, "{n}") {
						add("%s.schedule.key_pattern: a series from the event must carry {n} so every round gets its own key", sp)
					}
				case SeriesBasisNextSessions:
					if len(s.Schedule.Times) == 0 {
						add("%s.schedule.times: a series needs at least one time", sp)
					}
					for _, tm := range s.Schedule.Times {
						if _, _, err := parseWallClock(tm); err != nil {
							add("%s.schedule.times: %v", sp, err)
						}
					}
					if s.Schedule.Count < 1 || s.Schedule.Count > seriesMaxRounds {
						add("%s.schedule.count: a series of next sessions needs 1..%d rounds", sp, seriesMaxRounds)
					}
				case SeriesBasisFixedTimes, "":
					if len(s.Schedule.Times) == 0 {
						add("%s.schedule.times: a series needs at least one time", sp)
					}
					for _, tm := range s.Schedule.Times {
						if _, _, err := parseWallClock(tm); err != nil {
							add("%s.schedule.times: %v", sp, err)
						}
					}
					if s.Schedule.Days < 1 {
						add("%s.schedule.days: a series needs at least one day", sp)
					}
					if len(s.Schedule.Times)*s.Schedule.Days > seriesMaxRounds {
						add("%s.schedule: a series may run at most %d rounds", sp, seriesMaxRounds)
					}
				default:
					add("%s.schedule.basis: %q is not a series basis", sp, s.Schedule.Basis)
				}
				if strings.TrimSpace(s.Schedule.KeyPattern) == "" {
					add("%s.schedule.key_pattern: required for a series", sp)
				}
			case ScheduleKindAfterStep:
				if _, known := seenSteps[s.Schedule.Step]; !known || s.Schedule.Step == s.Key {
					add("%s.schedule.step: must name an EARLIER step in the same track", sp)
				}
				if s.Schedule.OffsetMinutes <= 0 {
					add("%s.schedule.offset_minutes: must be positive", sp)
				}
			default:
				add("%s.schedule.kind: %q is not a schedule kind", sp, s.Schedule.Kind)
			}
			for _, req := range s.Requires {
				if _, known := seenSteps[req]; !known || req == s.Key {
					add("%s.requires: %q must name an EARLIER step in the same track", sp, req)
				}
			}
			switch s.When {
			case StepWhenAlways, StepWhenKidPenUnresolved:
			default:
				add("%s.when: %q is not a step condition", sp, s.When)
			}
			if c := s.WhenAnswer; c != nil {
				for _, problem := range validateAnswerCondition(*c, s.Key, answerKinds) {
					add("%s.when_answer: %s", sp, problem)
				}
			}
			answerKinds[s.Key] = answerKindOf(s, taskTypes)
		}
	}
	return problems
}

// answerKindOf is the answer kind a step records: its own override, else its task type's.
func answerKindOf(s FollowUpStep, taskTypes map[string]FollowUpTaskTy) stepAnswer {
	kind := s.Answer
	if kind == "" {
		if tt, ok := taskTypes[s.TaskType]; ok {
			kind = tt.AnswerKind
		}
	}
	return stepAnswer{kind: kind, options: s.Options}
}

type stepAnswer struct {
	kind    string
	options []string
}

// validateAnswerCondition checks a branch condition against the EARLIER steps of its track: the
// question must exist before this step and record an answer; the operator must fit the answer
// kind; every value must be one the question can produce (yes/no, a listed option, a number).
func validateAnswerCondition(c AnswerCondition, stepKey string, earlier map[string]stepAnswer) []string {
	var problems []string
	q, ok := earlier[c.Step]
	switch {
	case c.Step == "" || c.Step == stepKey || !ok:
		return []string{fmt.Sprintf("step %q must name an EARLIER question step in the same track", c.Step)}
	case q.kind == AnswerKindNone || q.kind == "":
		return []string{fmt.Sprintf("step %q records no answer to branch on", c.Step)}
	}
	if len(c.Value) == 0 {
		problems = append(problems, "a value is required")
	}
	numeric := c.Op == AnswerOpGt || c.Op == AnswerOpGte || c.Op == AnswerOpLt || c.Op == AnswerOpLte
	switch c.Op {
	case AnswerOpEq, AnswerOpNe, AnswerOpIn, AnswerOpNotIn:
		if q.kind == AnswerKindNumber {
			for _, v := range c.Value {
				if _, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err != nil {
					problems = append(problems, fmt.Sprintf("%q is not a number", v))
				}
			}
		}
	case AnswerOpGt, AnswerOpGte, AnswerOpLt, AnswerOpLte:
		if q.kind != AnswerKindNumber {
			problems = append(problems, fmt.Sprintf("%s compares numbers; step %q answers %s", c.Op, c.Step, q.kind))
		}
		if len(c.Value) > 1 {
			problems = append(problems, "a number comparison takes one value")
		}
		for _, v := range c.Value {
			if _, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err != nil {
				problems = append(problems, fmt.Sprintf("%q is not a number", v))
			}
		}
	default:
		return append(problems, fmt.Sprintf("%q is not a comparison", c.Op))
	}
	if numeric {
		return problems
	}
	switch q.kind {
	case AnswerKindYesNo:
		for _, v := range c.Value {
			if lv := strings.ToLower(strings.TrimSpace(v)); lv != "yes" && lv != "no" {
				problems = append(problems, fmt.Sprintf("%q is not yes or no", v))
			}
		}
	case AnswerKindSelect, AnswerKindMultiSelect:
		for _, v := range c.Value {
			found := false
			for _, o := range q.options {
				if strings.EqualFold(strings.TrimSpace(o), strings.TrimSpace(v)) {
					found = true
					break
				}
			}
			if !found {
				problems = append(problems, fmt.Sprintf("%q is not one of step %q's options", v, c.Step))
			}
		}
	}
	return problems
}

// CompileTrack turns one authored track into the Template the engine stamps. It is deterministic
// for a given (document, options) pair -- the golden test relies on that.
func CompileTrack(track FollowUpTrack, taskTypes map[string]FollowUpTaskTy, opts CompileOptions) (Template, error) {
	if opts.EventAt.IsZero() {
		return Template{}, fmt.Errorf("%w: event moment is required", ErrFollowUpInvalid)
	}
	out := Template{Key: track.Key, Module: track.Module}
	seq := 0
	for _, s := range track.Steps {
		if s.When == StepWhenKidPenUnresolved && !opts.NeedsShedPlacement {
			continue
		}
		tt, ok := taskTypes[s.TaskType]
		if !ok {
			return Template{}, fmt.Errorf("%w: step %q names unknown task type %q", ErrFollowUpInvalid, s.Key, s.TaskType)
		}
		answer := s.Answer
		if answer == "" {
			answer = tt.AnswerKind
		}
		actionType := actionTypeFor(answer)
		section := s.Section
		if section == "" {
			section = SectionMain
		}
		base := ActionTemplate{
			Key:           s.Key,
			Section:       section,
			Type:          actionType,
			Title:         s.Title,
			Detail:        s.Detail,
			RequiresVideo: s.Proof.Video > 0,
			Options:       append([]string(nil), s.Options...),
			TaskType:      s.TaskType,
			AnswerKind:    answer,
			EngineHook:    tt.EngineHook,
			Proof:         s.Proof,
			HardTimeGate:  s.HardTimeGate,
			WaitForAll:    s.WaitForAll,
			Requires:      append([]string(nil), s.Requires...),
			AnswerGate:    s.WhenAnswer,
		}
		switch s.Schedule.Kind {
		case ScheduleKindSeries:
			sessions, err := expandSeries(s.Schedule, opts.EventAt)
			if err != nil {
				return Template{}, err
			}
			ordinal := s.Schedule.OrdinalStart
			if ordinal == 0 {
				ordinal = 1
			}
			for i, ses := range sessions {
				seq++
				row := base
				row.Seq = seq
				row.Key = strings.NewReplacer("{day}", strconv.Itoa(ses.DayOffset+1), "{hhmm}", fmt.Sprintf("%02d%02d", ses.Hour, ses.Minute), "{n}", strconv.Itoa(i+1)).Replace(s.Schedule.KeyPattern)
				dayLabel := "birth day"
				if ses.DayOffset >= 1 {
					dayLabel = "day after birth"
				}
				if s.TitlePattern != "" {
					row.Title = strings.NewReplacer("{ordinal}", ordinal2(ordinal+i), "{time}", ses.Label).Replace(s.TitlePattern)
				}
				row.Detail = strings.NewReplacer("{time}", ses.Label, "{day_label}", dayLabel).Replace(s.Detail)
				if ses.FromEvent {
					row.Schedule = Schedule{Offset: ses.Offset}
				} else {
					row.Schedule = Schedule{AtFixedTime: true, DayOffset: ses.DayOffset, Hour: ses.Hour, Minute: ses.Minute}
				}
				out.Actions = append(out.Actions, row)
			}
		default:
			seq++
			base.Seq = seq
			sched, err := scheduleFor(s.Schedule)
			if err != nil {
				return Template{}, fmt.Errorf("%w: step %q: %v", ErrFollowUpInvalid, s.Key, err)
			}
			base.Schedule = sched
			out.Actions = append(out.Actions, base)
		}
	}
	if len(out.Actions) == 0 {
		return Template{}, fmt.Errorf("%w: track %q compiled to no steps", ErrFollowUpInvalid, track.Key)
	}
	return out, nil
}

func actionTypeFor(answerKind string) string {
	switch answerKind {
	case AnswerKindSelect, AnswerKindMultiSelect:
		return ActionTypeQuestionSelect
	case AnswerKindYesNo, AnswerKindNumber, AnswerKindText:
		return ActionTypeQuestion
	default:
		return ActionTypeAction
	}
}

func scheduleFor(s FollowUpSchedule) (Schedule, error) {
	switch s.Kind {
	case ScheduleKindImmediately, "":
		return Schedule{}, nil
	case ScheduleKindAfterEvent:
		return Schedule{Offset: time.Duration(s.OffsetMinutes) * time.Minute}, nil
	case ScheduleKindAtFixedTime:
		h, m, err := parseWallClock(s.Time)
		if err != nil {
			return Schedule{}, err
		}
		return Schedule{AtFixedTime: true, DayOffset: s.DayOffset, Hour: h, Minute: m}, nil
	case ScheduleKindAfterStep:
		return Schedule{AfterStepKey: s.Step, Offset: time.Duration(s.OffsetMinutes) * time.Minute}, nil
	}
	return Schedule{}, fmt.Errorf("unknown schedule kind %q", s.Kind)
}

type seriesSession struct {
	DayOffset int
	Hour      int
	Minute    int
	Label     string
	// FromEvent rounds are due at event + Offset (the "from_event" basis); the wall-clock fields
	// above are then only the rendered label and key.
	FromEvent bool
	Offset    time.Duration
}

// expandSeries applies the series eligibility rule: a slot on the event day is included only when
// the event happened strictly before the slot's pre-notify cutoff; every slot on the following
// days is included. This is the legacy birthColostrumSessions rule, now driven by config.
func expandSeries(s FollowUpSchedule, eventAt time.Time) ([]seriesSession, error) {
	if s.Basis == SeriesBasisFromEvent {
		return expandSeriesFromEvent(s, eventAt)
	}
	nextSessions := s.Basis == SeriesBasisNextSessions
	type slot struct{ h, m int }
	var slots []slot
	for _, raw := range s.Times {
		h, m, err := parseWallClock(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: series time %q: %v", ErrFollowUpInvalid, raw, err)
		}
		slots = append(slots, slot{h, m})
	}
	sort.SliceStable(slots, func(i, j int) bool {
		if slots[i].h != slots[j].h {
			return slots[i].h < slots[j].h
		}
		return slots[i].m < slots[j].m
	})
	at := eventAt.In(biztime.DefaultLocation())
	day := biztime.BusinessDayStart(at)
	preNotify := time.Duration(s.PreNotifyMinute) * time.Minute
	var out []seriesSession
	// fixed_times walks `days` days; next_sessions walks as many days as it takes to collect
	// `count` rounds (bounded: at least one slot per day, count <= seriesMaxRounds).
	days := s.Days
	if nextSessions {
		days = s.Count + 1
	}
	for d := 0; d < days; d++ {
		for _, sl := range slots {
			if d == 0 {
				slotAt := time.Date(day.Year(), day.Month(), day.Day(), sl.h, sl.m, 0, 0, biztime.DefaultLocation())
				if !at.Before(slotAt.Add(-preNotify)) {
					continue
				}
			}
			if nextSessions && len(out) >= s.Count {
				return out, nil
			}
			out = append(out, seriesSession{DayOffset: d, Hour: sl.h, Minute: sl.m, Label: fmt.Sprintf("%02d:%02d", sl.h, sl.m)})
		}
	}
	return out, nil
}

// expandSeriesFromEvent runs `count` rounds every `interval_minutes` from the event instant, so
// a kid born at 15:00 still gets every round -- they just land at 19:00, 23:00, 03:00 … instead
// of the farm's fixed sessions. Nothing is skipped: the animal's clock starts at its own birth.
func expandSeriesFromEvent(s FollowUpSchedule, eventAt time.Time) ([]seriesSession, error) {
	if s.IntervalMinutes <= 0 || s.Count < 1 {
		return nil, fmt.Errorf("%w: series from the event needs a positive interval and count", ErrFollowUpInvalid)
	}
	at := eventAt.In(biztime.DefaultLocation())
	eventDay := biztime.BusinessDayStart(at)
	out := make([]seriesSession, 0, s.Count)
	for k := 1; k <= s.Count; k++ {
		off := time.Duration(k*s.IntervalMinutes) * time.Minute
		due := at.Add(off)
		dayOffset := int(biztime.BusinessDayStart(due).Sub(eventDay).Hours() / 24)
		out = append(out, seriesSession{
			DayOffset: dayOffset, Hour: due.Hour(), Minute: due.Minute(),
			Label: due.Format("15:04"), FromEvent: true, Offset: off,
		})
	}
	return out, nil
}

func parseWallClock(raw string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("%q is not HH:MM", raw)
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("%q is not HH:MM", raw)
	}
	return h, m, nil
}

func ordinal2(n int) string { return ordinal(n) }

// TemplateKeyToSOP maps a workflow template key to the SOP code + track that authors it. The
// tasks module opens workflows by template key (its natural keys and indexes are built on it), so
// this is the one place the two vocabularies meet.
func TemplateKeyToSOP(templateKey string) (sopCode string, trackKey string, ok bool) {
	// A GENERAL SOP (maintainer decision 2026-09-18) is keyed "general:<sop code>" and always
	// runs its "main" track; each start is its own workflow (subject_ref_id), never an animal.
	if code, isGeneral := GeneralSOPCode(templateKey); isGeneral {
		return code, GeneralTrackKey, true
	}
	switch templateKey {
	case TemplateKeyBirthKid, TemplateKeyBirthMother:
		return SOPCodeBirth, templateKey, true
	case TemplateKeyDeath:
		return SOPCodeDeath, templateKey, true
	case TemplateKeyReconcile:
		return SOPCodeReconcile, templateKey, true
	case TemplateKeyShifting:
		return SOPCodeShifting, templateKey, true
	}
	return "", "", false
}

// General SOP template keys.
const (
	GeneralTemplatePrefix = "general:"
	GeneralTrackKey       = "main"
	ModuleGeneral         = "general"
)

// GeneralTemplateKey is the workflow template key of a general SOP code.
func GeneralTemplateKey(sopCode string) string { return GeneralTemplatePrefix + sopCode }

// GeneralSOPCode reads a general template key back to its SOP code.
func GeneralSOPCode(templateKey string) (string, bool) {
	if !strings.HasPrefix(templateKey, GeneralTemplatePrefix) {
		return "", false
	}
	code := strings.TrimPrefix(templateKey, GeneralTemplatePrefix)
	return code, code != ""
}

// SOP codes the herd-operations follow-ups are authored under (sop_definitions.code).
const (
	SOPCodeBirth     = "counts.birth"
	SOPCodeDeath     = "counts.death"
	SOPCodeShifting  = "shifting" // the 000175 library code; kept so the existing versions stay attached
	SOPCodeReconcile = "counts.reconcile"

	TemplateKeyReconcile = "reconcile"
	TemplateKeyShifting  = "shifting"
	ModuleReconcile      = "reconcile"
	ModuleShifting       = "shifting"
)
