package app

import (
	"strings"
	"testing"
)

// TestFeedOverdueShiftingCopyMatchesEnforcedRule locks the Feed Direction / Feed Packing
// "overdue movement" copy to the timing rule the projection actually enforces.
//
// Maintainer decision 2026-07-27 retired the lead-day model: a movement now counts toward a shed's
// feed from the day it is AUTHORIZED (no lead, no priority branch — see
// counts/domain.FeedShiftingEffectiveBusinessDate), and it is flagged OVERDUE only once it has been
// standing open since before the packing day. The copy on the exact screen an operator uses to
// decide whether a shed is being fed for animals that have not arrived must state THAT rule, not the
// retired one. This test forbids the old lead-day sentence from drifting back and requires the copy
// to name the authorization-day rule, so the words and the engine cannot diverge.
func TestFeedOverdueShiftingCopyMatchesEnforcedRule(t *testing.T) {
	// Phrases from the retired lead-day wording. Their reappearance means the copy is describing a
	// lead the projection no longer applies.
	bannedRetiredPhrases := []string{
		"feed-effective date",
		"1 day after approval",
		"2 days",
		"emergency movements",
	}
	for _, routeID := range []string{"feed-direction", "feed-packing"} {
		copyMap := pageSpecificCopy(routeID)
		note, ok := copyMap["label.overdue_shifting_note"]
		if !ok || strings.TrimSpace(note) == "" {
			t.Fatalf("%s page copy is missing key %q", routeID, "label.overdue_shifting_note")
		}
		lower := strings.ToLower(note)
		for _, banned := range bannedRetiredPhrases {
			if strings.Contains(lower, strings.ToLower(banned)) {
				t.Errorf(
					"%s label.overdue_shifting_note = %q still contains the retired lead-day phrase %q — the projection no longer applies a lead, so this states a rule the engine does not",
					routeID, note, banned,
				)
			}
		}
		// The load-bearing half: the note must name the actual rule — a movement counts from the day
		// it is authorized. Without it, an operator cannot tell why a not-yet-moved animal is in the count.
		if !strings.Contains(lower, "authorized") {
			t.Errorf(
				"%s label.overdue_shifting_note = %q must say a movement counts toward the feed plan from the day it is authorized",
				routeID, note,
			)
		}
	}
}

// TestFeedBlockedCopyIsNeverReadAsZero is the medical/operational twin of the schema rule in
// migrations/postgres/000011_feed_ration_config.sql: an authored rate of 0 (K0/K1 kids on milk)
// and a MISSING rate are different states with opposite consequences. The database keeps them
// structurally distinct; the UI words have to as well, because "0 kg" on a blocked shed reads as
// a complete feed sheet for a shed that is about to be fed nothing by accident.
//
// This asserts the two copy strings exist, are distinct, and that the blocked one explicitly
// denies the zero reading rather than merely omitting it.
func TestFeedBlockedCopyIsNeverReadAsZero(t *testing.T) {
	for _, routeID := range []string{"feed-direction", "feed-packing", "feed-config"} {
		copyMap := pageSpecificCopy(routeID)

		blocked, ok := copyMap["label.blocked_note"]
		if !ok || strings.TrimSpace(blocked) == "" {
			t.Fatalf("%s page copy is missing key %q", routeID, "label.blocked_note")
		}
		zero, ok := copyMap["label.configured_zero_note"]
		if !ok || strings.TrimSpace(zero) == "" {
			t.Fatalf("%s page copy is missing key %q", routeID, "label.configured_zero_note")
		}
		if blocked == zero {
			t.Errorf("%s blocked and configured-zero copy are identical; the two states must read differently", routeID)
		}
		if !strings.Contains(strings.ToLower(blocked), "not") || !strings.Contains(strings.ToLower(blocked), "zero") {
			t.Errorf(
				"%s label.blocked_note = %q must explicitly say a blocked row is NOT zero — an operator reading a blank as 0 kg is the starvation path this wording exists to close",
				routeID, blocked,
			)
		}
		if !strings.Contains(strings.ToLower(zero), "0") {
			t.Errorf("%s label.configured_zero_note = %q must name the authored 0 rate", routeID, zero)
		}
	}
}

