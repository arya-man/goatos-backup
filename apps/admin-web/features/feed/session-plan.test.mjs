import { test } from "node:test";
import assert from "node:assert/strict";
import { fractionToPercent, parseSessionPlanForm, percentToFraction } from "./session-plan.ts";

test("a percentage becomes a four-place fraction without float drift", () => {
  assert.equal(percentToFraction("60"), "0.6000");
  assert.equal(percentToFraction("33.33"), "0.3333");
  assert.equal(percentToFraction("100"), "1.0000");
  assert.equal(percentToFraction("0.5"), "0.0050");
  for (const bad of ["", "0", "101", "33.333", "-5", "abc", "60%"]) {
    assert.equal(percentToFraction(bad), null, bad);
  }
  assert.equal(fractionToPercent("0.6000"), "60");
  assert.equal(fractionToPercent("0.3333"), "33.33");
  assert.equal(fractionToPercent("0.0050"), "0.5");
});

const form = (fields) => (name) => fields[name] ?? "";

test("the plan keeps named sessions, drops cleared ones, and must add up to 100%", () => {
  const ok = parseSessionPlanForm(form({
    session_count: "3",
    session_no_0: "1", session_label_0: " Morning ", session_share_0: "60",
    session_no_1: "2", session_label_1: "Evening", session_share_1: "40",
    session_no_2: "3", session_label_2: "", session_share_2: "",
  }));
  assert.deepEqual(ok, { ok: true, sessions: [
    { session_no: 1, session_label: "Morning", split_fraction: "0.6000" },
    { session_no: 2, session_label: "Evening", split_fraction: "0.4000" },
  ] });
  assert.deepEqual(
    parseSessionPlanForm(form({ session_count: "2", session_no_0: "1", session_label_0: "Morning", session_share_0: "50", session_no_1: "2", session_label_1: "Evening", session_share_1: "40" })),
    { ok: false, messageKey: "reason.session_split_not_whole" },
  );
  assert.deepEqual(parseSessionPlanForm(form({ session_count: "1", session_no_0: "1", session_label_0: "" })), { ok: false, messageKey: "reason.sessions_required" });
  assert.deepEqual(parseSessionPlanForm(form({ session_count: "1", session_no_0: "1", session_label_0: "All day", session_share_0: "" })), { ok: false, messageKey: "reason.session_share_invalid" });
});
