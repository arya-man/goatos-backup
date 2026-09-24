package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// decideVaccinationWrite is the pure half of the persistence guard shared by the single-row and
// batch (carry-over, drive override) paths. Every branch is pinned here without a database.

func decideDay(y int, m time.Month, d int) time.Time {
	return biztime.BusinessDayStart(time.Date(y, m, d, 12, 0, 0, 0, time.UTC))
}

const fatteningPolicy = `{"purpose_plans":{"fattening":{"first_wave":["ET_TT","PPR"],"second_wave_after_days":21,"goat_second_wave":["GOAT_POX"],"sheep_second_wave":["SHEEP_POX"]}}}`

func decideWrite(due time.Time, basis string) vaccinationWrite {
	return vaccinationWrite{
		Tenant: pgtype.UUID{Valid: true}, Rule: pgtype.UUID{Valid: true}, TargetType: "goat",
		Target: pgtype.UUID{Valid: true}, DueAt: due, ScheduleBasis: basis,
	}
}

func decideHistory(admin, anchors map[string]time.Time) vaccinationGoatHistory {
	if admin == nil {
		admin = map[string]time.Time{}
	}
	if anchors == nil {
		anchors = map[string]time.Time{}
	}
	return vaccinationGoatHistory{Administered: admin, Anchored: anchors}
}

type decideWant int

const (
	wantAllowed decideWant = iota
	wantFloor
	wantNotApplicable
	wantPlumbing
)

