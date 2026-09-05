package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
)

// The STRUCTURAL rules identity owns for a coded cause of death. The clinical vocabulary
// is Health's and is checked by the death route; what is asserted here is the shape, which
// identity enforces because it owns the animal's lifecycle.

func exitBody(mutate func(*domain.ExitGoatRequest)) *domain.ExitGoatRequest {
	body := &domain.ExitGoatRequest{
		LifecycleStatus: "dead",
		ExitReason:      "died",
		Reason:          "found down in the morning",
		RowVersion:      1,
		// THE TWO VIDEOS ARE STILL MANDATORY. Naming a disease does not replace the
		// evidence, and this fixture carries them so the tests below exercise the CAUSE
		// rules rather than tripping over the proof requirement.
		EvidenceRefs: []domain.EvidenceRef{
			{EvidenceType: "media", EvidenceID: "death-video"},
			{EvidenceType: "media", EvidenceID: "post-mortem-video"},
		},
	}
	if mutate != nil {
		mutate(body)
	}
	return body
}

// A DISEASE DEATH NEEDS NO NOTE. The coded cause is the recorded fact; the note is extra
// detail rather than the only thing standing between the farm and an unexplained death.
func TestADiseaseDeathIsAcceptedWithNoWrittenAccount(t *testing.T) {
	body := exitBody(func(b *domain.ExitGoatRequest) {
		b.Reason = ""
		b.DeathCauseKey = "MASTITIS"
		b.DeathCauseKind = "register_rule"
	})
	if err := validateCriticalDeathExit(body); err != nil {
		t.Fatalf("a disease death with no note was refused: %v", err)
	}
}

// A NORMAL DEATH STILL NEEDS ONE. Nothing else would explain it, and that is exactly the
// behaviour every death has had until now.
func TestANormalDeathStillRequiresTheWrittenAccount(t *testing.T) {
	body := exitBody(func(b *domain.ExitGoatRequest) { b.Reason = "" })
	err := validateCriticalDeathExit(body)
	if err == nil {
		t.Fatal("a normal death with no account was accepted")
	}
	if !strings.Contains(err.Error(), "reason") {
		t.Errorf("error = %v, want it to name the missing account", err)
	}
}

// A note SUPPLIED on a disease death is still length-checked, so "disease death" cannot
// become a way past the bound on a free-text field.
func TestANoteOnADiseaseDeathIsStillLengthChecked(t *testing.T) {
	for _, note := range []string{"ab", strings.Repeat("x", 501)} {
		body := exitBody(func(b *domain.ExitGoatRequest) {
			b.Reason = note
			b.DeathCauseKey = "MASTITIS"
			b.DeathCauseKind = "register_rule"
		})
		if err := validateCriticalDeathExit(body); err == nil {
			t.Errorf("a %d-character note on a disease death was accepted", len(note))
		}
	}
}

// BOTH OR NEITHER, matching the database constraint: a kind alone names nothing, and a key
// alone cannot be read because the same string can live in either vocabulary.
func TestHalfACauseIsRefused(t *testing.T) {
	for _, mutate := range []func(*domain.ExitGoatRequest){
		func(b *domain.ExitGoatRequest) { b.DeathCauseKey = "MASTITIS" },
		func(b *domain.ExitGoatRequest) { b.DeathCauseKind = "register_rule" },
	} {
		if err := validateCriticalDeathExit(exitBody(mutate)); err == nil {
			t.Error("half a cause pair was accepted")
		}
	}
}

func TestAnUnknownCauseKindIsRefused(t *testing.T) {
	body := exitBody(func(b *domain.ExitGoatRequest) {
		b.DeathCauseKey = "MASTITIS"
		b.DeathCauseKind = "guess"
	})
	if err := validateCriticalDeathExit(body); err == nil {
		t.Fatal("an unknown cause kind was accepted")
	}
}

// A CAUSE OF DEATH BELONGS ONLY TO A DEATH.
//
// The CULL is the case worth naming: it is the one exit a reader might expect to carry a
// disease, and it deliberately does not. A cull is a DECISION and a death is an OUTCOME,
// and the product counts them apart everywhere else — letting a cull carry a cause would
// quietly merge the two on the mortality board.
func TestOnlyADeathMayCarryACauseOfDeath(t *testing.T) {
	for status, reason := range map[string]string{
		"sold":        "sold",
		"culled":      "culled",
		"transferred": "transferred",
		"lost":        "lost",
	} {
		body := &domain.ExitGoatRequest{
			LifecycleStatus: status,
			ExitReason:      reason,
			Reason:          "left the herd",
			RowVersion:      1,
			DeathCauseKey:   "MASTITIS",
			DeathCauseKind:  "register_rule",
		}
		err := validateExitGoatCommon(body)
		if err == nil {
			t.Errorf("a %s exit was allowed to carry a cause of death", status)
			continue
		}
		if !strings.Contains(err.Error(), "cause of death") {
			t.Errorf("%s: error = %v, want it to say a cause belongs only to a death", status, err)
		}
	}
}

// Whitespace around a submitted cause is trimmed rather than stored, so " MASTITIS " and
// "MASTITIS" are one disease on the board instead of two.
func TestACauseIsTrimmedBeforeItIsStored(t *testing.T) {
	body := exitBody(func(b *domain.ExitGoatRequest) {
		b.DeathCauseKey = "  MASTITIS  "
		b.DeathCauseKind = "  register_rule  "
	})
	if err := validateCriticalDeathExit(body); err != nil {
		t.Fatalf("a padded cause was refused: %v", err)
	}
	if body.DeathCauseKey != "MASTITIS" || body.DeathCauseKind != "register_rule" {
		t.Errorf("cause = %q/%q, want it trimmed", body.DeathCauseKey, body.DeathCauseKind)
	}
}

// A cause that is ONLY whitespace is the same as none: a normal death, which then needs
// its written account like any other.
func TestAWhitespaceOnlyCauseIsANormalDeath(t *testing.T) {
	body := exitBody(func(b *domain.ExitGoatRequest) {
		b.DeathCauseKey = "   "
		b.DeathCauseKind = "   "
	})
	if err := validateCriticalDeathExit(body); err != nil {
		t.Fatalf("a blank cause was refused instead of read as a normal death: %v", err)
	}
	if body.DeathCauseKey != "" {
		t.Errorf("cause key = %q, want empty", body.DeathCauseKey)
	}
}
