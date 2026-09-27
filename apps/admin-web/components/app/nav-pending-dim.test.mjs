// guard: nav-pending-strip-exempt (REVIEW-16 O21). While a link navigation is in flight the shell
// dims the page body (app/frame.css `.wrap[data-nav-pending]` rule), but never the block holding the
// tab strip that is navigating: that strip must stay clickable and undimmed (alerts-page puts its
// UrlTabs inside a Card). The strips mark themselves busy with aria-busy on the MUI Tabs root.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

// Superseded (Ravi 2026-09-28 "just switch and show shimmer"): the page body is never dimmed while a
// navigation is in flight; the shell and the URL panels show skeletons (guard: pending-dim).
test("no data-nav-pending dim rule is left in frame.css", () => {
  const css = read("../../app/frame.css");
  assert.equal(css.split("\n").some((l) => l.startsWith(".wrap[data-nav-pending]")), false);
});

test("TemplateTabs and UrlTabs set aria-busy while their navigation is pending", () => {
  assert.match(read("./template-tabs.tsx"), /aria-busy=\{pending \|\| undefined\}/);
  assert.match(read("./url-tabs.tsx"), /aria-busy=\{pendingValue !== null \|\| undefined\}/);
});
