package domain

import "testing"

func TestRollupChipNamesTheMostUrgentPen(t *testing.T) {
	t.Parallel()
	today := "2026-09-12"
	open := func(due, work string) Task {
		return Task{Status: StatusOpen, WorkState: work, DueDate: due, PlannedDate: due}
	}
	cases := []struct {
		name       string
		visits     []Task
		owedNoRow  int
		chip, tone string
	}{
		{"nothing owed", nil, 0, "", ""},
		{"one pen owed today", []Task{open(today, WorkStateScheduled)}, 0, "Visit pen today", "info"},
		{"two pens owed today", []Task{open(today, WorkStateScheduled), open(today, WorkStateScheduled)}, 0, "Visit pens today", "info"},
		{"in review beside owed today: owed wins", []Task{{Status: StatusPendingVerification, WorkState: WorkStateScheduled}, open(today, WorkStateScheduled)}, 0, "Visit pen today", "info"},
		{"all in review", []Task{{Status: StatusPendingVerification, WorkState: WorkStateScheduled}}, 0, "Visit in review", "review"},
		{"rework beats everything", []Task{{Status: StatusRework, WorkState: WorkStateScheduled}, open(today, WorkStateDelayed)}, 0, "Visit needs another video", "danger"},
		{"delayed beats today", []Task{open("2026-09-10", WorkStateDelayed), open(today, WorkStateScheduled)}, 0, "Visit delayed since 10/09/2026", "danger"},
		{"videos verified, visit row not born yet", nil, 2, "Pen visit tomorrow", "info"},
		{"owed later", []Task{open("2026-09-14", WorkStateScheduled)}, 0, "Visit pen 14/09/2026", "info"},
		{"all verified", []Task{{Status: StatusCompleted, WorkState: WorkStateCompleted}}, 0, "Visit verified", "success"},
	}
	for _, c := range cases {
		chip, tone := RollupChip(c.visits, c.owedNoRow, today)
		if chip != c.chip || tone != c.tone {
			t.Errorf("%s: got (%q,%q) want (%q,%q)", c.name, chip, tone, c.chip, c.tone)
		}
	}
}
