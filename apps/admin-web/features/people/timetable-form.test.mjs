import assert from "node:assert/strict";
import test from "node:test";

import { draftFromTiming, hourLabel, minuteOptions, timingRequestFromDraft } from "./timetable-form.ts";

test("hours read the way the farm says them", () => {
  assert.equal(hourLabel(0), "12 am");
  assert.equal(hourLabel(6), "6 am");
  assert.equal(hourLabel(12), "12 pm");
  assert.equal(hourLabel(15), "3 pm");
  assert.equal(hourLabel(23), "11 pm");
});

test("a stored timing opens as itself; midnight opens as 12 am; an unset end opens empty", () => {
  assert.deepEqual(draftFromTiming(510, 1080), { start: { hour: "8", minute: "30" }, end: { hour: "18", minute: "0" } });
  assert.deepEqual(draftFromTiming(900, 1440), { start: { hour: "15", minute: "0" }, end: { hour: "0", minute: "0" } });
  assert.deepEqual(draftFromTiming(360, null), { start: { hour: "6", minute: "0" }, end: { hour: "", minute: "" } });
  assert.deepEqual(draftFromTiming(null, null), { start: { hour: "", minute: "" }, end: { hour: "", minute: "" } });
});

test("the form round-trips the three stated shifts, and 12 am as an END is midnight", () => {
  for (const [start, end] of [[510, 1080], [900, 1440], [360, null], [1320, 360]]) {
    const result = timingRequestFromDraft(draftFromTiming(start, end));
    assert.deepEqual(result, { ok: true, body: { start_minute: start, end_minute: end } });
  }
});

test("a start is required before saving", () => {
  assert.deepEqual(timingRequestFromDraft(draftFromTiming(null, null)), { ok: false, reason: "start_required" });
});

test("an off-step stored minute stays selectable", () => {
  assert.ok(minuteOptions("7").includes(7));
  assert.equal(minuteOptions("30").length, 12);
});
