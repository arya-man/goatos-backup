import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { overlayJourneys, readOnlyClickRefusal, WRITE_LABEL_PATTERN } from "./overlay-journeys.mjs";

const smokeSource = readFileSync(new URL("../smoke-visual-live.mjs", import.meta.url), "utf8");

test("guard refuses every write-shaped label", () => {
  for (const label of ["Save", "Approve", "Reject", "Delete row", "Retire", "Submit", "Upload CSV", "Download", "Export file", "Assign", "Mark reached", "Confirm", "Create vendor", "Add", "+ Add item"]) {
    assert.ok(readOnlyClickRefusal({ text: label }), `text ${label}`);
    assert.ok(readOnlyClickRefusal({ ariaLabel: label }), `aria ${label}`);
  }
});

test("guard allows read-only labels", () => {
  for (const label of ["Open task 12: Clean pen", "Filters", "Access — Ravi", "Assumptions", "Notifications", "Close", "Address book", "SF-048"]) {
    assert.equal(readOnlyClickRefusal({ text: label }), null, label);
  }
  assert.ok(WRITE_LABEL_PATTERN.test("add"));
  assert.ok(!WRITE_LABEL_PATTERN.test("Address"));
});

test("dialog-trigger exemption only covers links that declare aria-haspopup=dialog", () => {
  const opts = { allowDialogTrigger: true };
  assert.equal(readOnlyClickRefusal({ text: "Download", tagName: "A", ariaHaspopup: "dialog" }, opts), null);
  assert.ok(readOnlyClickRefusal({ text: "Download", tagName: "BUTTON", ariaHaspopup: "dialog" }, opts));
  assert.ok(readOnlyClickRefusal({ text: "Download", tagName: "A", ariaHaspopup: "" }, opts));
  assert.ok(readOnlyClickRefusal({ text: "Download", tagName: "A", ariaHaspopup: "dialog" }));
});

test("every journey targets a route in the live smoke list and names real source files", () => {
  for (const [route, steps] of Object.entries(overlayJourneys)) {
    assert.ok(smokeSource.includes(`name: "${route}"`), `route ${route} missing from smoke-visual-live.mjs`);
    for (const step of steps) {
      for (const key of ["id", "trigger", "overlay", "kind", "source"]) assert.ok(step[key], `${route}:${step.id} lacks ${key}`);
      assert.match(step.id, /^[a-z0-9-]+$/);
    }
  }
});

test("smoke calls exerciseOverlays right after manifest safe clicks", () => {
  assert.match(smokeSource, /exerciseManifestSafeClicks\(page, route\.name, viewport\.label\)\);\s*\n\s*await check\(\(\) => exerciseOverlays\(page,/);
});

test("people overlays are required journeys with live selectors; opening never scrolls (guard: overlay-no-scroll-jump)", async () => {
  const { readFileSync } = await import("node:fs");
  const read = (rel) => readFileSync(new URL(`../../${rel}`, import.meta.url), "utf8");
  const people = Object.fromEntries(overlayJourneys.people.map((step) => [step.id, step]));
  for (const id of ["person-access-modal", "person-add-drawer"]) assert.equal(people[id]?.required, true, `${id} must be required`);
  // Legacy kit classes are gone from the people feature; a trigger that still names them silently skips.
  for (const step of overlayJourneys.people) assert.doesNotMatch(step.trigger, /\.(btn|ghost|iconbtn)\b/, `${step.id} uses a legacy class`);
  assert.match(read("features/people/person-access-launcher.tsx"), /aria-label=\{`\$\{copy\(pageContract, "access\.open"\)\} — /);
  // On All People the drawer opens in place (LocalOverlayLink); the other desks navigate there with the
  // drawer open (J3 P1-1: the action stays on every desk so the tab strip never moves).
  assert.match(read("features/people/people-add-button.tsx"), /component=\{overlay \? LocalOverlayLink : Link\}[\s\S]*aria-haspopup="dialog"/);
  const runner = read("scripts/lib/overlay-journeys.mjs");
  assert.match(runner, /if \(step\.required\) throw new Error/);
  assert.match(runner, /page jumped on open/);
});
