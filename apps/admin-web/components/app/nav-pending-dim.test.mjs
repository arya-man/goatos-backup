// guard: nav-pending-strip-exempt (REVIEW-16 O21). While a link navigation is in flight the shell
// dims the page body (app/frame.css `.wrap[data-nav-pending]` rule), but never the block holding the
// tab strip that is navigating: that strip must stay clickable and undimmed (alerts-page puts its
// UrlTabs inside a Card). The strips mark themselves busy with aria-busy on the MUI Tabs root.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

test("the data-nav-pending dim exempts a block containing a busy tab strip", () => {
  const css = read("../../app/frame.css");
  const rule = css.split("\n").find((l) => l.startsWith(".wrap[data-nav-pending]"));
  assert.ok(rule, "dim rule present");
  assert.match(rule, /:not\(:has\(\.MuiTabs-root\[aria-busy="true"\]\)\)/);
});

test("TemplateTabs and UrlTabs set aria-busy while their navigation is pending", () => {
  assert.match(read("./template-tabs.tsx"), /aria-busy=\{pending \|\| undefined\}/);
  assert.match(read("./url-tabs.tsx"), /aria-busy=\{pendingValue !== null \|\| undefined\}/);
});
