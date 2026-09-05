package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
)

// The cause of death at the ROUTE boundary: what an operator standing over a dead animal
// gets back, and what never reaches the approver's queue.

type fakeDeathCauses struct {
	calls    int
	lastKey  string
	lastKind string
	err      error
}

func (f *fakeDeathCauses) ValidateCause(_ context.Context, key, kind string) error {
	f.calls++
	f.lastKey, f.lastKind = key, kind
	return f.err
}

func deathServerWithCauses(t *testing.T, causes DeathCauseValidator) (*http.ServeMux, *fakeApprovalWorkflow) {
	t.Helper()
	approvals := newFakeApprovalWorkflow()
	validator := newFakeGoatValidatorWithRepo(&stubIdentityRepo{})
	mux := http.NewServeMux()
	handler := NewAppWriteHandler(countsapp.NewService(newFakeShiftingRepo()), nil).
		WithApprovalWorkflow(approvals, validator)
	if causes != nil {
		handler = handler.WithDeathCauses(causes)
	}
	RegisterAppWrites(mux, handler)
	return mux, approvals
}

func diseaseDeathBody(key, kind string) map[string]any {
	body := deathBody("dead", "died")
	if key != "" {
		body["death_cause_key"] = key
	}
	if kind != "" {
		body["death_cause_kind"] = kind
	}
	return body
}

// A disease death carries its cause through to the stored payload, so the approver sees
// what the operator named and the exit applies it unchanged.
func TestDiseaseDeathCarriesTheCauseIntoTheApprovalPayload(t *testing.T) {
	causes := &fakeDeathCauses{}
	mux, approvals := deathServerWithCauses(t, causes)

	rec := post(t, mux, appDeathEventRoute, "death-cause-0001", diseaseDeathBody("MASTITIS", "register_rule"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	if causes.calls != 1 || causes.lastKey != "MASTITIS" || causes.lastKind != "register_rule" {
		t.Fatalf("validator saw calls=%d key=%q kind=%q", causes.calls, causes.lastKey, causes.lastKind)
	}
	var stored map[string]any
	if err := json.Unmarshal(approvals.lastSubmission.Payload, &stored); err != nil {
		t.Fatalf("stored payload is not JSON: %v", err)
	}
	if stored["death_cause_key"] != "MASTITIS" || stored["death_cause_kind"] != "register_rule" {
		t.Fatalf("the cause must survive into the approval payload: %+v", stored)
	}
}

// A NORMAL death never consults the disease list. A farm that does not use the toggle sees
// exactly the behaviour it always had, and a deployment without Health still records deaths.
func TestANormalDeathNeverConsultsTheDiseaseList(t *testing.T) {
	causes := &fakeDeathCauses{}
	mux, _ := deathServerWithCauses(t, causes)

	rec := post(t, mux, appDeathEventRoute, "death-cause-0002", deathBody("dead", "died"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	if causes.calls != 0 {
		t.Fatalf("validator was called %d times for a normal death", causes.calls)
	}
}

// A DISEASE THE REGISTER DOES NOT NAME COMES BACK TO THE OPERATOR, and nothing is parked.
//
// The check has to happen at raise time: the person who saw the animal is standing there
// now, and a rejection surfacing hours later in an approver's queue reaches someone who
// was not.
func TestAnUnknownDiseaseIsRefusedAtRaiseTimeAndParksNothing(t *testing.T) {
	causes := &fakeDeathCauses{err: errors.New(`"Mastitus" is not a disease in the diagnosis register`)}
	mux, approvals := deathServerWithCauses(t, causes)

	rec := post(t, mux, appDeathEventRoute, "death-cause-0003", diseaseDeathBody("Mastitus", "register_rule"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if approvals.submits != 0 {
		t.Fatalf("submissions=%d, want 0 -- a refused cause must not reach the approver", approvals.submits)
	}
	// The operator is told which disease was not recognised, not just that something failed.
	if body := rec.Body.String(); !containsDeathCause(body, "Mastitus") {
		t.Errorf("the refusal does not name the disease: %s", body)
	}
}

// WITH NO VALIDATOR WIRED, a cause is REFUSED rather than stored unchecked. Storing a key
// nothing verified defeats the one property that makes a coded cause worth having.
func TestACauseIsRefusedWhenTheDiseaseListIsNotWired(t *testing.T) {
	mux, approvals := deathServerWithCauses(t, nil)

	rec := post(t, mux, appDeathEventRoute, "death-cause-0004", diseaseDeathBody("MASTITIS", "register_rule"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if approvals.submits != 0 {
		t.Fatalf("submissions=%d, want 0", approvals.submits)
	}

	// ...while a NORMAL death on the same unwired handler still works.
	rec = post(t, mux, appDeathEventRoute, "death-cause-0005", deathBody("dead", "died"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("a normal death was refused on an unwired handler: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// Half a pair is refused before the validator is consulted, so a client cannot learn the
// vocabulary by probing with a bare key.
func TestHalfACausePairIsRefusedAtTheRoute(t *testing.T) {
	for _, body := range []map[string]any{
		diseaseDeathBody("MASTITIS", ""),
		diseaseDeathBody("", "register_rule"),
	} {
		causes := &fakeDeathCauses{}
		mux, approvals := deathServerWithCauses(t, causes)
		rec := post(t, mux, appDeathEventRoute, "death-cause-half", body)
		if rec.Code == http.StatusAccepted {
			t.Errorf("half a pair was accepted: %+v", body)
		}
		if approvals.submits != 0 {
			t.Errorf("half a pair reached the approver: %+v", body)
		}
	}
}

// A non-string cause is a malformed request, not a 500.
func TestANonStringCauseIsABadRequest(t *testing.T) {
	causes := &fakeDeathCauses{}
	mux, _ := deathServerWithCauses(t, causes)
	body := deathBody("dead", "died")
	body["death_cause_key"] = 42
	body["death_cause_kind"] = "register_rule"

	rec := post(t, mux, appDeathEventRoute, "death-cause-0006", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func containsDeathCause(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
