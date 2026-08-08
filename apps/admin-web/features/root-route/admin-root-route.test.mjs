import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const rootPageSource = readFileSync(
  new URL("../../app/(admin)/page.tsx", import.meta.url),
  "utf8",
);

test("admin root route redirects when control tower is absent from the role bootstrap contract", () => {
  assert.match(rootPageSource, /getAdminWebBootstrap/);
  assert.doesNotMatch(rootPageSource, /requireAdminWebPageContract\("control-tower"\)/);
  assert.match(rootPageSource, /route_id === "control-tower"/);
  assert.match(rootPageSource, /enabledPublished\.find\(\(item\) => item\.href === "\/actions"\)/);
  assert.match(rootPageSource, /redirect\(firstEnabledPublishedHref\(contract\) \?\? "\/vaccination"\)/);
});
