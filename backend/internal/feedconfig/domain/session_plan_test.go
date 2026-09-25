package domain

import (
	"errors"
	"testing"
)

func TestValidateSessionPlanRequiresTheWholeDay(t *testing.T) {
	good, err := ValidateSessionPlan([]SessionPlanEntry{
		{SessionNo: 1, Label: "  Morning ", SplitFraction: "0.6"},
		{SessionNo: 2, Label: "Evening", SplitFraction: ".4"},
	})
	if err != nil {
		t.Fatalf("a whole-day plan must validate: %v", err)
	}
	if good[0].Label != "Morning" || good[0].SplitFraction != "0.6000" || good[1].SplitFraction != "0.4000" {
		t.Fatalf("plan not canonicalised: %+v", good)
	}
	if _, err := ValidateSessionPlan([]SessionPlanEntry{{SessionNo: 1, Label: "All day", SplitFraction: "1"}}); err != nil {
		t.Fatalf("one session carrying the whole day is valid: %v", err)
	}

	for name, tc := range map[string]struct {
		plan []SessionPlanEntry
		want error
	}{
		"empty":        {nil, ErrMissingField},
		"short of day": {[]SessionPlanEntry{{1, "Morning", "0.5"}, {2, "Evening", "0.4"}}, ErrSessionSplitNotWhole},
		"over the day": {[]SessionPlanEntry{{1, "Morning", "0.6"}, {2, "Evening", "0.6"}}, ErrSessionSplitNotWhole},
		"one over day": {[]SessionPlanEntry{{1, "Morning", "1.5"}}, ErrValueOutOfRange},
		"duplicate no": {[]SessionPlanEntry{{1, "Morning", "0.5"}, {1, "Evening", "0.5"}}, ErrDuplicateSession},
		"session zero": {[]SessionPlanEntry{{0, "Morning", "1"}}, ErrValueOutOfRange},
		"blank label":  {[]SessionPlanEntry{{1, "  ", "1"}}, ErrMissingField},
		"zero share":   {[]SessionPlanEntry{{1, "Morning", "0"}, {2, "Evening", "1"}}, ErrNegativeValue},
		"too precise":  {[]SessionPlanEntry{{1, "Morning", "0.33333"}}, ErrInvalidDecimal},
		"not a number": {[]SessionPlanEntry{{1, "Morning", "half"}}, ErrInvalidDecimal},
		"too many":     {make([]SessionPlanEntry, MaxSessionsPerPark+1), ErrTooManySessions},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateSessionPlan(tc.plan)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}
