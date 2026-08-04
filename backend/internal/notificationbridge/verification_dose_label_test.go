package notificationbridge

import (
	"context"
	"strings"
	"testing"
)

// Regression test for the vaccine-label degradation (#19): production never sends a
// protocol_rules.rule_id in VerificationEventPayload.Category -- sopbridge stamps the fixed
// registry category "vaccination_proof" and puts the identity of the work in Source.TaskID. The
// enrichment keyed the dose lookup off Category alone, so every real vaccination approval fell
// through to the module-generic "vaccination proof for ... is verified." and no director was ever
// told WHICH dose was verified.
//
// These fakes stand in for the pool-backed resolvers so the copy decision is provable without a
// database: the point under test is WHICH key the enrichment looks the dose up by, not SQL.

type fakeLocationNames map[string]string

func (f fakeLocationNames) ResolveNames(_ context.Context, _ string, ids ...string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		if name, ok := f[id]; ok {
			out[id] = name
		}
	}
	return out
}

type fakeVaccineLabels struct {
	byRuleID  map[string]string
	byTaskID  map[string][]string
	ruleCalls []string
	taskCalls []string
}

func (f *fakeVaccineLabels) ResolveVaccineLabels(_ context.Context, _ string, ruleIDs ...string) map[string]string {
	f.ruleCalls = append(f.ruleCalls, ruleIDs...)
	out := map[string]string{}
	for _, id := range ruleIDs {
		if label, ok := f.byRuleID[id]; ok {
			out[id] = label
		}
	}
	return out
}

func (f *fakeVaccineLabels) ResolveVaccineLabelsForTask(_ context.Context, _ string, sopTaskID string) []string {
	f.taskCalls = append(f.taskCalls, sopTaskID)
	return f.byTaskID[sopTaskID]
}

const (
	doseTenant = "aa000000-0000-4000-8000-0000000000d1"
	dosePark   = "aa000000-0000-4000-8000-0000000000d2"
	doseShed   = "aa000000-0000-4000-8000-0000000000d3"
	doseTask   = "aa000000-0000-4000-8000-0000000000d4"
	doseRule   = "aa000000-0000-4000-8000-0000000000d5"
)

func doseLocations() fakeLocationNames {
	return fakeLocationNames{dosePark: "CPT", doseShed: "Shed A"}
}

// TestApprovedCopyNamesDoseFromSourceTaskWhenCategoryIsRegistryCategory is the failing-before case:
// the payload production actually emits (category="vaccination_proof", Source.TaskID set).
func TestApprovedCopyNamesDoseFromSourceTaskWhenCategoryIsRegistryCategory(t *testing.T) {
	labels := &fakeVaccineLabels{byTaskID: map[string][]string{doseTask: {"ET", "TT"}}}

	body := enrichApprovedNotificationCopy(context.Background(), doseLocations(), labels, nil,
		doseTenant, "vaccination", dosePark, doseShed, "vaccination_proof", doseTask)

	want := "ET+TT vaccination proof for Shed A (CPT) is verified."
	if body != want {
		t.Fatalf("approved body = %q, want %q", body, want)
	}
	if len(labels.taskCalls) != 1 || labels.taskCalls[0] != doseTask {
		t.Fatalf("dose lookup by sop task = %v, want exactly one lookup of %s", labels.taskCalls, doseTask)
	}
	// The registry category is not a rule id; firing that lookup would be a guaranteed miss.
	if len(labels.ruleCalls) != 0 {
		t.Fatalf("rule-id lookup fired for a non-rule-id category: %v", labels.ruleCalls)
	}
}

// TestApprovedCopyStillPrefersRealRuleIDWhenSent guards the cheaper, more precise key: if a
// producer ever puts a real rule id in category, that single indexed lookup must still win.
func TestApprovedCopyStillPrefersRealRuleIDWhenSent(t *testing.T) {
	labels := &fakeVaccineLabels{
		byRuleID: map[string]string{doseRule: "PPR · Booster"},
		byTaskID: map[string][]string{doseTask: {"ET"}},
	}

	body := enrichApprovedNotificationCopy(context.Background(), doseLocations(), labels, nil,
		doseTenant, "vaccination", dosePark, doseShed, doseRule, doseTask)

	if !strings.HasPrefix(body, "PPR · Booster vaccination proof") {
		t.Fatalf("approved body = %q, want the rule-id label to win", body)
	}
	if len(labels.taskCalls) != 0 {
		t.Fatalf("sop-task lookup fired even though the rule id resolved: %v", labels.taskCalls)
	}
}

// TestApprovedCopyFallsBackToGenericWhenTaskHasNoDoses proves the degrade is still honest: an
// unresolvable dose yields the module-generic sentence, never a blank or half-built phrase.
func TestApprovedCopyFallsBackToGenericWhenTaskHasNoDoses(t *testing.T) {
	labels := &fakeVaccineLabels{}

	body := enrichApprovedNotificationCopy(context.Background(), doseLocations(), labels, nil,
		doseTenant, "vaccination", dosePark, doseShed, "vaccination_proof", doseTask)

	if body != "vaccination proof for Shed A (CPT) is verified." {
		t.Fatalf("approved body = %q, want the generic vaccination sentence", body)
	}
}
