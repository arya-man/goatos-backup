import assert from "node:assert/strict";
import test from "node:test";

import { isCurrentUrl, isPlainLeftClick, shownTabValue } from "./url-tab-nav.ts";

const click = (over = {}) => ({ metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, button: 0, ...over });

test("only a plain primary click is taken over; modified clicks stay with the browser", () => {
  assert.equal(isPlainLeftClick(click()), true);
  for (const over of [{ metaKey: true }, { ctrlKey: true }, { shiftKey: true }, { altKey: true }, { button: 1 }, { defaultPrevented: true }]) {
    assert.equal(isPlainLeftClick(click(over)), false, JSON.stringify(over));
  }
});

test("the tab already shown is not a navigation, whatever the param order", () => {
  const here = { href: "http://x.test/weighing/analytics?tab=breed&wt_from=2026-08-01" };
  assert.equal(isCurrentUrl("/weighing/analytics?wt_from=2026-08-01&tab=breed", here), true);
  assert.equal(isCurrentUrl("/weighing/analytics?wt_from=2026-08-01&tab=pen", here), false);
  assert.equal(isCurrentUrl("/weighing/analytics?wt_from=2026-08-01", here), false);
  assert.equal(isCurrentUrl("/weighing/weights?wt_from=2026-08-01&tab=breed", here), false);
  assert.equal(isCurrentUrl("https://other.test/weighing/analytics?tab=breed&wt_from=2026-08-01", here), false);
  assert.equal(isCurrentUrl("#", here), true);
  assert.equal(isCurrentUrl("?tab=pen", here), false);
});

test("the clicked tab is drawn selected while its navigation runs, then the server's value", () => {
  assert.equal(shownTabValue("general", "breed"), "breed");
  assert.equal(shownTabValue("breed", null), "breed");
});

import { changesWatchedParams, watchedParamsKey } from "./url-tab-nav.ts";

test("a panel's key is its watched params only, from any search shape", () => {
  assert.equal(watchedParamsKey("?park=a&offset=25&deal_id=9", ["park", "offset"]), "park=a&offset=25");
  assert.equal(watchedParamsKey({ park: "a", offset: "25", deal_id: "9" }, ["park", "offset"]), "park=a&offset=25");
  assert.equal(watchedParamsKey({ tab: ["x", "y"] }, ["tab", "park"]), "tab=x,y&park=");
});

test("only a navigation that changes a watched param suspends the panel", () => {
  const here = { href: "http://x.test/sales/sold?park=a&offset=25" };
  assert.equal(changesWatchedParams("/sales/sold?park=b&offset=25", here, ["park"]), true);
  assert.equal(changesWatchedParams("/sales/sold?park=a&offset=50", here, ["park"]), false);
  assert.equal(changesWatchedParams("/sales/sold?park=a&offset=25&deal_id=1", here, ["park", "offset"]), false);
  assert.equal(changesWatchedParams("/sales/loads?park=b", here, ["park"]), false);
});

test("an ALL_PARAMS panel suspends on any param change but the ignored ones, and keys by them", async () => {
  const { ALL_PARAMS } = await import("./url-tab-nav.ts");
  const here = { href: "http://x.test/weighing/analytics?tab=breed&park=a" };
  const watch = [ALL_PARAMS];
  const ignore = ["wt_export", "drawer"];
  assert.equal(changesWatchedParams("/weighing/analytics?tab=shed&park=a", here, watch, ignore), true);
  assert.equal(changesWatchedParams("/weighing/analytics?tab=breed", here, watch, ignore), true, "a param that disappears counts");
  assert.equal(changesWatchedParams("/weighing/analytics?tab=breed&park=a&sex=all", here, watch, ignore), true, "a param that appears counts");
  assert.equal(changesWatchedParams("/weighing/analytics?park=a&tab=breed&wt_export=1", here, watch, ignore), false);
  assert.equal(changesWatchedParams("/weighing/weights?tab=shed", here, watch, ignore), false, "another route is not this panel's navigation");
  assert.equal(watchedParamsKey({ tab: "breed", park: "a", drawer: "x" }, watch, ignore), "park=a&tab=breed");
  assert.equal(watchedParamsKey("?park=a&tab=breed", watch, ignore), watchedParamsKey({ tab: "breed", park: "a" }, watch, ignore));
});
