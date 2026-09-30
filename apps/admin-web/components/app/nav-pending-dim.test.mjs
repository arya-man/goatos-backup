// guard: nav-pending-strip-exempt (REVIEW-16 O21, superseded by pending-dim). Nothing dims while a
// link navigation is in flight; the navigating tab strip marks itself busy with aria-busy on the MUI
// Tabs root.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { legacyCss } from "../../scripts/lib/legacy-css.mjs";

const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

// Superseded (Ravi 2026-09-28 "just switch and show shimmer"): the page body is never dimmed while a
// navigation is in flight; the shell and the URL panels show skeletons (guard: pending-dim).
test("no data-nav-pending dim rule is left in frame.css, and nothing sets the hook", () => {
  const css = legacyCss("frame");
  assert.doesNotMatch(css, /data-nav-pending|kit-navpend/);
  assert.doesNotMatch(read("../mesha-shell.tsx"), /LinkNavPending|kit-navpend/);
});

test("TemplateTabs and UrlTabs set aria-busy while their navigation is pending", () => {
  assert.match(read("./template-tabs.tsx"), /aria-busy=\{pending \|\| undefined\}/);
  assert.match(read("./url-tabs.tsx"), /aria-busy=\{pendingValue !== null \|\| undefined\}/);
});