// TestFeedZeroOmissionCopyPromisesBlockedStaysVisible guards the disclosure that Feed Direction and
// Feed Packing print because they HIDE configured-zero item lines (an authored 0 g/head is a real
// instruction to feed none of that item, and a "pack 0.000 kg" line is noise on a working sheet).
//
// Hiding anything from an operational sheet creates a reading hazard: an operator who expects RGS
// Concentrate and does not find it has to be able to conclude "authored as zero" and NOT "the rate is
// missing". Those have opposite consequences — the second means the shed goes unfed. The copy is
// therefore only safe if it states BOTH halves: that zeros are omitted, and that a missing rate never
// is. This asserts the second half survives any future rewording, because a copy edit that drops it
// silently converts a documented omission into an unexplained one.
//
// It also pins the scope: feed-config is the AUTHORING surface and hides nothing, so it must not
// declare this key at all — the ration grid is exactly where a rate of 0 must stay visible and
// editable.
func TestFeedZeroOmissionCopyPromisesBlockedStaysVisible(t *testing.T) {
	for _, routeID := range []string{"feed-direction", "feed-packing"} {
		copyMap := pageSpecificCopy(routeID)

		omitted, ok := copyMap["label.zero_items_omitted"]
		if !ok || strings.TrimSpace(omitted) == "" {
			t.Fatalf("%s page copy is missing key %q — the sheet hides rows without saying so", routeID, "label.zero_items_omitted")
		}
		lower := strings.ToLower(omitted)
		// The omission itself must be stated.
		if !strings.Contains(lower, "0 g/head") && !strings.Contains(lower, "zero") {
			t.Errorf(
				"%s label.zero_items_omitted = %q must name WHAT is omitted (items authored at 0 g/head), or a reader cannot tell an omitted line from a missing one",
				routeID, omitted,
			)
		}
		// The load-bearing half: a blocked/unauthored item is never among the hidden rows.
		if !strings.Contains(lower, "never hidden") {
			t.Errorf(
				"%s label.zero_items_omitted = %q must promise that items with NO authored rate are NEVER hidden — without that sentence an absent line is ambiguous between 'fed none of it' and 'nobody said what to feed', which is the starvation reading",
				routeID, omitted,
			)
		}
		if !strings.Contains(lower, "no authored rate") && !strings.Contains(lower, "no ration configured") {
			t.Errorf(
				"%s label.zero_items_omitted = %q must name the blocked state it exempts from hiding",
				routeID, omitted,
			)
		}

		// The per-session empty state that replaces a shed whose every item was a configured zero.
		nothing, ok := copyMap["empty.nothing_to_feed"]
		if !ok || strings.TrimSpace(nothing) == "" {
			t.Fatalf("%s page copy is missing key %q — an all-zero shed would render as a blank hole", routeID, "empty.nothing_to_feed")
		}
	}

	// Feed Config authors rates; it must never hide a zero.
	configCopy := pageSpecificCopy("feed-config")
	if _, ok := configCopy["label.zero_items_omitted"]; ok {
		t.Errorf(
			"feed-config declares %q, but the ration grid is the authoring surface — a rate of 0 must stay visible and editable there, so nothing is omitted and no omission notice belongs on the page",
			"label.zero_items_omitted",
		)
	}
}

