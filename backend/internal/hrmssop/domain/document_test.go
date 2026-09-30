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
		{Key: "late", Title: "Late", DefaultFine: 100, Active: true},
		{Key: "late", Title: "Late again", DefaultFine: -1, Active: true},
		{Key: "Bad Key", Title: " ", DefaultFine: 0},
	}
	doc.Enquiries = append(doc.Enquiries, Enquiry{Trigger: "rain", Title: "x", DueHours: 0,
		Questions: []Question{{ID: "q", Kind: "photo", Title: "x"}, {ID: "q", Kind: QuestionText, Title: "y"}}})
	got := strings.Join(Validate(doc), "\n")
	for _, want := range []string{"is used twice", "default fine", "key must be", "give it a name", "not an event", "deadline", "text or yes / no"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing problem %q in:\n%s", want, got)
		}
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
