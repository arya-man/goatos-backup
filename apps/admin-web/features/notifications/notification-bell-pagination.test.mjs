import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const bellSource = readFileSync(new URL("./notification-bell.tsx", import.meta.url), "utf8");
const panelSource = readFileSync(new URL("./notification-panel.tsx", import.meta.url), "utf8");

test("the bell loads the next cursor and appends older notification pages", () => {
  assert.match(bellSource, /const cursor = feed\.next_cursor/);
  // Reads go through the GET route handler (a Server Action POST crashed redirecting pages).
  assert.match(bellSource, /fetchNotificationFeed\(cursor\)/);
  // Appended, deduped by id, never replacing the rows already shown.
  assert.match(bellSource, /items: \[\.\.\.current\.items, \.\.\.result\.feed\.items\.filter\(\(item\) => !seen\.has\(item\.notification_request_id\)\)\]/);
  assert.match(panelSource, /sortNotificationsNewestFirst/);
});

test("scrolling near the bottom asks for the older page and still has a button fallback", () => {
  assert.match(panelSource, /new IntersectionObserver\(/);
  assert.match(panelSource, /loadMoreRef\.current\?\.\(\)/);
  assert.match(panelSource, /onClick=\{onLoadMore\}/);
  assert.match(panelSource, /centreCopy\.loadMore/);
});
