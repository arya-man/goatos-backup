import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./config-console.tsx", import.meta.url), "utf8");

test("Config protocol records open and close locally without route navigation", () => {
  assert.match(source, /LocalOverlayLink/);
  assert.match(source, /LOCAL_OVERLAY_URL_CHANGE_EVENT/);
  assert.match(source, /popstate/);
  assert.match(source, /currentHistoryEntryIsLocalOverlay/);
  assert.doesNotMatch(source, /router\.push\(recordHref/);
  assert.doesNotMatch(source, /<Link[^>]+className="veil"/);
  assert.match(source, /<Link href=\{detailHref\} className="btn p"/);
  assert.match(source, /selectedRule\.id === selectedRuleId \? selectedRuleDetail/);
});