// TestFeedPagesDeclareRequiredCopy fails fast on the single most common way this contract breaks:
// the frontend calls copy(key) and it THROWS at render time when the key is absent. Every key
// listed here is a real product state the Feed pages must be able to name.
func TestFeedPagesDeclareRequiredCopy(t *testing.T) {
	required := map[string][]string{
		"feed-direction": {
			"crumb",
			"section.direction.title", "section.direction.aria", "section.direction.caption", "section.direction.note",
			"table.direction.aria", "table.direction.noun",
			"filter.all_option",
			"label.projected_count", "label.projected_count_note",
			"label.current_count", "label.current_count_note",
			"label.blocked", "label.blocked_note",
			"label.configured_zero", "label.configured_zero_note",
			"label.overdue_shifting", "label.overdue_shifting_note",
			"label.workflow_normal", "label.workflow_normal_note",
			"label.workflow_experiment", "label.workflow_experiment_note",
			"label.zero_items_omitted",
			"empty.direction", "empty.direction_filtered", "empty.nothing_to_feed",
			"state.direction_unavailable", "state.generation_blocked",
		},
		"feed-packing": {
			"crumb",
			"section.packing.title", "section.packing.aria", "section.packing.caption", "section.packing.note",
			"caption.feed_for",
			"table.packing.aria", "table.packing.noun",
			"filter.all_option",
			"label.blocked", "label.blocked_note",
			"label.configured_zero", "label.configured_zero_note",
			"label.overdue_shifting", "label.overdue_shifting_note",
			"label.workflow_normal", "label.workflow_normal_note",
			"label.workflow_experiment", "label.workflow_experiment_note",
			"label.zero_items_omitted",
			"empty.packing", "empty.packing_filtered", "empty.nothing_to_feed",
			"state.packing_unavailable", "state.generation_blocked",
		},
		"feed-config": {
			"crumb",
			"section.ration_grid.title", "section.shed_factors.title",
			"section.session_template.title", "section.schedule.title",
			"table.ration_grid.aria", "table.shed_factors.aria",
			"table.session_template.aria", "table.schedule.aria",
			"filter.all_option",
			"label.blocked", "label.blocked_note",
			"label.configured_zero", "label.configured_zero_note",
			"label.workflow_normal", "label.workflow_normal_note",
			"label.workflow_experiment", "label.workflow_experiment_note",
			"label.direction_time_note", "label.correction_time_note", "label.transport_time_note",
			"empty.ration_grid", "empty.shed_factors", "empty.session_template", "empty.schedule",
			"state.ration_grid_unavailable", "state.shed_factors_unavailable",
			"state.session_template_unavailable", "state.schedule_unavailable",
			// Experiment sheds.
			"section.experiment.title", "section.experiment.aria", "section.experiment.caption",
			"section.experiment.note", "section.experiment.switch_note",
			"table.experiment.aria", "table.experiment.noun",
			"label.experiment_absolute_kg", "label.experiment_absolute_kg_note",
			"label.experiment_head_count", "label.experiment_head_count_note",
			"label.experiment_category", "label.experiment_category_note",
			"label.experiment_active", "label.experiment_active_note",
			"label.experiment_retired", "label.experiment_retired_note",
			"label.experiment_not_dated_note",
			"action.add_experiment_pen", "action.add_experiment_item", "action.edit_experiment_cell",
			"action.withdraw_experiment_shed", "action.restore_experiment_shed",
			"action.experiment_saved", "action.experiment_switched",
			"reason.experiment_blank_is_not_zero", "reason.experiment_switch_consequence",
			"empty.experiment", "empty.experiment_filtered", "empty.experiment_candidates",
			"empty.experiment_items_authored",
			"notice.park_scope_fallback",
			// The pen enroller (one atomic write for a pen and every feed item of it) and the
			// company-wide scope chip.
			"filter.pen_label", "label.experiment_enrol_items", "label.experiment_enrol_items_note",
			"label.all_parks", "reason.experiment_enrol_park",
			"state.experiment_unavailable",
		},
	}

	for routeID, keys := range required {
		// pageCopy() is what the page actually receives (shared chrome + page-specific).
		copyMap := pageCopy(routeID)
		for _, key := range keys {
			value, ok := copyMap[key]
			if !ok || strings.TrimSpace(value) == "" {
				t.Errorf("%s page copy is missing required key %q — copy() throws at render time on a missing key", routeID, key)
			}
		}
	}
}

// feedTableColumnKeys returns one table's declared column keys in contract order, failing the test
// if the page or table is absent or if any column has an empty label. The frontend renders cells
// POSITIONALLY against these labels, so order is part of the contract, not a detail.
func feedTableColumnKeys(t *testing.T, routeID, tableID string) []string {
	t.Helper()
	for _, p := range pages() {
		if p.RouteID != routeID {
			continue
		}
		for _, tbl := range p.Tables {
			if tbl.ID != tableID {
				continue
			}
			keys := make([]string, 0, len(tbl.Columns))
			for _, col := range tbl.Columns {
				keys = append(keys, col.Key)
				if strings.TrimSpace(col.Label) == "" {
					t.Errorf("%s/%s column %q has an empty label", routeID, tableID, col.Key)
				}
			}
			return keys
		}
		t.Fatalf("%s page declares no %q table", routeID, tableID)
	}
	t.Fatalf("no page declares route %q", routeID)
	return nil
}