func TestDecideVaccinationWriteCoversEveryBranch(t *testing.T) {
	dob := decideDay(2026, 1, 1)
	arrival := decideDay(2026, 8, 1)
	prev := decideDay(2026, 3, 1)
	birth := func(offset int32) *vaccinationWriteContract {
		return &vaccinationWriteContract{TriggerType: "birth_age", OffsetDays: offset, Repeat: "none", VaccineCode: "ET_TT", DOB: &dob, Species: "goat"}
	}
	cases := []struct {
		name  string
		write vaccinationWrite
		in    vaccinationWriteInputs
		want  decideWant
	}{
		{"unknown basis is a plumbing error", decideWrite(dob, "bogus"), vaccinationWriteInputs{}, wantPlumbing},
		{"non-goat anchored target passes", vaccinationWrite{TargetType: "shed", DueAt: dob}, vaccinationWriteInputs{}, wantAllowed},
		{"non-goat catch-up is refused", vaccinationWrite{TargetType: "shed", DueAt: dob, ScheduleBasis: domain.ScheduleBasisAnchorMissingCatchUp}, vaccinationWriteInputs{}, wantFloor},
		{"non-vaccination rule anchored passes", decideWrite(dob, ""), vaccinationWriteInputs{}, wantAllowed},
		{"non-vaccination rule catch-up is refused", decideWrite(dob, domain.ScheduleBasisAnchorMissingCatchUp), vaccinationWriteInputs{}, wantFloor},
		{"malformed policy is a plumbing error", decideWrite(dob, ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "birth_age", VaccineCode: "ET_TT", DOB: &dob, PolicyJSON: []byte(`{`)}}, wantPlumbing},
		{"non_breeding purpose is not applicable", decideWrite(dob.AddDate(1, 0, 0), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "birth_age", VaccineCode: "ET_TT", DOB: &dob, Purpose: "non_breeding"}}, wantNotApplicable},
		{"vaccine outside the fattening plan is not applicable", decideWrite(dob.AddDate(1, 0, 0), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "birth_age", VaccineCode: "FMD", DOB: &dob, Purpose: "fattening", PolicyJSON: []byte(fatteningPolicy)}}, wantNotApplicable},
		{"birth_age below floor", decideWrite(dob.AddDate(0, 0, 27), ""), vaccinationWriteInputs{Contract: birth(28)}, wantFloor},
		{"birth_age at floor", decideWrite(dob.AddDate(0, 0, 28), ""), vaccinationWriteInputs{Contract: birth(28)}, wantAllowed},
		{"catch-up with a known anchor still gets the floor", decideWrite(dob.AddDate(0, 0, 27), domain.ScheduleBasisAnchorMissingCatchUp), vaccinationWriteInputs{Contract: birth(28)}, wantFloor},
		{"post_arrival below arrival floor", decideWrite(arrival.AddDate(0, 0, 6), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "post_arrival", OffsetDays: 7, VaccineCode: "ET_TT", ArrivalAt: &arrival}}, wantFloor},
		{"post_arrival at arrival floor", decideWrite(arrival.AddDate(0, 0, 7), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "post_arrival", OffsetDays: 7, VaccineCode: "ET_TT", ArrivalAt: &arrival, DOB: &dob}}, wantAllowed},
		{"post_arrival missing arrival anchored is refused", decideWrite(arrival, ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "post_arrival", VaccineCode: "ET_TT", DOB: &dob}}, wantFloor},
		{"birth_age missing DOB anchored is refused", decideWrite(arrival, ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "birth_age", VaccineCode: "ET_TT"}}, wantFloor},
		{"catch-up on a blank family passes", decideWrite(arrival, domain.ScheduleBasisAnchorMissingCatchUp), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "birth_age", VaccineCode: "ET_TT"}, History: decideHistory(nil, nil)}, wantAllowed},
		{"catch-up with administration history is refused", decideWrite(arrival, domain.ScheduleBasisAnchorMissingCatchUp), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "birth_age", VaccineCode: "ET_TT"}, History: decideHistory(map[string]time.Time{"ettt": prev}, nil)}, wantFloor},
		{"catch-up with an anchor event is refused", decideWrite(arrival, domain.ScheduleBasisAnchorMissingCatchUp), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "birth_age", VaccineCode: "ET_TT"}, History: decideHistory(nil, map[string]time.Time{"ettt": prev})}, wantFloor},
		{"catch-up family falls back to the vaccine name", decideWrite(arrival, domain.ScheduleBasisAnchorMissingCatchUp), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "birth_age", VaccineName: "ET+TT"}, History: decideHistory(map[string]time.Time{"ettt": prev}, nil)}, wantFloor},
		{"after_previous_completion without history is refused", decideWrite(arrival, ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "after_previous_completion", OffsetDays: 21, VaccineCode: "ET_TT"}, History: decideHistory(nil, nil)}, wantFloor},
		{"after_previous_completion below max(offset,min_gap)", decideWrite(prev.AddDate(0, 0, 29), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "after_previous_completion", OffsetDays: 21, MinGapDays: 30, VaccineCode: "ET_TT"}, History: decideHistory(map[string]time.Time{"ettt": prev}, nil)}, wantFloor},
		{"after_previous_completion at max(offset,min_gap)", decideWrite(prev.AddDate(0, 0, 30), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "after_previous_completion", OffsetDays: 21, MinGapDays: 30, VaccineCode: "ET_TT"}, History: decideHistory(map[string]time.Time{"ettt": prev}, nil)}, wantAllowed},
		{"after_previous_completion offset wins over smaller min_gap", decideWrite(prev.AddDate(0, 0, 20), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "after_previous_completion", OffsetDays: 21, MinGapDays: 7, VaccineCode: "ET_TT"}, History: decideHistory(map[string]time.Time{"ettt": prev}, nil)}, wantFloor},
		{"after_previous_completion zero gap has no floor", decideWrite(prev, ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "after_previous_completion", VaccineCode: "ET_TT"}, History: decideHistory(map[string]time.Time{"ettt": prev}, nil)}, wantAllowed},
		{"yearly revac below one year", decideWrite(prev.AddDate(1, 0, -1), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "after_previous_completion", OffsetDays: 21, Repeat: "yearly", VaccineCode: "ET_TT"}, History: decideHistory(map[string]time.Time{"ettt": prev}, nil)}, wantFloor},
		{"yearly revac at one year", decideWrite(prev.AddDate(1, 0, 0), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "after_previous_completion", OffsetDays: 21, Repeat: "yearly", VaccineCode: "ET_TT"}, History: decideHistory(map[string]time.Time{"ettt": prev}, nil)}, wantAllowed},
		{"anchor event chains the previous dose (later of the two wins)", decideWrite(prev.AddDate(0, 0, 25), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "after_previous_completion", OffsetDays: 21, VaccineCode: "ET_TT"}, History: decideHistory(map[string]time.Time{"ettt": prev}, map[string]time.Time{"ettt": prev.AddDate(0, 0, 10)})}, wantFloor},
		{"anchor event alone chains the previous dose", decideWrite(prev.AddDate(0, 0, 21), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "after_previous_completion", OffsetDays: 21, VaccineCode: "ET_TT"}, History: decideHistory(nil, map[string]time.Time{"ettt": prev})}, wantAllowed},
		{"unknown trigger has no timing floor", decideWrite(dob, ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "manual", VaccineCode: "ET_TT"}}, wantAllowed},
		{"second wave with incomplete first wave", decideWrite(arrival.AddDate(1, 0, 0), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "manual", VaccineCode: "GOAT_POX", Purpose: "fattening", Species: "goat", PolicyJSON: []byte(fatteningPolicy)}, History: decideHistory(map[string]time.Time{"ettt": prev}, nil)}, wantFloor},
		{"second wave ignores anchor events as first-wave evidence", decideWrite(arrival.AddDate(1, 0, 0), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "manual", VaccineCode: "GOAT_POX", Purpose: "fattening", Species: "goat", PolicyJSON: []byte(fatteningPolicy)}, History: decideHistory(map[string]time.Time{"ettt": prev}, map[string]time.Time{"ppr": prev})}, wantFloor},
		{"second wave below latest first wave + delay", decideWrite(prev.AddDate(0, 0, 30), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "manual", VaccineCode: "GOAT_POX", Purpose: "fattening", Species: "goat", PolicyJSON: []byte(fatteningPolicy)}, History: decideHistory(map[string]time.Time{"ettt": prev, "ppr": prev.AddDate(0, 0, 10)}, nil)}, wantFloor},
		{"second wave at latest first wave + delay", decideWrite(prev.AddDate(0, 0, 31), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "manual", VaccineCode: "GOAT_POX", Purpose: "fattening", Species: "goat", PolicyJSON: []byte(fatteningPolicy)}, History: decideHistory(map[string]time.Time{"ettt": prev, "ppr": prev.AddDate(0, 0, 10)}, nil)}, wantAllowed},
		{"first-wave vaccine is plan governed but has no wave floor", decideWrite(dob.AddDate(0, 0, 28), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "birth_age", OffsetDays: 28, VaccineCode: "PPR", DOB: &dob, Purpose: "fattening", PolicyJSON: []byte(fatteningPolicy)}}, wantAllowed},
		{"sheep second wave uses the sheep list", decideWrite(prev.AddDate(0, 0, 21), ""), vaccinationWriteInputs{Contract: &vaccinationWriteContract{TriggerType: "manual", VaccineCode: "SHEEP_POX", Purpose: "fattening", Species: "sheep", PolicyJSON: []byte(fatteningPolicy)}, History: decideHistory(map[string]time.Time{"ettt": prev, "ppr": prev}, nil)}, wantAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := decideVaccinationWrite(tc.write, tc.in)
			switch tc.want {
			case wantAllowed:
				if err != nil {
					t.Fatalf("want allowed, got %v", err)
				}
			case wantFloor:
				if !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
					t.Fatalf("want ErrBeforeVaccinationAgeFloor, got %v", err)
				}
			case wantNotApplicable:
				if !errors.Is(err, ports.ErrVaccinationNotApplicable) {
					t.Fatalf("want ErrVaccinationNotApplicable, got %v", err)
				}
			case wantPlumbing:
				if err == nil || isVaccinationContractViolation(err) {
					t.Fatalf("want a non-clinical error, got %v", err)
				}
			}
		})
	}
}

