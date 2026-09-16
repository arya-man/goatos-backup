package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestEffectiveConfigAppliesDefaultsDropsUnknownAndClamps(t *testing.T) {
	out := EffectiveConfig([]StoredRuleConfig{
		{Key: RuleFeedLowStock, Enabled: false, Threshold: 500, UpdatedBy: "Ravi"},
		{Key: "made_up_rule", Enabled: true, Threshold: 1},
	})
	if len(out) != len(Rules()) {
		t.Fatalf("effective config is exactly the catalog, got %d rows", len(out))
	}
	byKey := map[RuleKey]RuleConfig{}
	for _, c := range out {
		byKey[c.Key] = c
	}
	pen := byKey[RulePenFeedQuantityChange]
	if !pen.Enabled || pen.Threshold != 1 || pen.Stored {
		t.Fatalf("unstored rule must run at its default, enabled: %+v", pen)
	}
	stock := byKey[RuleFeedLowStock]
	if stock.Enabled || stock.Threshold != 90 || !stock.Stored || stock.UpdatedBy != "Ravi" {
		t.Fatalf("stored row must apply, with an out-of-range threshold clamped to the catalog max: %+v", stock)
	}
	if _, ok := byKey["made_up_rule"]; ok {
		t.Fatalf("a stored row for an unknown rule must never invent a detector")
	}
}

func TestSetRuleConfigValidateRefusesUnknownAndOutOfRange(t *testing.T) {
	if err := (SetRuleConfig{Key: "nope", Threshold: 1}).Validate(); !errors.Is(err, ErrUnknownRule) {
		t.Fatalf("unknown rule must be refused, got %v", err)
	}
	if err := (SetRuleConfig{Key: RuleFeedLowStock, Threshold: 0}).Validate(); !errors.Is(err, ErrThresholdOutOfRange) {
		t.Fatalf("out-of-range threshold must be refused, never defaulted, got %v", err)
	}
	if err := (SetRuleConfig{Key: RuleFeedLowStock, Threshold: 7}).Validate(); err != nil {
		t.Fatalf("in-range write must pass, got %v", err)
	}
}

func TestLowStockAlertsNameFarmFeedAndDays(t *testing.T) {
	out := DetectLowStock("2026-09-16", map[string]string{"p1": "Channapatna"}, []LowStockFeed{
		{ParkID: "p1", FarmLabel: "CPT", FeedItemLabel: "Mesha Adult Concentrate", FeedItemKey: "mesha_adult_concentrate", BalanceKg: "120.0", AvgDailyKg: "80.0", DaysLeft: 1},
		{ParkID: "p1", FarmLabel: "CPT", FeedItemLabel: "Maize", FeedItemKey: "maize", BalanceKg: "400.0", AvgDailyKg: "100.0", DaysLeft: 4},
	})
	if len(out) != 2 || out[0].Severity != SeverityCritical || out[1].Severity != SeverityWarning {
		t.Fatalf("under two days is critical and sorts first, got %+v", out)
	}
	if out[0].Title != "Channapatna: Mesha Adult Concentrate lasts 1 more day(s)" {
		t.Fatalf("title must name park, feed and days: %q", out[0].Title)
	}
}

func TestSetEventRuleValidateAndDetectEventsCap(t *testing.T) {
	if err := (SetEventRule{Label: "x", Kind: "nope", Severity: SeverityWarning}).Validate(); !errors.Is(err, ErrUnknownEventKind) {
		t.Fatalf("unknown kind must be refused, got %v", err)
	}
	if err := (SetEventRule{Label: "  ", Kind: EventBirthRecorded, Severity: SeverityWarning}).Validate(); !errors.Is(err, ErrInvalidEventRule) {
		t.Fatalf("blank label must be refused, got %v", err)
	}
	if err := (SetEventRule{Label: "Kid born", Kind: EventBirthRecorded, Severity: "loud"}).Validate(); !errors.Is(err, ErrInvalidEventRule) {
		t.Fatalf("unknown severity must be refused, got %v", err)
	}
	rule := EventRule{ID: "r1", Label: "Kid born", Kind: EventBirthRecorded, Severity: SeverityWarning, Enabled: true}
	events := make([]Event, 0, 30)
	for i := 0; i < 30; i++ {
		events = append(events, Event{Key: fmt.Sprintf("g%d", i), ParkID: "p1", ShedName: "Yashoda 2", Subject: fmt.Sprintf("tag %d", i), Detail: "Litter of 1."})
	}
	out := DetectEvents("2026-09-16", "Coimbatore", rule, events)
	if len(out) != MaxEventRowsPerRule+1 {
		t.Fatalf("a bulk day caps at %d rows plus one 'more' row, got %d", MaxEventRowsPerRule, len(out))
	}
	first := out[0]
	if first.Title != "Kid born: tag 0" || first.OperationalLocationDisplay != "Yashoda 2" || first.RuleKey != "event:r1" || first.ParkLabel != "Coimbatore" {
		t.Fatalf("row copy/keys: %+v", first)
	}
	if last := out[len(out)-1]; !strings.Contains(last.Title, "5 more today") {
		t.Fatalf("summary row must count the overflow: %+v", last)
	}
}

func TestEventKeysRemainUniqueAcrossParksIncludingOverflow(t *testing.T) {
	rule := EventRule{ID: "r", Label: "Movement", Kind: EventShiftingRaised, Severity: SeverityWarning}
	seen := map[string]bool{}
	for _, park := range []string{"cbe", "cpt"} {
		events := make([]Event, MaxEventRowsPerRule+2)
		for i := range events {
			events[i] = Event{Key: fmt.Sprintf("shared-movement-%d", i), ParkID: park}
		}
		rows := DetectEvents("2026-09-16", park, rule, events)
		replay := DetectEvents("2026-09-16", park, rule, events)
		if len(rows) != MaxEventRowsPerRule+1 {
			t.Fatalf("unexpected cap: %d", len(rows))
		}
		for i, row := range rows {
			if seen[row.Key] {
				t.Fatalf("all-parks key collision: %s", row.Key)
			}
			if row.Key != replay[i].Key {
				t.Fatalf("key changed on reread: %s", row.Key)
			}
			seen[row.Key] = true
		}
	}
}
