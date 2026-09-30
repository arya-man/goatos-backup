// A verifier may leave a note on an ACCEPTED video, not only a reason on a rejected one
// (maintainer request 2026-09-08). The backend has always stored `reason` on either decision;
// the web drawer hid the box until Reject was pressed and the server action dropped the value
// on an approve, so an accepted proof could never carry the verifier's words.
//
// Source-shape tests, like every other test in this folder: there is no DOM harness here.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const drawerSource = readFileSync(new URL("./verification-review-drawer.tsx", import.meta.url), "utf8");
const actionsSource = readFileSync(new URL("./actions.ts", import.meta.url), "utf8");

test("the note box is always rendered, not revealed only by Reject", () => {
  assert.doesNotMatch(
    drawerSource,
    /display:\s*rejecting\s*\?\s*"grid"\s*:\s*"none"/,
    "the reason field must not be hidden until Reject is pressed -- an approval can carry a note too",
  );
  assert.match(drawerSource, /<TextField\s+fullWidth\s+multiline[\s\S]{0,200}name="reason"/, "the verdict form keeps its reason field (template multiline TextField)");
});

test("the server action sends the note on an approve when one was typed", () => {
  assert.match(
    actionsSource,
    /\.\.\.\(reason \? \{ reason \} : \{\}\)/,
    "reason must be forwarded whenever present, regardless of decision",
  );
  assert.doesNotMatch(
    actionsSource,
    /\.\.\.\(decision === "rejected" \? \{ reason \} : \{\}\)/,
    "the reject-only forwarding is the defect being replaced",
  );
  // The reject rule is untouched: a blank reason still bounces before any request is made.
  assert.match(actionsSource, /if \(decision === "rejected" && !reason\) \{\s*redirect\(withFeedback\(url, "error", "missing_reason"\)\);/);
});

test("the server action keys idempotency to the exact verdict payload", () => {
  assert.match(
    actionsSource,
    /const request = \{[\s\S]{0,600}\.\.\.\(reason \? \{ reason \} : \{\}\)[\s\S]{0,600}\};/,
    "the request object should be assembled before deriving the idempotency key",
  );
  assert.match(
    actionsSource,
    /verification-verdict-\$\{itemId\}-\$\{rowVersion\}-\$\{decision\}-\$\{verdictRequestFingerprint\(request\)\}/,
    "the key must change when the optional approval note changes",
  );
});

test("a typed visible note lets Reject submit on the first click", () => {
  assert.match(
    drawerSource,
    /type=\{reasonReady \? "submit" : "button"\}/,
    "once the always-visible note has text, Reject should submit without a second arming click",
  );
  assert.doesNotMatch(
    drawerSource,
    /type=\{rejecting && reasonReady \? "submit" : "button"\}/,
    "rejecting mode alone should not gate submit now that the note box is always visible",
  );
});

test("the drawer shows the verifier's words on an approved item too", () => {
  assert.doesNotMatch(
    drawerSource,
    /item\.status === "rejected" && item\.verdict_reason/,
    "verdict_reason must render for either decision, not only a rejection",
  );
  assert.match(drawerSource, /\{item\.verdict_reason && \(/);
});
