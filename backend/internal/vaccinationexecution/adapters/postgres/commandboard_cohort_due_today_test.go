package postgres

import (
	"strings"
	"testing"
)

func TestCommandBoardCohortPendingExcludesDueToday(t *testing.T) {
	if strings.Contains(commandBoardCohortSQL, "date <= ($2::timestamptz") {
		t.Fatalf("cohort pending must not include doses due today; use a strict business-date comparison")
	}
	if !strings.Contains(commandBoardCohortSQL, "date < ($2::timestamptz") {
		t.Fatalf("cohort pending must use a strict business-date comparison")
	}
}
