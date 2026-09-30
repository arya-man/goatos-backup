// Package domain is the HRMS SOP document (maintainer instruction 2026-09-30: "every violation
// type, everything is SOP driven; in future every list should be changeable"). The published
// `hrms.violations` version says WHICH violation types exist and -- per farm event that opens an
// enquiry -- its title, deadline and questions. A TYPE CARRIES NO MONEY (maintainer, 2026-09-30:
// "never map money to mistake, both are separate"): the fine is typed on each violation by the
// person recording it, never priced by the list. HR and the CEO author it on People / HRMS > HRMS
// SOP; the workforce module runs it and stamps each violation and enquiry with the version it used.
//
// What is NOT authored, and why: the TRIGGERS. An enquiry is opened by an event the backend
// already emits (an approved animal death is the first); a trigger key names that engine wiring,
// so the document may only choose among KnownTriggers. Adding a trigger is code, the questions and
// deadline for it are not.
package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// SOPCode is the HRMS violations and enquiries document.
	SOPCode = "hrms.violations"
	// Section is the form_dsl key the document lives under.
	Section = "violations"
	// SchemaVersion is the section's schema.
	SchemaVersion = "goatos.sop-hrms-violations.v1"

	// TriggerAnimalDeath opens an enquiry when an animal's death is approved.
	TriggerAnimalDeath = "animal_death"

	QuestionText  = "text"
	QuestionYesNo = "yes_no"

	MaxViolationTypes = 100
	MaxQuestions      = 30
	// MaxFineRupees bounds the fine typed on a violation (the list itself carries none).
	MaxFineRupees = 10_000_000
	MaxDueHours       = 24 * 30
	// MaxGraceMinutes bounds how late a clock-in may be before the attendance check raises one.
	MaxGraceMinutes = 240
)

// KnownTriggers are the farm events the backend can open an enquiry for.
var KnownTriggers = map[string]string{
	TriggerAnimalDeath: "Animal death (when approved)",
}

// ViolationType is one authored kind of violation. Key is its stable identity (a rename keeps
// it); Active=false retires it from new records while past records keep their label.
// It carries no fine: a mistake and its money are separate facts.
type ViolationType struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
}

// Question is one question an enquiry report asks.
type Question struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Required bool   `json:"required"`
}

// Enquiry is the authored report a trigger opens.
type Enquiry struct {
	Trigger   string     `json:"trigger"`
	Title     string     `json:"title"`
	DueHours  int        `json:"due_hours"`
	Questions []Question `json:"questions"`
}

// Attendance is the automatic clock-in check (maintainer decisions 2026-09-30): a person mapped
// to a shift who clocks in more than GraceMinutes after its start gets a LateType violation, and
// one who does not clock in at all by the shift's end gets an AbsentType violation -- both WAITING
// for HR, who keeps or closes each. A day covered by leave the person applied for is never
// checked. A blank type turns that half off; no section at all turns the check off.
type Attendance struct {
	GraceMinutes int    `json:"grace_minutes"`
	LateType     string `json:"late_type"`
	AbsentType   string `json:"absent_type"`
	// StartsOn (YYYY-MM-DD) is the first day checked: switching the check on never raises old days.
	StartsOn string `json:"starts_on,omitempty"`
}

// Document is the `violations` section of the HRMS SOP.
type Document struct {
	SchemaVersion  string          `json:"schema_version"`
	ViolationTypes []ViolationType `json:"violation_types"`
	Enquiries      []Enquiry       `json:"enquiries"`
	Attendance     *Attendance     `json:"attendance,omitempty"`
}

// AttendanceType is the ACTIVE type an attendance kind raises, or false when that half is off
// (no section, a blank key, or the type retired).
func (d Document) AttendanceType(absent bool) (ViolationType, bool) {
	if d.Attendance == nil {
		return ViolationType{}, false
	}
	key := d.Attendance.LateType
	if absent {
		key = d.Attendance.AbsentType
	}
	t, ok := d.ViolationType(key)
	if key == "" || !ok || !t.Active {
		return ViolationType{}, false
	}
	return t, true
}

// Rules is a document together with the SOP version it came from (0 = the seed, no version
// published yet).
type Rules struct {
	Version  int
	Document Document
}

// Seed is the seeded document -- byte for byte the migration 000471 seed.
func Seed() Document {
	return Document{
		SchemaVersion:  SchemaVersion,
		ViolationTypes: []ViolationType{},
		Enquiries: []Enquiry{{
			Trigger:  TriggerAnimalDeath,
			Title:    "Death enquiry",
			DueHours: 48,
			Questions: []Question{
				{ID: "what_happened", Kind: QuestionText, Title: "What happened", Required: true},
			},
		}},
	}
}

// ViolationType returns the type with this key.
func (d Document) ViolationType(key string) (ViolationType, bool) {
	for _, t := range d.ViolationTypes {
		if t.Key == key {
			return t, true
		}
	}
	return ViolationType{}, false
}

// Enquiry returns the enquiry authored for a trigger.
func (d Document) Enquiry(trigger string) (Enquiry, bool) {
	for _, e := range d.Enquiries {
		if e.Trigger == trigger {
			return e, true
		}
	}
	return Enquiry{}, false
}