// TestFeedTableColumnsAreExact locks every Feed table's column key list, in order.
//
// Two separate defects live here. The first is POSITIONAL DRIFT: the Feed pages render cells
// positionally under `tableLabels(...)`, so a key added or removed in the contract without the
// matching <td> silently shifts every remaining header one column sideways — a quantity ends up
// under "Session total (kg)", which on this screen is a feeding instruction.
//
// The second is the reason `park` is absent from all six lists. Every Feed read endpoint
// (/feed-direction/generation-preview, /feed-packing/worklist and all four /feed-config/*) takes
// park_id as a REQUIRED parameter and scopes its SQL with `AND park_id = $2`. One request can only
// ever return one park, so a park column prints an identical value on every row while consuming
// width that the real content needs: on Feed Direction it was the difference between the
// "Mesha Concentrate Goat" label sitting on one line and wrapping to three, which grew the page
// 4125px -> 6022px. The pinned park is already named by each page's Park filter control. Re-adding
// a park column here is therefore a density regression, not a feature — if Feed ever gains a
// genuinely multi-park read, give that endpoint its own table contract.
func TestFeedTableColumnsAreExact(t *testing.T) {
	for _, tc := range []struct {
		routeID string
		tableID string
		want    []string
	}{
		// shed_tag and breed must BOTH stay, on every workflow. The defect they close is an
		// experiment row printing its trial arm ("Sheep M NEW") under shed_tag while the shed's
		// real tag ("F2-Male") and breed ("Anantapur Sheep") went unreported — one column meaning
		// "management stage" on normal rows and "trial group" on experiment rows is a column an
		// operator cannot read without first checking the workflow tag.
		//
		// experiment_arm is deliberately NOT a column: it is authoring context (which trial a shed
		// is enrolled in), not something that changes what gets weighed out, and it is empty on
		// every normal row. It is authored and shown on /feed/config. The row contract still
		// carries the field, so re-adding a column is a rendering decision, not a data change —
		// but it must never be re-merged into shed_tag.
		{"feed-direction", "direction-rows", []string{
			"shed", "shed_tag", "breed", "session", "head_count",
			"feed_item", "quantity_kg", "session_total_kg", "status",
		}},
		// The packing worklist deliberately declares NO descriptive columns — a packer's unit of
		// work is the bag, not the animal.
		{"feed-packing", "packing-worklist", []string{
			"shed", "session", "feed_item", "expected_kg", "status",
		}},
		{"feed-config", "ration-grid", []string{
			"ration_group", "shed_tag", "feed_item", "grams_per_head", "valid_from", "valid_to",
		}},
		{"feed-config", "shed-factors", []string{
			"shed", "feed_item", "multiplier", "valid_from", "valid_to",
		}},
		{"feed-config", "session-template", []string{
			"session_no", "session_label", "split_fraction", "status",
		}},
		// The experiment table has NO valid_from/valid_to pair, unlike every other effective-dated
		// table on this page — feed_experiment_config is not effective-dated (migration 000006), and
		// declaring the columns anyway would render two permanently empty cells that read as a
		// missing effective window rather than an absent concept.
		// PARK LEADS THIS ONE, and it is the only feed table that carries it. park_id became OPTIONAL
		// on GET /feed-config/experiment so a company-wide scope can show every authored pen instead of
		// silently showing one park's — and a cross-park list MUST name the park, because the shed name
		// cannot: Castro, Gandhi and Yashoda each exist in both parks. The ban below still holds for
		// every other table here, all of which do require park_id.
		{"feed-config", "experiment-config", []string{
			"park", "shed", "experiment_category", "informational_head_count", "feed_item", "absolute_kg", "status",
		}},
	} {
		got := feedTableColumnKeys(t, tc.routeID, tc.tableID)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf(
				"%s/%s columns = %v, want %v — cells are rendered positionally, so a contract change without the matching TSX cell shifts every header sideways",
				tc.routeID, tc.tableID, got, tc.want,
			)
		}
		// The park-column ban applies to every table whose endpoint REQUIRES park_id: it would repeat
		// one value on every row and take the width the feed-item label needs. experiment-config is
		// exempt because its park_id is optional and the list can legitimately span both parks; that
		// exemption is scoped by table id so it cannot leak to the four that are still park-owned.
		if tc.tableID != "experiment-config" {
			for _, key := range got {
				if key == "park" {
					t.Errorf(
						"%s/%s declares a %q column, but this endpoint requires park_id and returns exactly one park — the column would repeat one value on every row and take the width the feed-item label needs",
						tc.routeID, tc.tableID, key,
					)
				}
			}
		}
	}

	// schedule-config is asserted separately (see below) because its column set carries an extra
	// dispatch-time-vs-session-time rule; it must still be park-free.
	for _, key := range feedTableColumnKeys(t, "feed-config", "schedule-config") {
		if key == "park" {
			t.Errorf("feed-config/schedule-config declares a %q column; every /feed-config/* read is single-park by contract", key)
		}
	}
}

