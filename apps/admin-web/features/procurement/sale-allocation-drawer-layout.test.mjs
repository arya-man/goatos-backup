import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

// Tag animals to sale on a 390px phone (2026-09-25): the global phone floor made both tagging
// tables 540px scrollers inside a 300px drawer (the weight step showed ")6" beside a clipped
// box), the 170px select floor pushed Park/Pen past the drawer's edge, and the close control sat
// beside the title instead of at the drawer's edge.
const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
const drawer = readFileSync(new URL("./sale-allocation-drawer.tsx", import.meta.url), "utf8");

test("the tagging tables opt out of the 540px phone floor", () => {
  assert.match(css, /aside\.sales-tagdrawer table\.sales-tagtable\{min-width:0\}/);
  assert.match(css, /\.sales-tagweights \.sales-tagtable td:first-child\{width:auto;[^}]*white-space:normal/);
});

test("the tagging filters fill their own column", () => {
  assert.match(css, /\.sales-tagfilters select,\.sales-tagfilters input\{width:100%;min-width:0\}/);
});

test("the close control sits at the drawer's edge", () => {
  assert.match(drawer, /<span className="sp" style=\{\{ flex: 1 \}\} \/>\s*<button ref=\{closeButtonRef\}/);
});

test("a card's bare empty line is padded and muted, at zero specificity", () => {
  assert.match(css, /:where\(\.card\)>:where\(\.empty\)\{padding:14px 16px;color:var\(--muted\)/);
});
