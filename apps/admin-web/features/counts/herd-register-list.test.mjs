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
  assert.match(page, /<Scrollbar\b[\s\S]*?<Table sx=\{HERD_TABLE_SX\}/, "table scrolls inside the card");
  assert.match(page, /const HERD_TABLE_SX = \{\s*minWidth: 960,/, "the table keeps the template 960 floor");
  // FIXJ2 COUNTS: cells link through sx on the table (no legacy .celllink / .muted classes).
  assert.doesNotMatch(page, /className=/, "the herd register renders no legacy class");
  assert.doesNotMatch(page, /herd-register-table/, "no legacy table class");
  assert.doesNotMatch(css, /herd-register-table/, "legacy min-width rules deleted");
  assert.doesNotMatch(page, /onSelectAllRows|<Checkbox/, "no checkbox column without a bulk action");
  // REVIEW-40 O61: the sort is an announced navigation (the table skeletons at once) and the KPI
  // panel, which the sort / tab / pager never narrow, ignores those params.
  const head = readFileSync(new URL("../../components/app/table/url-sort-head.tsx", import.meta.url), "utf8");
  assert.match(head, /useUrlNavigate\(\)/);
  assert.doesNotMatch(head, /router\.replace|useRouter/);
  assert.match(page, /HERD_KPI_IGNORE = \[[^\]]*"order"[^\]]*\]/);
  assert.match(page, /HERD_KPI_IGNORE = \[[^\]]*"status"[^\]]*\]/);
  assert.match(page, /ignore=\{HERD_KPI_IGNORE\} fallback=\{<HerdKpiSkeleton/);
});
