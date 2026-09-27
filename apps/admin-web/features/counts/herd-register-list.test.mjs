// Guard: herd-register-list-anatomy (TR1-#18/#19, REVIEW-36 O50). /counts/herd is the template user
// list: status Tabs with Label counts driving ?status=, the URL-sorted Display ID head (backend keyset
// order), the avatar lead cell, and a table sized to scroll inside its card's Scrollbar (no legacy
// 1600px min-width class). No checkbox column: the register has no bulk action (no dead controls).
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const page = readFileSync(new URL("./herd-register.tsx", import.meta.url), "utf8");
const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");

test("herd-register-list-anatomy: tabs, URL sort, lead cell, table in card", () => {
  assert.match(page, /<UrlTabs\b[\s\S]*?count: tab\.count/, "status tabs carry Label counts");
  assert.match(page, /HERD_STATUS_TABS = \["all", "alive", "dead", "sold", "culled"\]/);
  assert.match(page, /<UrlSortHead\b[\s\S]*?sortHrefs=/, "Display ID head sorts by URL");
  assert.match(page, /searchGoats\(\{[^}]*order/, "the sort reaches the backend");
  assert.match(page, /<Avatar\b/, "avatar lead cell");
  assert.match(page, /<Scrollbar\b[\s\S]*?<Table sx=\{\{ minWidth: 960/, "table scrolls inside the card");
  assert.doesNotMatch(page, /herd-register-table/, "no legacy table class");
  assert.doesNotMatch(css, /herd-register-table/, "legacy min-width rules deleted");
  assert.doesNotMatch(page, /onSelectAllRows|<Checkbox/, "no checkbox column without a bulk action");
});
