import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./feed-purchases.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./feed-purchase-drawer.tsx", import.meta.url), "utf8");

test("feed purchase drawer route param is purchase_id end to end", () => {
  assert.match(drawerSource, /searchParams\.get\("purchase_id"\)/, "drawer must read purchase_id from the URL");
  assert.match(pageSource, /purchase_id:\s*"new"/, "record button must open the new drawer with purchase_id=new");
  assert.match(
    pageSource,
    /purchase_id:\s*purchase\.feed_purchase_id/,
    "row links must pass the purchase id through the purchase_id URL param",
  );
  assert.doesNotMatch(pageSource, /feed_purchase_id:\s*purchase\.feed_purchase_id/, "row links must not use the stale param name");
});
