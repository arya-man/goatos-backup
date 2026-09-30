package domain

import (
	"errors"
	"strings"
	"testing"
)

func fieldCodes(t *testing.T, err error) map[string]string {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	out := map[string]string{}
	for _, f := range v.Fields {
		out[f.Field] = f.Code
	}
	return out
}

func mustRegister(t *testing.T, key string) Register {
	t.Helper()
	reg, ok := RegisterByKey(key)
	if !ok {
		t.Fatalf("register %s missing", key)
	}
	return reg
}

// TestEveryRegisterHasAConsistentDefinition pins the catalog shape the screen renders from: every
// ref points at a known register, every enum has options, keys are unique, and a read-only
// register says where it is edited.
func TestEveryRegisterHasAConsistentDefinition(t *testing.T) {
	seen := map[string]bool{}
	for _, reg := range Registers {
		if seen[reg.Key] {
			t.Fatalf("duplicate register %s", reg.Key)
		}
		seen[reg.Key] = true
		if reg.Label == "" || reg.One == "" || reg.Group == "" {
			t.Errorf("%s: label/one/group must be set", reg.Key)
		}
		if reg.ReadOnly && reg.EditHref == "" {
			t.Errorf("%s: a read-only register must say where it is edited", reg.Key)
		}
		for _, f := range reg.Filters {
			if _, ok := reg.Column(f); !ok {
				t.Errorf("%s: filter %s is not a column", reg.Key, f)
			}
		}
		hasName := false
		for _, c := range reg.Columns {
			if c.Key == "name" || c.Key == "label" || (reg.DisplayColumn != "" && c.Key == reg.DisplayColumn) {
				hasName = true
			}
			if c.Type == TypeRef {
				if _, ok := RegisterByKey(c.Ref); !ok {
					t.Errorf("%s.%s points at unknown register %s", reg.Key, c.Key, c.Ref)
				}
			}
		}
		if !hasName {
			t.Errorf("%s: no name/label column", reg.Key)
		}
	}
}

func TestValidateWriteCreateRequiresAndCoerces(t *testing.T) {
	reg := mustRegister(t, RegPens)
	_, err := ValidateWrite(reg, map[string]any{"name": "  "}, nil, "")
	codes := fieldCodes(t, err)
	if codes["park_id"] != "required" || codes["name"] != "required" {
		t.Fatalf("codes = %v, want park_id and name required", codes)
	}

	clean, err := ValidateWrite(reg, map[string]any{
		"park_id": "11111111-1111-4111-8111-111111111111", "name": " Castro ", "notes": "",
	}, nil, "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if clean["name"] != "Castro" {
		t.Fatalf("clean = %#v", clean)
	}
	// Capacity is set per partition, never on the building (maintainer instruction 2026-09-30).
	if _, err := ValidateWrite(reg, map[string]any{
		"park_id": "11111111-1111-4111-8111-111111111111", "name": "Castro", "capacity": "120",
	}, nil, ""); fieldCodes(t, err)["capacity"] != "unknown" {
		t.Fatalf("capacity must be unknown on a pen now, got %v", fieldCodes(t, err))
	}
	part, err := ValidateWrite(mustRegister(t, RegPartitions), map[string]any{
		"park_id": "11111111-1111-4111-8111-111111111111", "pen_id": "22222222-2222-4222-8222-222222222222", "label": " Part 3 ", "capacity": "120",
	}, nil, "")
	if err != nil || part["capacity"] != int64(120) || part["label"] != "Part 3" {
		t.Fatalf("partition capacity must coerce: %v %#v", err, part)
	}
	// A pen is a building in a park (maintainer instruction 2026-09-22): what is KEPT in it is not
	// a setting, so the register no longer carries stage, gender or ICU and a write naming one is
	// an unknown field like any other.
	if _, err := ValidateWrite(reg, map[string]any{
		"park_id": "11111111-1111-4111-8111-111111111111", "name": "Castro", "sex": "female", "has_icu": true, "stage_id": "x",
	}, nil, ""); fieldCodes(t, err)["sex"] != "unknown" || fieldCodes(t, err)["has_icu"] != "unknown" || fieldCodes(t, err)["stage_id"] != "unknown" {
		t.Fatalf("stage / gender / ICU must be unknown on a pen now, got %v", fieldCodes(t, err))
	}
	if v, ok := clean["notes"]; !ok || v != nil {
		t.Fatalf("a blank optional field must be sent as nil (clear), got %#v", clean["notes"])
	}
}

