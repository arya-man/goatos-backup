import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const bellSource = readFileSync(new URL("./notification-bell.tsx", import.meta.url), "utf8");
const panelSource = readFileSync(new URL("./notification-panel.tsx", import.meta.url), "utf8");

test("the bell loads the next cursor and appends older notification pages", () => {
  assert.match(bellSource, /const cursor = feed\.next_cursor/);
  assert.match(bellSource, /loadNotificationFeedAction\(cursor\)/);
  assert.match(bellSource, /mode:\s*"replace"\s*\|\s*"append"/);
  assert.match(bellSource, /notification_request_id/);
  assert.match(bellSource, /sortNotificationsNewestFirst/);
});

test("scrolling near the bottom asks for the older page and still has a button fallback", () => {
  assert.match(panelSource, /onScroll=\{\(event\) =>/);
  assert.match(panelSource, /scrollHeight - list\.scrollTop - list\.clientHeight < 64/);
  assert.match(panelSource, /onLoadMore\?\.\(\)/);
  assert.match(panelSource, /centreCopy\.loadOlder/);
});