// TestFeedScheduleColumnsAreDispatchTimesNotSessionTimes locks the feed day clock table's column
// keys to the three fields the screen actually renders: feed_schedule_config.direction_time,
// .correction_time and .transport_time.
//
// This shipped wrong once. The table was declared with session-shaped keys
// ("session_no", "session_label", "start_time", "end_time") while the page rendered the three
// dispatch times underneath them, so a direction time appeared as "Session name: 14:00:00" and the
// remaining pair rendered as "Starts 14:00 / Ends 15:45" — which an operator reads as the window in
// which the ANIMALS ARE FED. It is not: the animals eat the next morning, and 14:00/15:45 are when
// an amended sheet is reissued and when it can no longer reach the shed. Someone trusting that
// header would time a correction against the wrong deadline.
//
// The column set is asserted exactly (order included) because the frontend renders cells
// positionally against these labels; a key added here without a matching cell silently shifts every
// header one column to the right, which is the same class of defect.
func TestFeedScheduleColumnsAreDispatchTimesNotSessionTimes(t *testing.T) {
	want := []string{"workflow", "direction_time", "correction_time", "transport_time", "status"}

	found := feedTableColumnKeys(t, "feed-config", "schedule-config")
	if strings.Join(found, ",") != strings.Join(want, ",") {
		t.Fatalf("schedule-config columns = %v, want %v — the header row must name the dispatch times the page renders, never session times", found, want)
	}

	// A session-shaped key on this table is the exact regression described above.
	for _, banned := range []string{"session_no", "session_label", "start_time", "end_time"} {
		for _, key := range found {
			if key == banned {
				t.Errorf("schedule-config declares %q — this table carries the feed day dispatch clock, not session times", banned)
			}
		}
	}

	// Each time column must be labelled by the ACT it performs. A bare "Starts"/"Ends" pair is what
	// made the old header read as a feeding window.
	for key, wantLabel := range map[string]string{
		"direction_time":  "Direction issued",
		"correction_time": "Corrections reissued",
		"transport_time":  "Transport cutoff",
	} {
		if got := humanLabel(key); got != wantLabel {
			t.Errorf("humanLabel(%q) = %q, want %q", key, got, wantLabel)
		}
	}
}

// TestFeedScheduleNoteDoesNotReadAsFeedingTime guards the helper copy under the same table. The
// operational point of the whole section is that these clock times are when the SHEET moves, not
// when animals eat — the note has to say so, or it re-creates the misreading the headers just fixed.
func TestFeedScheduleNoteDoesNotReadAsFeedingTime(t *testing.T) {
	copyMap := pageSpecificCopy("feed-config")
	note := strings.ToLower(copyMap["section.schedule.note"])
	if note == "" {
		t.Fatal("feed-config is missing section.schedule.note")
	}
	if !strings.Contains(note, "tomorrow") && !strings.Contains(note, "next") {
		t.Errorf("section.schedule.note = %q must say the direction is issued for the NEXT feed day; without it the times read as today's feeding window", copyMap["section.schedule.note"])
	}
}