func TestDedupeCarryOverRebindsKeepsFirstPerUniqueKey(t *testing.T) {
	u := func(b byte) pgtype.UUID { return pgtype.UUID{Bytes: [16]byte{b}, Valid: true} }
	due := decideDay(2026, 9, 10)
	cs := []carryOverCandidate{
		{obligationID: "a", target: u(1), dueAt: due, version: u(9), rule: u(8)},
		{obligationID: "b", target: u(1), dueAt: due, version: u(9), rule: u(8)}, // dup_guard clash with a
		{obligationID: "c", target: u(1), dueAt: due.AddDate(0, 0, 1), version: u(9), rule: u(8), repeatSourceRef: pgtype.Text{String: "x", Valid: true}},
		{obligationID: "d", target: u(1), dueAt: due.AddDate(0, 0, 2), version: u(9), rule: u(8), repeatSourceRef: pgtype.Text{String: "x", Valid: true}}, // cause clash with c
		{obligationID: "e", target: u(2), dueAt: due, version: u(9), rule: u(8), repeatAnchor: u(5)},
		{obligationID: "f", target: u(3), dueAt: due, version: u(9), rule: u(8), repeatAnchor: u(5)}, // anchor clash with e
		{obligationID: "g", target: u(3), dueAt: due, version: u(9), rule: u(7)},
	}
	got := dedupeCarryOverRebinds(cs)
	var ids []string
	for _, c := range got {
		ids = append(ids, c.obligationID)
	}
	want := []string{"a", "c", "e", "g"}
	if len(ids) != len(want) {
		t.Fatalf("kept %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("kept %v, want %v", ids, want)
		}
	}
}

func TestCarryOverGoatChunksDedupesSortsAndBounds(t *testing.T) {
	chunks := carryOverGoatChunks([]string{"C", "a", "b", "a", " ", "d", "e"}, 2)
	want := [][]string{{"a", "b"}, {"c", "d"}, {"e"}}
	if len(chunks) != len(want) {
		t.Fatalf("chunks %v, want %v", chunks, want)
	}
	for i := range want {
		for j := range want[i] {
			if chunks[i][j] != want[i][j] {
				t.Fatalf("chunks %v, want %v", chunks, want)
			}
		}
	}
}
