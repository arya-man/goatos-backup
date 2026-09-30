package domain

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSeedIsValid(t *testing.T) {
	if problems := Validate(Seed()); len(problems) > 0 {
		t.Fatalf("seed invalid: %v", problems)
	}
}

// Every list is authored, so every rule a bad authoring could break is refused at save.
func TestValidateRefusesWhatCouldNotRun(t *testing.T) {
	doc := Seed()
	doc.ViolationTypes = []ViolationType{
		{Key: "late", Title: "Late", Active: true},
		{Key: "late", Title: "Late again", Active: true},
		{Key: "Bad Key", Title: " "},
	}
	doc.Enquiries = append(doc.Enquiries, Enquiry{Trigger: "rain", Title: "x", DueHours: 0,
		Questions: []Question{{ID: "q", Kind: "photo", Title: "x"}, {ID: "q", Kind: QuestionText, Title: "y"}}})
	got := strings.Join(Validate(doc), "\n")
	for _, want := range []string{"is used twice", "key must be", "give it a name", "not an event", "deadline", "text or yes / no"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing problem %q in:\n%s", want, got)
		}
	}
}

// A violation type carries no money (maintainer, 2026-09-30: "never map money to mistake, both are
// separate"): a type that still names a fine is refused, never silently priced.
func TestAViolationTypeCarriesNoFine(t *testing.T) {
	_, problems := Parse(map[string]any{
		"schema_version":  SchemaVersion,
		"violation_types": []any{map[string]any{"key": "late", "title": "Late", "active": true, "default_fine": 100}},
		"enquiries":       []any{},
	})
	if len(problems) == 0 {
		t.Fatal("a violation type naming a fine must be refused")
	}
}

func TestParseRefusesAnUnknownField(t *testing.T) {
	_, problems := Parse(map[string]any{"schema_version": SchemaVersion, "violation_types": []any{}, "enquiries": []any{}, "fine_policy": "x"})
	if len(problems) == 0 {
		t.Fatal("an unknown field must be refused, never dropped")
	}
	doc, problems := Parse(map[string]any{"schema_version": SchemaVersion})
	if len(problems) > 0 || doc.ViolationTypes == nil || doc.Enquiries == nil {
		t.Fatalf("empty lists must parse as empty, got %+v %v", doc, problems)
	}
}

// The migration 000458 seed and Seed() are one document: a version-0 fallback that disagreed with
// the published v1 would run different rules for the same farm.
func TestMigrationEmbedsTheSeed(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/000458_hrms_violations_and_enquiries.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	start := strings.Index(s, "$seed$")
	end := strings.Index(s[start+6:], "$seed$")
	if start < 0 || end < 0 {
		t.Fatal("seed block not found")
	}
	var section map[string]any
	if err := json.Unmarshal([]byte(s[start+6:start+6+end]), &section); err != nil {
		t.Fatal(err)
	}
	doc, problems := Parse(section)
	if len(problems) > 0 {
		t.Fatalf("migration seed invalid: %v", problems)
	}
	if !reflect.DeepEqual(doc, Seed()) {
		t.Fatalf("migration seed %+v != Seed() %+v", doc, Seed())
	}
}

// The clock-in check names violation types from the SAME list, so a rename keeps it and a key the
// list does not carry is refused at save; a retired type turns that half off.
func TestAttendanceNamesTypesFromTheList(t *testing.T) {
	doc := Seed()
	doc.ViolationTypes = []ViolationType{{Key: "late_clock_in", Title: "Late clock-in", Active: true}, {Key: "did_not_clock_in", Title: "Did not clock in", Active: false}}
	doc.Attendance = &Attendance{GraceMinutes: 15, LateType: "late_clock_in", AbsentType: "did_not_clock_in"}
	if p := Validate(doc); len(p) > 0 {
		t.Fatalf("valid attendance refused: %v", p)
	}
	if _, ok := doc.AttendanceType(false); !ok {
		t.Fatal("the late half must be on")
	}
	if _, ok := doc.AttendanceType(true); ok {
		t.Fatal("a retired absent type must turn that half off")
	}
	doc.Attendance = &Attendance{GraceMinutes: 999, LateType: "nope"}
	got := strings.Join(Validate(doc), "\n")
	for _, want := range []string{"grace", "not one of the violation types"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if _, ok := Seed().AttendanceType(false); ok {
		t.Fatal("no section means the check is off")
	}
}

// Migration 000459 adds the clock-in check to every published HRMS SOP in place; its patch must
// itself be a valid document part.
func TestMigrationAddsAValidClockInCheck(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/000459_hrms_attendance_violations.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	start := strings.Index(s, "$types$")
	end := strings.Index(s[start+7:], "$types$")
	astart := strings.Index(s, "$att$")
	aend := strings.Index(s[astart+5:], "$att$")
	if start < 0 || end < 0 || astart < 0 || aend < 0 {
		t.Fatal("patch blocks not found")
	}
	var types []ViolationType
	var att Attendance
	if err := json.Unmarshal([]byte(s[start+7:start+7+end]), &types); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(s[astart+5:astart+5+aend]), &att); err != nil {
		t.Fatal(err)
	}
	doc := Seed()
	doc.ViolationTypes = types
	doc.Attendance = &att
	if p := Validate(doc); len(p) > 0 || att.GraceMinutes != 15 {
		t.Fatalf("patch invalid: %v %+v", p, att)
	}
}