// TestExperimentHeadCountIsNeverPresentedAsAMultiplier is the experiment-shed twin of
// TestFeedBlockedCopyIsNeverReadAsZero, and it guards the single most consequential confusion this
// screen can create.
//
// feed_experiment_config.absolute_kg is a SHED TOTAL, already inclusive of every animal in the shed.
// The head count beside it is context for the operator who authored the figure, NOT a multiplier —
// multiplying the two would overfeed the shed by a factor of its entire population. That is the
// opposite of Feed Direction, where head_count genuinely IS the first term of
// head count x grams per head x shed factor.
//
// Because humanLabel() is keyed by column key alone and has no table context, the two screens are
// kept apart by USING DIFFERENT KEYS ("informational_head_count" here, "head_count" there). This
// test locks both halves of that arrangement: the experiment header must carry the warning, and Feed
// Direction's must NOT — a well-meaning edit that "unified" the two keys would either deny the
// multiplication that really happens on Feed Direction, or assert one that must never happen here.
func TestExperimentHeadCountIsNeverPresentedAsAMultiplier(t *testing.T) {
	if got := humanLabel("informational_head_count"); !strings.Contains(strings.ToLower(got), "informational") {
		t.Errorf(
			"humanLabel(%q) = %q must mark the count as informational — on an experiment row the kg is already a shed total, so a header that reads like Feed Direction's multiplier invites someone to multiply it",
			"informational_head_count", got,
		)
	}
	// The shared key must stay the plain multiplier label for Feed Direction's sake.
	if got := humanLabel("head_count"); strings.Contains(strings.ToLower(got), "informational") {
		t.Errorf(
			"humanLabel(%q) = %q — this key labels Feed Direction's head count, where the value IS multiplied by the ration rate; the informational wording belongs only to %q",
			"head_count", got, "informational_head_count",
		)
	}

	copyMap := pageSpecificCopy("feed-config")

	// The hover note must actively DENY the multiplication rather than merely omitting it, for the
	// same reason label.blocked_note must deny the zero reading: an operator fills a silence with the
	// behaviour they know from the other Feed screens.
	headNote := strings.ToLower(copyMap["label.experiment_head_count_note"])
	if headNote == "" {
		t.Fatal("feed-config is missing label.experiment_head_count_note")
	}
	if !strings.Contains(headNote, "not a multiplier") && !strings.Contains(headNote, "never multiplied") {
		t.Errorf(
			"label.experiment_head_count_note = %q must state outright that the count is NOT a multiplier",
			copyMap["label.experiment_head_count_note"],
		)
	}

	kgNote := strings.ToLower(copyMap["label.experiment_absolute_kg_note"])
	if kgNote == "" {
		t.Fatal("feed-config is missing label.experiment_absolute_kg_note")
	}
	if !strings.Contains(kgNote, "not a per-head") && !strings.Contains(kgNote, "whole shed") && !strings.Contains(kgNote, "shed total") {
		t.Errorf(
			"label.experiment_absolute_kg_note = %q must say the quantity is a whole-shed total rather than a per-head figure",
			copyMap["label.experiment_absolute_kg_note"],
		)
	}
}

