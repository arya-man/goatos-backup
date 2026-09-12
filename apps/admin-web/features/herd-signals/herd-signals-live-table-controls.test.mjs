import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");

test("live table defaults to a stable Smart tag sort", () => {
  const params = read("./params.ts");
  assert.match(params, /const sort = .* \?\? "smart_tag"/, "Smart tag must remain the default live-table sort");
  assert.match(params, /hs_sort: params\.sort === "smart_tag" \? undefined : params\.sort/, "default sort should keep the URL clean");
});

test("live table exposes the operator-requested sort keys", () => {
  const params = read("./params.ts");
  for (const key of ["smart_tag", "tag_temp", "last_seen", "motion_count", "delta_15m", "delta_1h"]) {
    assert.match(params, new RegExp(`"${key}"`), `${key} must be accepted as a live-table sort key`);
  }

  const table = read("./herd-signals-table.tsx");
  for (const label of ["Smart tag", "Tag temp", "Last seen", "Motion count", "15m delta", "1h delta"]) {
    assert.match(table, new RegExp(label), `${label} must render as a sortable header`);
  }
});

test("Activity and Pattern info copy explains the non-contradiction", () => {
  const table = read("./herd-signals-table.tsx");
  assert.match(table, /Activity is the current 15-minute motion-count delta/, "Activity must explain the current-window delta");
  assert.match(table, /No movement: delta 0 while packets are still received/, "No movement must distinguish packets from counter movement");
  assert.match(table, /Pattern is the broader classification for the tag/, "Pattern must explain it is not the same field as Activity");
  assert.match(table, /Normal activity means the current deltas are within that tag's baseline band/, "Normal activity must explain why Quiet can coexist with it");
});

test("Quiet KPI copy matches its movement-state filter", () => {
  const kpis = read("./herd-signals-kpis.tsx");
  assert.match(kpis, /detail: \(\) => "motion-count delta 1 to 9 in last 15 min"/, "Quiet card must describe quiet only, not low or zero deltas");
  assert.doesNotMatch(kpis, /detail: \(\) => "low or zero delta this window"/, "Quiet card must not describe states outside movement_state=quiet");
});
