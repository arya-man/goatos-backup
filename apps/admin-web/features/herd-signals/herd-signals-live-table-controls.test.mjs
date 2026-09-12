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

test("live table keeps tag temperature beside motion count", () => {
  const table = read("./herd-signals-table.tsx");
  const headers = Array.from(table.matchAll(/(?:sortableHead\("([^"]+)"|<th>([^<]+)<\/th>)/g), (match) => match[1] || match[2]);
  const cells = Array.from(table.matchAll(/data-l="([^"]+)"/g), (match) => match[1]);
  assert.deepEqual(
    headers.slice(headers.indexOf("Motion count"), headers.indexOf("Motion count") + 3),
    ["Motion count", "Tag temp", "15m delta"],
    "Tag temp must remain immediately after Motion count in the live table header",
  );
  assert.deepEqual(
    cells.slice(cells.indexOf("Motion count"), cells.indexOf("Motion count") + 3),
    ["Motion count", "Tag temp", "15m delta"],
    "Tag temp must remain immediately after Motion count in the live table rows",
  );
});

test("tag temperature renders Celsius and Fahrenheit", () => {
  const format = read("./format.ts");
  assert.match(format, /const fahrenheit = \(celsius \* 9\) \/ 5 \+ 32/, "formatter must derive Fahrenheit from Celsius");
  assert.match(format, /°C \/ \$\{fahrenheit\.toFixed\(1\)\} °F/, "tag temperature must show both Celsius and Fahrenheit");
});

test("live table sorting is server-side, not a fetched-page resort", () => {
  const board = read("./herd-signals-board.tsx");
  assert.match(board, /sort: params\.sort/, "live read must send the active sort key to the backend");
  assert.match(board, /sortDir: params\.sortDir/, "live read must send the active sort direction to the backend");

  const api = read("../../lib/api/herd-signals.ts");
  assert.match(api, /sort: params\.sort/, "API client must forward sort to /herd-signals/live");
  assert.match(api, /dir: params\.sortDir/, "API client must forward sort direction to /herd-signals/live");

  const table = read("./herd-signals-table.tsx");
  assert.doesNotMatch(table, /sortHerdSignalItems/, "table must not sort only the fetched page");
  assert.doesNotMatch(table, /compareNullableNumber|compareNullableTime/, "null ordering belongs in the server query");
});

test("live table sort contract accepts backend-shaped cursors", () => {
  const params = read("./params.ts");
  assert.match(params, /const CURSOR_MAX = 1024/, "opaque sort cursors can exceed the old 200-char cap");
  assert.match(params, /boundedText\(one\(sp, "hs_cursor"\), CURSOR_MAX\)/, "hs_cursor must use the raised cursor cap");

  const openapi = read("../../../../contracts/openapi/app-api.yaml");
  assert.match(openapi, /name: sort[\s\S]*enum: \[smart_tag, tag_temp, last_seen, motion_count, delta_15m, delta_1h\]/, "OpenAPI must publish live sort keys");
  assert.match(openapi, /name: dir[\s\S]*enum: \[asc, desc\]/, "OpenAPI must publish live sort directions");
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
