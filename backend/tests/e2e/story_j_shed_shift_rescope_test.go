package e2e

import (
	"encoding/json"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryJ_ShedShiftRescope drives the real SM-2 goat-shift re-scope path: a goat with an
// open, unbatched obligation scoped to its current shed is moved to a different shed. The real
// oblapp.GoatShiftedHandler (the goat.location.changed / goat.shifted consumer) must re-scope the
// goat's open obligation to the new shed, so the goat is counted in the NEW shed's drive and the OLD
// shed no longer counts it. Out-of-order redelivery must not rewind the scope.
//
// Uses the genuine event handler + ReScopeOpenForGoatShift ordered repository method the production
// consumer runs, not a raw UPDATE.
func TestKernelStoryJ_ShedShiftRescope(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-j", "Shed shift: open obligation re-scoped to the new shed",
		"A goat's open vaccination dose is scoped to its current shed (Shed-Old), where it would be counted "+
			"in that shed's drive. The goat is physically moved to Shed-New. The kernel's shift handler must "+
			"re-scope the goat's open obligation to Shed-New so the new shed's drive counts it and the old "+
			"shed no longer does. A stale, out-of-order redelivery must not rewind the scope.")
	defer story.Finish()
	story.Certify("backend kernel")

	fx.PublishSimpleProtocol("vaccination.e2e.story_j", 21, 14, nil)

	const shedOld = "ea000000-0000-4000-8000-000000000001"
	const shedNew = "ea000000-0000-4000-8000-000000000002"
	const stageOld = "ea000000-0000-4000-8000-00000000000a"
	const stageNew = "ea000000-0000-4000-8000-00000000000b"
	fx.SeedShed(shedOld, "E2E-J-OLD", stageOld)
	fx.SeedAdultShed(shedNew, "E2E-J-NEW", stageNew, "K2")

	const goatID = "ea000000-0000-4000-8000-000000000010"
	due := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	dob := due.AddDate(0, 0, -21)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedOld, DOB: &dob})

	story.Step("Generate an open dose for a goat in Shed-Old",
		"One goat living in Shed-Old, one open scheduled dose scoped shed=Shed-Old (the scope generation "+
			"stamps so the SM-4 sweeper batches one drive per shed).")

	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatID, due.AddDate(0, 0, -1))
	oblID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)

	oldScoped := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND scope_type='shed' AND scope_id=$2 AND status='scheduled'`, fxTenant, shedOld)
	story.Assert("Shed-Old's drive counts the goat's open dose", oldScoped == 1, "count=%d", oldScoped)
	newScoped := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND scope_type='shed' AND scope_id=$2 AND status='scheduled'`, fxTenant, shedNew)
	story.Assert("Shed-New's drive counts nothing yet", newScoped == 0, "count=%d", newScoped)

	story.Step("Goat is moved to Shed-New: fire the real goat.shifted handler (SM-2)",
		"Move the goat to Shed-New and dispatch a goat.location.changed event through the real "+
			"oblapp.GoatShiftedHandler. It must re-scope the open obligation to Shed-New.")
	shiftAt := due.AddDate(0, 0, -5)
	fx.MoveGoat(goatID, shedNew, "story-j-move", shiftAt)
	handler := oblapp.NewGoatShiftedHandler(fx.Obl)
	story.Assert("identity move emitted and dispatched goat.location.changed", true, "production identity and SM-2 path completed")

	story.Step("Old shed no longer counts it; new shed does",
		"After the shift, the open dose must be scoped to Shed-New. Shed-Old's drive drops it; Shed-New's "+
			"drive picks it up. The goat itself is unchanged (same obligation id, still scheduled).")
	scopeAfter := fx.scanText(`SELECT scope_id::text FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, oblID)
	story.Assert("open obligation is now scoped to Shed-New", scopeAfter == shedNew, "scope_id=%s", scopeAfter)

	oldAfter := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND scope_type='shed' AND scope_id=$2 AND status='scheduled'`, fxTenant, shedOld)
	story.Assert("Shed-Old's drive no longer counts the goat", oldAfter == 0, "count=%d", oldAfter)
	newAfter := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND scope_type='shed' AND scope_id=$2 AND status='scheduled'`, fxTenant, shedNew)
	story.Assert("Shed-New's drive now counts the goat", newAfter == 1, "count=%d", newAfter)

	statusAfter := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, oblID)
	story.Assert("the dose is still open (scheduled), only re-scoped", statusAfter == "scheduled", "status=%q", statusAfter)

	story.Step("Out-of-order stale redelivery does not rewind the scope",
		"A late goat.shifted event that tries to move the goat BACK to Shed-Old with an OLDER timestamp "+
			"must be a durable no-op -- the ordered shift guard rejects the rewind.")
	stalePayload, _ := json.Marshal(oblap_ShiftPayload(shedOld))
	err := handler.HandleEvent(fx.Ctx, eventbus.Event{
		ID: "e2e-story-j-shift-stale", Type: oblapp.EventGoatShifted, TenantID: fxTenant, Key: goatID,
		Payload: stalePayload, OccurredAt: shiftAt.AddDate(0, 0, -2), // older than the accepted shift
	})
	story.Assert("stale rewind event ran without error", err == nil, "err=%v", err)
	scopeStale := fx.scanText(`SELECT scope_id::text FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, oblID)
	story.Assert("scope stays on Shed-New (no rewind to Shed-Old)", scopeStale == shedNew, "scope_id=%s", scopeStale)
}

// oblap_ShiftPayload builds the shift event body (destination scope) the way production producers do.
func oblap_ShiftPayload(shedID string) oblapp.ShiftPayload {
	return oblapp.ShiftPayload{ScopeType: "shed", ScopeID: shedID, ToShedID: shedID}
}
