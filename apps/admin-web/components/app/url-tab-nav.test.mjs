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
