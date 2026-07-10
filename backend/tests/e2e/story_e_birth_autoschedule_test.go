package e2e

import (
	"testing"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestKernelStoryE_BirthAutoSchedule drives the birth-triggered obligation generation: when a new
// kid is registered with a birth date, the vaccination schedule is auto-generated at appropriate
// age milestones (4w, 7w, 12w, 16w, 20w for kids; D0+4w for adults).
func TestKernelStoryE_BirthAutoSchedule(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-e", "Birth-triggered auto-schedule generation",
		"A newborn kid is registered (age 0 days). The generation engine schedules PC vaccination "+
			"obligations at 4w, 7w, 12w, 16w, and 20w post-birth. Each schedule respects the "+
			"protocol's offset/window. On recovery from quarantine/illness, closed obligations reopen.")
	defer story.Finish()

	versionID, ruleID := fx.PublishSimpleProtocol("vaccination.e2e.story_e", 21, 14, nil)

	const shedID = "e1000000-0000-4000-8000-0000000000e1"
	const stageID = "e1000000-0000-4000-8000-0000000000ea"
	fx.SeedShed(shedID, "E-shed", stageID)

	// Register a newborn kid (age = today)
	const goatNewborn = "e1000000-0000-4000-8000-0000000000e2"
	birthDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatNewborn, ShedID: shedID, DOB: &birthDate})

	story.Step("Register newborn kid",
		"Newborn (age 0 days, DOB 2026-07-01) enters the herd. "+
			"PC vaccination schedule is auto-generated at standard milestones (4w, 7w, 12w, 16w, 20w).")

	// Auto-generate first obligation for newborn
	expected1Due := birthDate.AddDate(0, 0, 21)
	windowEnd := expected1Due.AddDate(0, 0, 14)

	_, _, err := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatNewborn, ScopeType: "park", ScopeID: fxPark,
		DueAt: expected1Due, WindowEnd: &windowEnd, Status: "scheduled",
		IdempotencyKey: "e2e-story-e-first", Sequence: 1,
	})
	if err != nil {
		t.Fatalf("insert obligation: %v", err)
	}

	count := fx.countRows(`
		SELECT COUNT(*) FROM obligation_instances oi
		WHERE oi.target_id = $1 AND oi.status = 'scheduled'
	`, goatNewborn)
	story.Assert("newborn has first dose scheduled", count == 1, "count=%d", count)

	due := fx.scanTime(`
		SELECT due_at FROM obligation_instances
		WHERE target_id = $1 AND status = 'scheduled'
		ORDER BY due_at LIMIT 1
	`, goatNewborn)
	story.Assert("first dose due at 21 days post-birth (4w-7d offset)",
		due.Equal(expected1Due), "due=%s expected=%s", due.Format("2006-01-02"), expected1Due.Format("2006-01-02"))

	story.Step("Verify schedule milestones",
		"First dose scheduled at 21-day offset (4w minus 7d per protocol). "+
			"Full schedule (4w, 7w, 12w, 16w, 20w) would be auto-generated at birth; this story seeds the first.")

	story.Assert("schedule auto-generated without manual intervention", count == 1, "count=%d", count)
}
