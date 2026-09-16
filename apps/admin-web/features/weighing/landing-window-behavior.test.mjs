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

test("seeded calendar opens July 5 while landing remains fixed on August 3", () => {
  assert.deepEqual(weightsWindowSettings(undefined, "2026-09-16"), {
    defaultFrom: "2026-08-03", earliestDate: "2026-07-05",
  });
});

test("rolling weeks count back from today, independently of the latest weighing", () => {
  const copy = {
    "weights.window.default_from_mode": "rolling_weeks",
    "weights.window.default_from_weeks": "6",
    "weights.window.earliest_date": "2026-07-05",
  };
  const settings = weightsWindowSettings(copy, "2026-09-16");
  assert.deepEqual(settings, { defaultFrom: "2026-08-05", earliestDate: "2026-07-05" });
  assert.deepEqual(windowThroughLatest("2026-09-16", "2026-09-15", settings), {
    from: "2026-08-05", to: "2026-09-15",
  });
  assert.equal(weightsWindowSettings(copy, "2026-09-17").defaultFrom, "2026-08-06");
  assert.equal(weightsWindowSettings({ ...copy, "weights.window.default_from_weeks": "2" }, "2026-09-16").defaultFrom, "2026-09-02");
  assert.equal(weightsWindowSettings({ ...copy, "weights.window.default_from_weeks": "20" }, "2026-09-16").defaultFrom, "2026-07-05");
});

test("rolling weeks handle leap days and year boundaries using calendar arithmetic", () => {
  const copy = {
    "weights.window.default_from_mode": "rolling_weeks",
    "weights.window.default_from_weeks": "1",
    "weights.window.earliest_date": "2020-01-01",
  };
  assert.equal(weightsWindowSettings(copy, "2028-03-07").defaultFrom, "2028-02-29");
  assert.equal(weightsWindowSettings(copy, "2027-01-03").defaultFrom, "2026-12-27");
});

test("invalid rolling week values cannot create an invalid page date", () => {
  for (const value of ["", "0", "-1", "1.5", "6oops", "Infinity", "521", "9999999999999999999999"]) {
    assert.equal(weightsWindowSettings({
      "weights.window.default_from_mode": "rolling_weeks",
      "weights.window.default_from_weeks": value,
    }, "2026-09-16").defaultFrom, "2026-08-03", value);
  }
});

test("published fixed date and earliest date overrides remain independent", () => {
  assert.deepEqual(weightsWindowSettings({
    "weights.window.default_from_mode": "fixed_date",
    "weights.window.default_from_date": "2026-07-20",
    "weights.window.earliest_date": "2026-07-10",
  }, "2026-09-16"), { defaultFrom: "2026-07-20", earliestDate: "2026-07-10" });
});
