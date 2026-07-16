import { test } from "node:test";
import assert from "node:assert";
import { markerTonesForDate } from "./calendar-marker-tones.ts";

test("markerTonesForDate: prioritizes deferred over normal drive marker", () => {
  assert.deepStrictEqual(
    markerTonesForDate(
      {
        date: "2026-07-18",
        event_count: 1,
        completed_count: 0,
        open_count: 1,
        drive_count: 1,
        due_count: 0,
        overdue_count: 0,
        deferred_count: 1,
      },
      [],
    ),
    ["deferred"],
  );
});

test("markerTonesForDate: distinguishes due drive from history-only date", () => {
  assert.deepStrictEqual(
    markerTonesForDate(
      {
        date: "2026-12-18",
        event_count: 1,
        completed_count: 0,
        open_count: 1,
        drive_count: 1,
        due_count: 1,
        overdue_count: 0,
        deferred_count: 0,
      },
      [],
    ),
    ["due"],
  );
  assert.deepStrictEqual(
    markerTonesForDate(
      {
        date: "2026-06-30",
        event_count: 3,
        completed_count: 3,
        open_count: 0,
        drive_count: 0,
        due_count: 0,
        overdue_count: 0,
        deferred_count: 0,
      },
      [],
    ),
    ["history"],
  );
});

test("markerTonesForDate: uses event drive summary when marker aggregate is sparse", () => {
  assert.deepStrictEqual(
    markerTonesForDate(undefined, [
      {
        event_id: "parkdrive:park:date",
        event_type: "vaccination_drive",
        owner_key: "pc",
        title: "Park vaccination drive",
        subtitle: "",
        aggregated: true,
        all_day: true,
        summary_primary: "",
        summary_secondary: "",
        summary_tertiary: "",
        status: "scheduled",
        severity: "info",
        due_at: "2026-07-18T00:00:00+05:30",
        window_start: null,
        window_end: null,
        timezone: "Asia/Kolkata",
        timezone_source: "tenant",
        park_id: null,
        park_code: null,
        shed_id: null,
        shed_name: null,
        cohort_id: null,
        cohort_name: null,
        target_type: "goat",
        target_count: 4,
        shed_count: 2,
        vaccine_count: 1,
        drive_count: 1,
        catch_up_count: 0,
        scheduled_count: 4,
        deferred_count: 2,
        review_count: 0,
        shed_labels: [],
        vaccine_labels: [],
        protocol_id: null,
        protocol_version_id: null,
        rule_id: null,
        vaccine_name: null,
        dose_code: null,
        source_backed: false,
        source_label: "",
        assignee_label: null,
        executor_role: null,
        verifier_label: null,
        reminder_state: "",
        primary_notification_channel: "",
        escalation_state: "",
        system: false,
        cross_cutting: false,
        links: {},
        drive_summary: {
          park_name: "CPT",
          due_date: "2026-07-18",
          shed_count: 2,
          sheds_completed: 0,
          vaccine_labels: ["FMD"],
          total_count: 4,
          completed_count: 0,
          remaining_count: 4,
          due_count: 2,
          overdue_count: 0,
          deferred_count: 2,
          total_animals: 4,
          completed_animals: 0,
          owner_label: "PC",
        },
      },
    ]),
    ["deferred"],
  );
});
