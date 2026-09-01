import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const panel = readFileSync(new URL("./anchor-panel.tsx", import.meta.url), "utf8");
const actions = readFileSync(new URL("./plan-actions.ts", import.meta.url), "utf8");
const editor = readFileSync(new URL("./plan-editor.tsx", import.meta.url), "utf8");
const server = readFileSync(new URL("../../lib/api/server.ts", import.meta.url), "utf8");

test("anchor panel defaults all safety flags to true", () => {
  assert.match(panel, /<input type="checkbox" checked readOnly \/>/);
  assert.match(panel, /suppressBeforeAnchor:\s*true/);
  assert.match(panel, /chainFutureFromAnchor:\s*true/);
  assert.match(panel, /enforceAgeEligibility:\s*true/);
});

test("anchor panel is a simple draft rule setting", () => {
  assert.match(panel, /Anchor\/base date/);
  assert.match(panel, /No anchor/);
  assert.match(panel, /Plus/);
  assert.match(panel, /Pencil/);
  assert.match(panel, /aria-label=\{anchorDate \? "Edit anchor" : "Add anchor"\}/);
  assert.doesNotMatch(panel, /CircleSlash/);
  assert.doesNotMatch(panel, /aria-label="Skip anchor"/);
  assert.doesNotMatch(panel, /onChange\?\.\(row\.rule\.dose_code, null\)/);
  assert.match(panel, /onChange\?\.\(row\.rule\.dose_code, state\)/);
  assert.match(panel, /disabled=\{!canSave\}/);
  assert.match(panel, /isValidIsoDate\(state\.anchorDate\)/);
  assert.doesNotMatch(panel, /setConfigured/);
  assert.match(panel, /Save anchor to draft/);
  assert.match(panel, /Apply anchor to this rule's eligible scope/);
  assert.match(panel, /Chain boosters\/revacs from anchor/);
  assert.match(panel, /Suppress earlier catch-up rows before anchor/);
  assert.match(panel, /Rule\/dose/);
  assert.doesNotMatch(panel, /Preview/);
  assert.doesNotMatch(panel, /previewAnchor/);
  assert.doesNotMatch(panel, /buildAnchorPayload/);
  assert.doesNotMatch(panel, /Clear anchor/);
  assert.doesNotMatch(panel, /Seed a completed vaccine date/);
  assert.doesNotMatch(panel, /Animal IDs/);
  assert.doesNotMatch(panel, /<select/);
  assert.doesNotMatch(panel, /<th>RFID<\/th>/);
  assert.doesNotMatch(panel, /animal\.identifier/);
  assert.doesNotMatch(panel, /Start this vaccine from this date/);
});

test("draft anchor row editor does not apply operational anchors directly", () => {
  assert.doesNotMatch(panel, /createAnchor/);
  assert.doesNotMatch(panel, /runCreate/);
  assert.doesNotMatch(actions, /publishPlanWithAnchors/);
  assert.doesNotMatch(editor, /publishPlanWithAnchors/);
  assert.match(editor, /publishPlan\(liveId\)/);
});

test("anchor API request shape still uses top-level booleans", () => {
  assert.doesNotMatch(panel, /flags:\s*{/);
  assert.match(server, /suppress_before_anchor\?: boolean/);
  assert.doesNotMatch(server, /flags\?:/);
});
