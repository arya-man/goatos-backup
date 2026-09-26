import { test } from "node:test";
import assert from "node:assert/strict";
import shell, { TEMPLATE_SHELL, evaluateShell, intersects, isLinkColour } from "./shell.mjs";

const good = {
  failedSheets: [],
  nav: { left: 0, items: [
    { kind: "subheader", text: "OVERVIEW", x: 16, pl: 12, active: false, bgAlpha: 0 },
    { kind: "item", text: "Verify", x: 16, pl: 12, active: true, bg: "rgba(118, 176, 65, 0.08)", bgAlpha: 0.08 },
  ] },
  headerButtons: [{ label: "Switch to light theme", sig: "button.MuiIconButton-root", bg: "rgba(0, 0, 0, 0)", bgAlpha: 0, border: 0 }],
  tabsOverlaps: [],
  controlOverlaps: [],
  sortHeaders: [{ text: "Captured", color: "rgb(255, 255, 255)", underline: false, sig: "span.MuiTableSortLabel-root" }],
};

test("the template shell (what a good build of 77ed32f5b measures) passes", () => {
  assert.deepEqual(evaluateShell(good), []);
  assert.equal(shell.name, "shell");
  assert.equal(shell.p0, true);
});

test("Ravi 01:17 regression — UA defaults from a half-swapped build — fails every shell check", () => {
  // measured on :3450 with the /_next/static css blocked: 40px list indent, grey buttonface squares
  const bad = {
    failedSheets: ["_next/static/chunks/09o_mjgrwqg31.css (HTTP 404)"],
    nav: { left: 0, items: [
      { kind: "subheader", text: "OVERVIEW", x: 56, pl: 12, active: false, bgAlpha: 0 },
      { kind: "item", text: "Verify", x: 96, pl: 12, active: true, bg: "rgb(40, 60, 30)", bgAlpha: 1 },
    ] },
    headerButtons: [{ label: "Switch to light theme", sig: "button.iconbtn", bg: "rgb(107, 107, 107)", bgAlpha: 1, border: 2 }],
    tabsOverlaps: [{ what: "label.MuiInputLabel-root \"Capture date\"", h: 6, detail: "x" }],
    controlOverlaps: [{ a: "div.MuiFormControl-root", b: "div.MuiFormControl-root", w: 30, h: 20 }],
    sortHeaders: [{ text: "Captured", color: "rgb(0, 0, 238)", underline: true, sig: "a" }],
  };
  const got = new Set(evaluateShell(bad).map((f) => f.pattern));
  for (const p of ["stylesheet-failed", "nav-item-left", "nav-active-opaque", "header-button-bg", "tabs-overlap", "controls-overlap", "sort-header-link"]) assert.ok(got.has(p), `missing ${p}`);
  assert.ok(evaluateShell(bad).every((f) => f.p0));
});

test("nav padding off the template 12px fails; 1px rounding does not", () => {
  const m = structuredClone(good);
  m.nav.items[1].pl = 20;
  m.nav.items[0].x = 16.8;
  assert.deepEqual(evaluateShell(m).map((f) => f.pattern), ["nav-item-padding"]);
  assert.equal(TEMPLATE_SHELL.navPadX + TEMPLATE_SHELL.itemPadLeft, 28, "item content starts on the logo column (28px)");
});

test("link colour: UA blues and visited purple yes; Mesha green, info cyan and neutrals no", () => {
  for (const c of ["rgb(0, 0, 238)", "rgb(158, 158, 255)", "rgb(85, 26, 139)"]) assert.ok(isLinkColour(c), c);
  for (const c of ["rgb(118, 176, 65)", "rgb(0, 184, 217)", "rgb(145, 158, 171)", "rgb(255, 255, 255)", "rgb(28, 37, 46)"]) assert.ok(!isLinkColour(c), c);
});

test("intersects ignores touching edges", () => {
  assert.equal(intersects({ x: 0, y: 0, w: 10, h: 10 }, { x: 10, y: 0, w: 10, h: 10 }), null);
  assert.deepEqual(intersects({ x: 0, y: 0, w: 10, h: 10 }, { x: 5, y: 5, w: 10, h: 10 }), { w: 5, h: 5 });
});
