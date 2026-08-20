package domain

import (
	"strings"
	"testing"
)

func liveState() SaleCandidateState {
	return SaleCandidateState{
		GoatID: "g1", Exists: true, LifecycleStatus: "alive", ManagementStage: "F2", RowVersion: 3,
	}
}

// THE GATE'S WHOLE JOB. Each of these is an animal that must not reach a buyer's truck,
// and each is named as a sale/allocation blocker in the critical-animal-action
// guardrails doc. A regression here does not show up as a crash -- it shows up as a
// contagious or residue-carrying animal leaving the farm -- so every one is pinned.
func TestSaleGateBlocksEveryAnimalThatMustNotBeSold(t *testing.T) {
	today := "2026-08-20"
	for _, tc := range []struct {
		name        string
		mutate      func(*SaleCandidateState)
		wantBlocker SaleBlocker
		wantReason  string
	}{
		{"quarantine", func(s *SaleCandidateState) { s.LifecycleStatus = "quarantine" }, BlockerClinical, "In quarantine"},
		{"icu", func(s *SaleCandidateState) { s.LifecycleStatus = "icu" }, BlockerClinical, "In ICU"},
		{"sick", func(s *SaleCandidateState) { s.LifecycleStatus = "sick" }, BlockerClinical, "Currently sick"},
		{"under treatment", func(s *SaleCandidateState) { s.LifecycleStatus = "under_treatment" }, BlockerClinical, "Under treatment"},
		{"newborn kid", func(s *SaleCandidateState) { s.ManagementStage = "K0" }, BlockerMilkKid, "Still a milk-drinking kid"},
		{"k1 kid", func(s *SaleCandidateState) { s.ManagementStage = "K1" }, BlockerMilkKid, "Still a milk-drinking kid"},
		{"weaning k3 kid", func(s *SaleCandidateState) { s.ManagementStage = "K3" }, BlockerMilkKid, "Still a milk-drinking kid"},
		{"withdrawal", func(s *SaleCandidateState) { s.WithdrawalUntil = "2026-08-24" }, BlockerWithdrawal, "Medicine withdrawal until 2026-08-24"},
		{"already sold", func(s *SaleCandidateState) { s.LifecycleStatus = "sold" }, BlockerAlreadyExited, "Already sold"},
		{"dead", func(s *SaleCandidateState) { s.LifecycleStatus = "dead" }, BlockerAlreadyExited, "Recorded as dead"},
		{"merged", func(s *SaleCandidateState) { s.Merged = true }, BlockerAlreadyExited, "This animal's record was merged into another"},
		{"missing", func(s *SaleCandidateState) { s.Exists = false }, BlockerAlreadyExited, "This animal is not in the herd"},
		{"tagged elsewhere", func(s *SaleCandidateState) { s.TaggedToOtherDealID = "deal-9" }, BlockerAlreadyTagged, "Already tagged to another sale"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := liveState()
			tc.mutate(&state)
			got := ResolveSaleBlocker(state, today)
			if got.Blocker != tc.wantBlocker {
				t.Fatalf("blocker = %q, want %q", got.Blocker, tc.wantBlocker)
			}
			if got.Reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tc.wantReason)
			}
			if got.Sellable() {
				t.Fatal("a blocked animal must never report itself sellable")
			}
		})
	}
}

// The gate must not block a healthy adult, or the flow is useless.
func TestSaleGateClearsAHealthyAdult(t *testing.T) {
	got := ResolveSaleBlocker(liveState(), "2026-08-20")
	if !got.Sellable() {
		t.Fatalf("a healthy adult must be sellable, got blocker %q (%s)", got.Blocker, got.Reason)
	}
	if got.Reason != "" {
		t.Fatalf("a sellable animal must carry no reason, got %q", got.Reason)
	}
}

// WITHDRAWAL IS INCLUSIVE OF ITS LAST DAY. "Withdrawal until the 24th" means the residue
// is gone ON the 24th, so blocking through the 24th would hold an animal back a day
// longer than the medicine requires, and blocking only up to the 23rd would release it a
// day early. Both directions are pinned so neither can be "simplified" in.
func TestWithdrawalClearsOnTheDateItselfAndBlocksTheDayBefore(t *testing.T) {
	state := liveState()
	state.WithdrawalUntil = "2026-08-24"

	if ResolveSaleBlocker(state, "2026-08-23").Blocker != BlockerWithdrawal {
		t.Fatal("the day before the withdrawal date must still block")
	}
	if got := ResolveSaleBlocker(state, "2026-08-24"); !got.Sellable() {
		t.Fatalf("the withdrawal date itself must clear, got %q", got.Reason)
	}
	if got := ResolveSaleBlocker(state, "2026-08-25"); !got.Sellable() {
		t.Fatalf("after the withdrawal date must clear, got %q", got.Reason)
	}
}

// SEVERITY ORDER. An animal can carry more than one blocker at once, and the person
// reading the screen needs the one that must be resolved FIRST. Reporting the withdrawal
// on a quarantined animal would send someone off to wait out a date when the real answer
// is that the animal is contagious.
func TestTheMostSeriousBlockerIsTheOneReported(t *testing.T) {
	state := liveState()
	state.LifecycleStatus = "quarantine"
	state.ManagementStage = "K1"
	state.WithdrawalUntil = "2026-09-30"
	if got := ResolveSaleBlocker(state, "2026-08-20"); got.Blocker != BlockerClinical {
		t.Fatalf("quarantine must outrank kid and withdrawal, got %q", got.Blocker)
	}

	// An animal that already left is not a live animal, so no medical sentence about it
	// can be true. "Already sold" must win over every clinical fact still on the row.
	exited := liveState()
	exited.LifecycleStatus = "sold"
	exited.ManagementStage = "K1"
	exited.WithdrawalUntil = "2026-09-30"
	if got := ResolveSaleBlocker(exited, "2026-08-20"); got.Blocker != BlockerAlreadyExited {
		t.Fatalf("an exited animal must report its exit, not a medical state, got %q", got.Blocker)
	}
}

// The reason is FARM COPY and is rendered verbatim by every surface. The copy firewall
// bans internal vocabulary in visible text, and these strings are visible text.
func TestBlockerReasonsAreFarmCopyNotInternalVocabulary(t *testing.T) {
	banned := []string{"lifecycle", "status=", "management_stage", "row_version",
		"backend", "api", "payload", "goat_id", "_"}
	states := []SaleCandidateState{}
	for _, s := range []string{"quarantine", "icu", "sick", "under_treatment", "sold", "dead", "culled", "transferred", "lost"} {
		st := liveState()
		st.LifecycleStatus = s
		states = append(states, st)
	}
	kid := liveState()
	kid.ManagementStage = "K1"
	wd := liveState()
	wd.WithdrawalUntil = "2026-08-24"
	states = append(states, kid, wd)

	for _, state := range states {
		reason := ResolveSaleBlocker(state, "2026-08-20").Reason
		if reason == "" {
			t.Fatalf("state %+v produced no reason; a blocked animal is always owed one", state)
		}
		for _, word := range banned {
			if strings.Contains(strings.ToLower(reason), word) {
				t.Fatalf("reason %q leaks internal vocabulary %q to an operator", reason, word)
			}
		}
	}
}
