import assert from "node:assert/strict";
import test from "node:test";
import { windowThroughLatest, weightsWindowSettings } from "./landing-window-constants.ts";

test("an authored fixed start after the latest weighing keeps a valid empty period", () => {
  assert.deepEqual(windowThroughLatest("2026-09-16", "2026-09-01", {defaultFrom: "2026-09-10", earliestDate: "2026-08-01"}), {from: "2026-09-10", to: "2026-09-16"});
});
test("a rolling window still loads after an inactive week or an earliest-date clamp", () => {
  for (const earliest of ["2026-08-01", "2026-09-12"]) {
    const settings = weightsWindowSettings({"weights.window.default_from_mode":"rolling_days", "weights.window.default_from_days":"7", "weights.window.earliest_date":earliest}, "2026-09-16");
    assert.deepEqual(windowThroughLatest("2026-09-16", "2026-09-01", settings), {from: earliest === "2026-09-12" ? earliest : "2026-09-10", to:"2026-09-16"});
  }
});
test("latest weighing still closes a populated window and invalid or future dates use today", () => {
  const settings = {defaultFrom:"2026-09-10", earliestDate:"2026-08-01"};
  for (const [latest,to] of [["2026-09-10","2026-09-10"],["2026-09-15","2026-09-15"],["","2026-09-16"],["2026-09-20","2026-09-16"]]) {
    assert.deepEqual(windowThroughLatest("2026-09-16",latest,settings), {from:"2026-09-10",to});
  }
  assert.deepEqual(windowThroughLatest("2026-09-16","2026-09-01",{...settings,defaultFrom:"2026-10-01"}), {from:"2026-09-16",to:"2026-09-16"});
});
