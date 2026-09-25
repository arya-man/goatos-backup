import assert from "node:assert/strict";
import test from "node:test";

import {
  draftFromShift,
  shiftLabelName,
  shiftRequestFromDraft,
  shiftSummary,
} from "./vaccination-operator-shift-form.ts";

test("stored shift codes are named in farm words, never shown raw", () => {
  assert.equal(shiftLabelName("am"), "Morning");
  assert.equal(shiftLabelName("pm"), "Afternoon");
  assert.equal(shiftLabelName("rover"), "Rover");
  assert.equal(shiftSummary({ operatorId: "o", shiftLabel: "rover", shiftStartMinute: 510, shiftEndMinute: 1020 }), "Rover · 08:30–17:00");
});

test("an existing shift opens as itself", () => {
  assert.deepEqual(
    draftFromShift({ operatorId: "o", shiftLabel: "pm", shiftStartMinute: 780, shiftEndMinute: 1260, weekOffWeekday: "sunday" }),
    { shiftLabel: "pm", shiftStart: "13:00", shiftEnd: "21:00", weekOffWeekday: "sunday" },
  );
});

test("a new shift opens empty, with no invented label or times", () => {
  assert.deepEqual(draftFromShift(undefined, "Tuesday"), { shiftLabel: "", shiftStart: "", shiftEnd: "", weekOffWeekday: "tuesday" });
  assert.deepEqual(draftFromShift(undefined, "—"), { shiftLabel: "", shiftStart: "", shiftEnd: "", weekOffWeekday: "" });
});

test("a blank week off is sent as null, and times are trimmed not reformatted", () => {
  assert.deepEqual(shiftRequestFromDraft("p", "o", { shiftLabel: "am", shiftStart: " 8:00 ", shiftEnd: "17:00", weekOffWeekday: "" }), {
    park_id: "p",
    operator_id: "o",
    shift_label: "am",
    shift_start: "8:00",
    shift_end: "17:00",
    week_off_weekday: null,
  });
});
