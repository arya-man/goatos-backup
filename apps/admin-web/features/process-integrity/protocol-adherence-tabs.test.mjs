import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { ADHERENCE_TAB_STATES } from "./protocol-adherence-layout.ts";

const page = readFileSync(new URL("./protocol-adherence.tsx", import.meta.url), "utf8");
const loading = readFileSync(new URL("../../app/(admin)/protocol-adherence/loading.tsx", import.meta.url), "utf8");

// guard: adherence-tabs-fit (TR2-P1-7). 12 work-state tabs overflowed the card at 1440 with scroll
// arrows and a clipped "Defe…". The template order list fits 5 tabs: All + 4 states as tabs, and
// EVERY state reachable through the work-state select.
test("guard: adherence-tabs-fit - at most All + 4 work-state tabs, every state in the select", () => {
  assert.ok(ADHERENCE_TAB_STATES.length <= 4);
  const tabs = page.slice(page.indexOf("<UrlTabs"), page.indexOf("/>", page.indexOf("<UrlTabs")));
  assert.match(tabs, /ADHERENCE_TAB_STATES\.map/);
  assert.doesNotMatch(tabs, /WORK_STATE_ORDER|scrollButtons/);
  const select = page.slice(page.indexOf("minWidth={ADHERENCE_STATE_WIDTH}") - 200, page.indexOf("minWidth={ADHERENCE_STATE_WIDTH}") + 400);
  assert.match(select, /WORK_STATE_ORDER\.map/, "the work-state select lists every state");
  assert.match(loading, /ADHERENCE_TAB_STATES\.length \+ 1/, "the skeleton draws the same tab count");
});

test("guard: adherence-tabs-fit - no lone header info glyph", () => {
  const header = page.slice(page.indexOf("<PageHeader"), page.indexOf("/>", page.indexOf("<PageHeader")));
  assert.doesNotMatch(header, /actions=/);
});

// pr294 L-A4: the fourth tile counted process_intact rows (medically deferred included) under
// "On-track (no action)", contradicting the ledger's only row "Watch / medically deferred".
test("protocol adherence no-gap tile says deferred rows are inside it", () => {
  const src = readFileSync(new URL("./protocol-adherence.tsx", import.meta.url), "utf8");
  assert.match(src, /label\.on_track_includes/);
  const service = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");
  assert.doesNotMatch(service, /"label\.on_track":\s+"On-track \(no action\)"/);
});