// ActiveTypes are the types a new violation may use.
func (d Document) ActiveTypes() []ViolationType {
	out := []ViolationType{}
	for _, t := range d.ViolationTypes {
		if t.Active {
			out = append(out, t)
		}
	}
	return out
}

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Parse decodes a section (as it sits in form_dsl) strictly: an unknown key is a problem, never a
// silent drop, because a misspelt field would otherwise publish a rule nobody applies.
func Parse(section any) (Document, []string) {
	raw, err := json.Marshal(section)
	if err != nil {
		return Document{}, []string{"the document could not be read"}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return Document{}, []string{"the document has a field this version does not know: " + err.Error()}
	}
	if doc.ViolationTypes == nil {
		doc.ViolationTypes = []ViolationType{}
	}
	if doc.Enquiries == nil {
		doc.Enquiries = []Enquiry{}
	}
	problems := Validate(doc)
	// A blank grace decodes as 0 -- a rule nobody typed -- so the key must be there with a number.
	if doc.Attendance != nil && !graceGiven(raw) {
		problems = append(problems, "clock-in check: give the grace in minutes (0 to 240)")
	}
	return doc, problems
}

// graceGiven reports whether the attendance section names grace_minutes as a number.
func graceGiven(raw []byte) bool {
	var probe struct {
		Attendance map[string]json.RawMessage `json:"attendance"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	v, ok := probe.Attendance["grace_minutes"]
	if !ok {
		return false
	}
	var n json.Number
	return json.Unmarshal(v, &n) == nil && n != ""
}

// Validate returns every problem with a document, in farm words.
func Validate(doc Document) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if doc.SchemaVersion != SchemaVersion {
		add("schema_version must be %s", SchemaVersion)
	}
	if len(doc.ViolationTypes) > MaxViolationTypes {
		add("at most %d violation types", MaxViolationTypes)
	}
	seenKey, seenTitle := map[string]bool{}, map[string]bool{}
	for i, t := range doc.ViolationTypes {
		where := fmt.Sprintf("violation type %d", i+1)
		if !keyPattern.MatchString(t.Key) {
			add("%s: its key must be lower-case letters, digits and _ (up to 40)", where)
		}
		if seenKey[t.Key] {
			add("%s: the key %q is used twice", where, t.Key)
		}
		seenKey[t.Key] = true
		title := strings.TrimSpace(t.Title)
		if title == "" || len([]rune(title)) > 80 {
			add("%s: give it a name of up to 80 letters", where)
		}
		if seenTitle[strings.ToLower(title)] && title != "" {
			add("%s: the name %q is used twice", where, title)
		}
		seenTitle[strings.ToLower(title)] = true
	}
	if a := doc.Attendance; a != nil {
		if a.GraceMinutes < 0 || a.GraceMinutes > MaxGraceMinutes {
			add("clock-in check: the grace must be between 0 and %d minutes", MaxGraceMinutes)
		}
		if a.StartsOn != "" {
			// The shape AND a real calendar day: 2026-13-45 would stop the check for everyone.
			if _, err := time.Parse("2006-01-02", a.StartsOn); err != nil || !datePattern.MatchString(a.StartsOn) {
				add("clock-in check: the start date must be a real date, YYYY-MM-DD")
			}
		}
		for _, k := range []string{a.LateType, a.AbsentType} {
			if k != "" && !seenKey[k] {
				add("clock-in check: %q is not one of the violation types", k)
			}
		}
	}
	seenTrigger := map[string]bool{}
	for i, e := range doc.Enquiries {
		where := fmt.Sprintf("enquiry %d", i+1)
		if _, ok := KnownTriggers[e.Trigger]; !ok {
			add("%s: %q is not an event that can open an enquiry", where, e.Trigger)
		}
		if seenTrigger[e.Trigger] {
			add("%s: the event %q already has an enquiry", where, e.Trigger)
		}
		seenTrigger[e.Trigger] = true
		if t := strings.TrimSpace(e.Title); t == "" || len([]rune(t)) > 80 {
			add("%s: give it a name of up to 80 letters", where)
		}
		if e.DueHours < 1 || e.DueHours > MaxDueHours {
			add("%s: the deadline must be between 1 and %d hours", where, MaxDueHours)
		}
		if len(e.Questions) > MaxQuestions {
			add("%s: at most %d questions", where, MaxQuestions)
		}
		seenQ := map[string]bool{}
		for j, q := range e.Questions {
			qw := fmt.Sprintf("%s, question %d", where, j+1)
			if !keyPattern.MatchString(q.ID) {
				add("%s: its id must be lower-case letters, digits and _ (up to 40)", qw)
			}
			if seenQ[q.ID] {
				add("%s: the id %q is used twice", qw, q.ID)
			}
			seenQ[q.ID] = true
			if q.Kind != QuestionText && q.Kind != QuestionYesNo {
				add("%s: the answer must be text or yes / no", qw)
			}
			if t := strings.TrimSpace(q.Title); t == "" || len([]rune(t)) > 200 {
				add("%s: write the question (up to 200 letters)", qw)
			}
		}
	}
	return out
}