// TestExperimentSwitchCopyStatesTheFeedingConsequence guards the copy behind the add/remove control.
//
// Membership in feed_experiment_config IS the workflow flag — there is nothing else to consult (see
// ExperimentPlanner.Applies). So moving a shed in or out of this section is not a filing decision,
// it changes the arithmetic that feeds the animals: absolute authored kg one way, projected head
// count x grams per head x shed factor the other. Measured on the live 2026-07-20 data, the
// difference for CBE was 182.0 kg vs 398.8 kg of concentrate.
//
// If the UI ever describes that switch in neutral list-management language ("add", "remove") without
// the consequence attached, an operator tidying a list would silently re-plan a park's feed. This
// test requires the section note and the switch reason to name the consequence explicitly.
func TestExperimentSwitchCopyStatesTheFeedingConsequence(t *testing.T) {
	copyMap := pageSpecificCopy("feed-config")

	switchNote := strings.ToLower(copyMap["section.experiment.switch_note"])
	if switchNote == "" {
		t.Fatal("feed-config is missing section.experiment.switch_note")
	}
	// It must say membership IS the mechanism, so nobody goes looking for a separate flag to set.
	if !strings.Contains(switchNote, "no other reason") && !strings.Contains(switchNote, "no separate flag") {
		t.Errorf(
			"section.experiment.switch_note = %q must say that being listed here is what puts a shed on the experiment workflow; otherwise the control reads as a filing action",
			copyMap["section.experiment.switch_note"],
		)
	}
	// Withdrawal keeps the authored quantities. Saying so is what stops an operator treating
	// "return to normal grid" as destructive and hoarding stale rows to avoid it.
	if !strings.Contains(switchNote, "keeps") && !strings.Contains(switchNote, "kept") {
		t.Errorf(
			"section.experiment.switch_note = %q must say the authored quantities are kept when a shed is returned to the normal grid",
			copyMap["section.experiment.switch_note"],
		)
	}

	consequence := strings.ToLower(copyMap["reason.experiment_switch_consequence"])
	if consequence == "" {
		t.Fatal("feed-config is missing reason.experiment_switch_consequence")
	}
	if !strings.Contains(consequence, "eat") && !strings.Contains(consequence, "fed") {
		t.Errorf(
			"reason.experiment_switch_consequence = %q must name the feeding consequence, not describe a display setting",
			copyMap["reason.experiment_switch_consequence"],
		)
	}

	// The retired label must name the arithmetic a withdrawn shed falls back to. "Retired"/"inactive"
	// alone would not tell an operator that the shed is now being fed per head from the grid above.
	retired := strings.ToLower(copyMap["label.experiment_retired_note"])
	if retired == "" {
		t.Fatal("feed-config is missing label.experiment_retired_note")
	}
	if !strings.Contains(retired, "grid") {
		t.Errorf(
			"label.experiment_retired_note = %q must say a withdrawn shed is fed from the ration grid again",
			copyMap["label.experiment_retired_note"],
		)
	}
}

// TestFeedOptionGroupsCarryNoLiveTenantData guards the rule at service.go's feedOptionGroups
// comment: only FIXED SCHEMA CONSTRAINTS may be declared in contract code. Parks, sheds, breeds,
// ration groups, shed tags and feed items are live tenant data and must arrive through the
// response facets or ReferenceFamilies, never as constants here.
func TestFeedOptionGroupsCarryNoLiveTenantData(t *testing.T) {
	allowed := map[string]bool{
		"feed_workflow":             true,
		"feed_row_status":           true,
		"feed_quantity_states":      true,
		"feed_effective_window":     true,
		"feed_shed_tag_applies_to":  true,
		"feed_config_status":        true,
		"feed_quantity_units":       true,
		"feed_generation_readiness": true,
		// The grams comparison vocabulary. Fixed, not live: these six keys ARE the grams_op enum
		// that /feed-config/ration-rates accepts, so the set changes only when that contract does.
		// Note what is deliberately NOT here — the VALUES compared against are typed by the author,
		// never enumerated, because an authored rate is live data.
		"feed_grams_compare": true,
	}

	for _, group := range feedOptionGroups() {
		if !allowed[group.ID] {
			t.Errorf(
				"feedOptionGroups declares %q, which is not a known fixed schema constraint — live vocabularies (parks, sheds, breeds, ration groups, shed tags, feed items, sessions) must come from facets or ReferenceFamilies",
				group.ID,
			)
		}
		if len(group.Options) == 0 {
			t.Errorf("feed option group %q declares no options", group.ID)
		}
	}

	// Session numbers are authored per park in feed_session_templates; the only schema rule is
	// session_no >= 1. Freezing today's two-session configuration into the contract would
	// mislabel any park that later runs a third session.
	for _, group := range feedOptionGroups() {
		if strings.Contains(group.ID, "session") {
			t.Errorf("feed option group %q hardcodes session vocabulary — sessions are authored per park in feed_session_templates", group.ID)
		}
	}
}