func TestValidateWriteRefusesUnknownEnumNumberAndKey(t *testing.T) {
	reg := mustRegister(t, RegPartitions)
	_, err := ValidateWrite(reg, map[string]any{
		"park_id": "x", "pen_id": "y", "label": "A", "capacity": "-3", "bogus": 1,
	}, nil, "")
	codes := fieldCodes(t, err)
	if codes["capacity"] != "invalid" || codes["bogus"] != "unknown" {
		t.Fatalf("codes = %v", codes)
	}
	// The enum refusal moved to a register that still has one; Pens carries no enum any more.
	tasks := mustRegister(t, RegTaskTypes)
	_, err = ValidateWrite(tasks, map[string]any{"name": "A", "code": "a", "answer_kind": "other"}, nil, "")
	if fieldCodes(t, err)["answer_kind"] != "invalid" {
		t.Fatalf("an unknown enum choice must be refused, got %v", fieldCodes(t, err))
	}
	_, err = ValidateWrite(reg, map[string]any{"park_id": "x", "pen_id": "y", "label": "A", "capacity": "12.5"}, nil, "")
	if fieldCodes(t, err)["capacity"] != "invalid" {
		t.Fatalf("a fractional integer must be refused")
	}
}

func TestValidateWriteCodeIsShapedAndImmutable(t *testing.T) {
	reg := mustRegister(t, RegSpecies)
	_, err := ValidateWrite(reg, map[string]any{"name": "Cow", "code": "Cow 1"}, nil, "")
	if fieldCodes(t, err)["code"] != "invalid" {
		t.Fatalf("a code with spaces/upper case must be refused")
	}
	existing := &Row{Fields: map[string]any{"name": "Goat", "code": "goat"}}
	_, err = ValidateWrite(reg, map[string]any{"code": "goats"}, existing, "")
	if fieldCodes(t, err)["code"] != "immutable" {
		t.Fatalf("changing a code must be refused")
	}
	clean, err := ValidateWrite(reg, map[string]any{"code": "goat", "name": "Goats"}, existing, "")
	if err != nil || clean["name"] != "Goats" {
		t.Fatalf("resending the same code is fine: %v %#v", err, clean)
	}
	if _, sent := clean["code"]; sent {
		t.Fatalf("an unchanged immutable code must not be written again")
	}
}

func TestValidateWriteUpdateOnlyChecksSentFields(t *testing.T) {
	reg := mustRegister(t, RegPartitions)
	existing := &Row{Fields: map[string]any{"park_id": "p", "pen_id": "s", "label": "3"}}
	clean, err := ValidateWrite(reg, map[string]any{"capacity": 40}, existing, "")
	if err != nil || clean["capacity"] != int64(40) || len(clean) != 1 {
		t.Fatalf("update: %v %#v", err, clean)
	}
	_, err = ValidateWrite(reg, map[string]any{"label": ""}, existing, "")
	if fieldCodes(t, err)["label"] != "required" {
		t.Fatalf("blanking a required field on update must be refused")
	}
}

func TestValidateWriteKindScopedColumns(t *testing.T) {
	reg := mustRegister(t, RegItems)
	base := map[string]any{"name": "Oxytet", "category_id": "c", "unit": "ml"}
	// A vaccine field on a medicine is refused; on a vaccine it is kept.
	withDisease := map[string]any{"disease": "PPR"}
	for k, v := range base {
		withDisease[k] = v
	}
	_, err := ValidateWrite(reg, withDisease, nil, "medicine")
	if fieldCodes(t, err)["disease"] != "not_for_kind" {
		t.Fatalf("disease on a medicine must be refused, got %v", err)
	}
	clean, err := ValidateWrite(reg, withDisease, nil, "vaccine")
	if err != nil || clean["disease"] != "PPR" {
		t.Fatalf("disease on a vaccine: %v %#v", err, clean)
	}
	// With no kind known yet (category unresolved), nothing is refused for kind.
	if _, err := ValidateWrite(reg, withDisease, nil, ""); err != nil {
		t.Fatalf("no kind: %v", err)
	}
}

func TestNormalizeCode(t *testing.T) {
	for in, want := range map[string]string{
		"Boer Goat":   "boer_goat",
		"  F2-Male ":  "f2_male",
		"Part 3":      "part_3",
		"9 lives":     "x_9_lives",
		"___":         "",
		"Sheep/Goat!": "sheep_goat",
	} {
		if got := NormalizeCode(in); got != want {
			t.Errorf("NormalizeCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUsageSentence(t *testing.T) {
	u := Usage{Uses: []UsageCount{{"animals", 12}, {"partitions", 0}, {"pens", 3}}}
	if got := u.Sentence(); got != "In use by 12 animals, 3 pens" {
		t.Fatalf("sentence = %q", got)
	}
	if (Usage{}).Sentence() != "" {
		t.Fatalf("empty usage must render nothing")
	}
	if !strings.Contains((&ValidationError{Fields: []FieldError{{Field: "name", Message: "x"}}}).Error(), "name") {
		t.Fatalf("validation error must name the field")
	}
}
