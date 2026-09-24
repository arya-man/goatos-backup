import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");

test("live table defaults to a stable Smart tag sort", () => {
  const params = read("./params.ts");
  assert.match(params, /const sort = .* \?\? "smart_tag"/, "Smart tag must remain the default live-table sort");
  assert.match(params, /hs_sort: params\.sort === "smart_tag" \? undefined : params\.sort/, "default sort should keep the URL clean");
});

test("live table exposes the operator-requested sort keys and motion headers", () => {
  const params = read("./params.ts");
  for (const key of ["smart_tag", "tag_temp", "last_seen", "motion_count", "delta_15m", "delta_1h"]) {
    assert.match(params, new RegExp(`"${key}"`), `${key} must be accepted as a live-table sort key`);
  }

  const table = read("./herd-signals-table.tsx");
  for (const label of ["Smart tag", "Tag temp", "Last seen", "Motion count", "15m delta", "1h delta"]) {
    assert.match(table, new RegExp(label), `${label} must render as a sortable header`);
  }
  assert.match(table, /24h delta/, "24h delta must render as a display-only aggregate header");
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
  assert.match(table, /Rolling 24-hour motion-counter delta/, "24h delta must explain it is a rolling motion-counter value");
  assert.match(table, /not a step count/, "24h delta must not be presented as steps");
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

test("Low battery KPI filter includes critical battery rows", () => {
  const rowFilter = read("./herd-signals-row-filter.ts");
  assert.match(rowFilter, /item\.battery_state === "low" \|\| item\.battery_state === "critical"/, "low_battery must filter low and critical rows");
});

test("Realtime movement KPI cards use backend live-state filters", () => {
  const params = read("./params.ts");
  assert.match(params, /kpiToLiveState/, "Realtime movement KPI keys must map to a server-side live_state filter");
  assert.match(params, /kpi === "moving_now" \|\| kpi === "active_1m"/, "moving_now and active_1m must be recognized live_state keys");

  const board = read("./herd-signals-board.tsx");
  assert.match(board, /liveState: kpiToLiveState\(params\.kpi\)/, "Live tab must send realtime KPI clicks to the backend");

  const kpis = read("./herd-signals-kpis.tsx");
  assert.match(kpis, /filterKey === "moving_now" \|\| filterKey === "active_1m"/, "Realtime KPI clicks must clear stale movement_state filters");
  assert.match(kpis, /hs_move: filterKey === "moving_now" \|\| filterKey === "active_1m" \? undefined : params\.movementState/, "Realtime KPI clicks must not combine live_state with an old hs_move filter");

  const streamBridge = read("./herd-signals-stream-bridge.tsx");
  assert.match(streamBridge, /useSearchParams/, "Stream and export URLs must update after in-app search-param navigation");
  assert.match(streamBridge, /const searchKey = searchParams\.toString\(\)/, "Stream bridge must key stream/export query construction off current search params");
  assert.match(streamBridge, /\[live, tabHidden, streamConsumesLiveSnapshot, liveKey, liveQuery\]/, "EventSource must reconnect when the live query changes");
  assert.match(streamBridge, /hs_risk: "risk_state"/, "Stream/export URLs must preserve the watchlist filter");
  assert.match(streamBridge, /out\.set\("live_state", liveState\)/, "Stream/export URLs must map realtime KPI filters to live_state");
  assert.match(streamBridge, /out\.delete\("movement_state"\)/, "Realtime KPI stream/export URLs must not keep conflicting movement_state");
  assert.match(streamBridge, /if \(tab === "live"\)/, "Realtime KPI live_state mapping must only apply on the Live tab");
  assert.match(streamBridge, /tab === "animals"[\s\S]*out\.set\("mapping_state", "mapped"\)/, "Animals tab export/stream must force the same mapped filter as the table");
  assert.match(streamBridge, /tab === "alerts"[\s\S]*out\.set\("risk_state", "attention"\)/, "Alerts tab export/stream must force the watchlist sentinel used by the table");
  assert.match(streamBridge, /unsupportedExportTab = tab === "gateways" \|\| tab === "insights"/, "Export must be disabled on non-table tabs");
  assert.match(streamBridge, /residualKpi === "weak_signal" \|\| residualKpi === "missing_signal" \|\| residualKpi === "low_battery"/, "Export must be disabled for page-only residual KPI filters");
  assert.match(streamBridge, /Clear this page-only KPI filter before exporting/, "Disabled export must explain why it is unavailable");

  const api = read("../../lib/api/herd-signals.ts");
  assert.match(api, /live_state: params\.liveState/, "API wrapper must forward the live_state query parameter");

  const openapi = read("../../../../contracts/openapi/app-api.yaml");
  assert.match(openapi, /name: live_state[\s\S]*HerdSignalLiveStateFilter/, "OpenAPI must publish the live_state query parameter");
  assert.match(openapi, /HerdSignalLiveStateFilter:[\s\S]*enum: \[moving_now, active_1m\]/, "OpenAPI must document realtime live_state values");
  assert.match(openapi, /\/herd-signals\/live\/stream:[\s\S]*operationId: streamHerdSignalsLive/, "OpenAPI must publish the SSE stream route");
  assert.match(openapi, /SSE stream of heartbeat ticks and live snapshots[\s\S]*text\/event-stream:/, "OpenAPI must document the SSE response media type");

  const rowFilter = read("./herd-signals-row-filter.ts");
  assert.doesNotMatch(rowFilter, /kpi === "moving_now"/, "moving_now must not be a page-only residual filter");
  assert.doesNotMatch(rowFilter, /kpi === "active_1m"/, "active_1m must not be a page-only residual filter");
});

test("SSE ticks must not force full page refreshes", () => {
  const streamBridge = read("./herd-signals-stream-bridge.tsx");
  const tickHandler = streamBridge.match(/source\.addEventListener\("tick", \(\) => \{[\s\S]*?\n    \}\);/)?.[0] ?? "";
  assert.ok(tickHandler, "stream bridge must register an SSE tick handler");
  assert.doesNotMatch(tickHandler, /refresh\(\)/, "SSE tick handler must not refresh the route");
  assert.doesNotMatch(streamBridge, /router\.refresh|useRouter|onClick=\{refresh\}/, "SSE bridge must not refresh the route; data must arrive through EventSource snapshots");
  assert.doesNotMatch(streamBridge, /STREAM_REFRESH_MIN_MS|lastStreamRefreshAtRef/, "SSE route-refresh throttles must not exist; ticks must not refresh the route at all");
  assert.match(streamBridge, /stream ticks update connection\/freshness state only/, "stream bridge comment must preserve the no-refresh SSE contract");
});

test("SSE snapshot drives the live KPI and table stores", () => {
  const streamBridge = read("./herd-signals-stream-bridge.tsx");
  const store = read("./herd-signals-live-store.ts");
  const kpis = read("./herd-signals-kpis.tsx");
  const table = read("./herd-signals-table.tsx");

  assert.match(streamBridge, /source\.addEventListener\("snapshot"/, "stream bridge must listen for backend snapshot events");
  assert.match(streamBridge, /writeHerdSignalsLiveSnapshot\(liveKey, JSON\.parse\(event\.data\)/, "snapshot events must write the parsed live response into the live store");
  assert.match(store, /useSyncExternalStore\(subscribe, readSnapshot, readServerSnapshot\)/, "live store must be a React external store, not a route refresh side channel");
  assert.match(kpis, /const liveSnapshot = useHerdSignalsLiveSnapshot\(liveKey\)/, "KPI cards must subscribe to stream snapshots");
  assert.match(kpis, /const displayedSummary = liveSnapshot\?\.data\.summary \?\? summary/, "KPI cards must prefer streamed summaries over stale server props");
  assert.match(table, /const liveSnapshot = useHerdSignalsLiveSnapshot\(liveKey\)/, "live table must subscribe to stream snapshots");
  assert.match(table, /const displayedNowMs = clientNowMs \|\| nowMs/, "live table relative ages must tick from client time instead of freezing at server render time");
  assert.match(table, /const displayedItems = liveSnapshot\?\.data\.items \?\? items/, "live table rows must prefer streamed items over stale server props");
  assert.match(table, /const displayedNextCursor = liveSnapshot\?\.data\.next_cursor \?\? nextCursor/, "live table pagination must come from streamed snapshots when present");
});

test("live table exposes own-baseline and group-comparison risk signals", () => {
  const params = read("./params.ts");
  assert.match(params, /hs_risk/, "Watchlist filter must round-trip through the live monitor URL");

  const filters = read("./herd-signals-filters.tsx");
  assert.match(filters, /aria-label="Watchlist"/, "Watchlist filter must be available in the filter bar");

  const table = read("./herd-signals-table.tsx");
  assert.match(table, /Watchlist rules/, "Watchlist column needs explanatory copy");
  assert.match(table, /own_motion_delta_pct/, "Risk cell must show own-baseline motion comparison");
  assert.match(table, /group_motion_delta_pct/, "Risk cell must show same-pen group motion comparison");
  assert.match(table, /group_temp_delta_c/, "Risk cell must show same-pen temperature comparison");
  assert.match(table, /Movement is far below|Movement lower than usual|Tag warmer than pen average/, "Risk cell must render human-readable reasons, not own/group/temp shorthand");
  assert.doesNotMatch(table, /`own \$\{Math\.round/, "Risk cell must not render the old own/group/temp shorthand");
  assert.match(table, /hs_risk: undefined/, "Clear filters must clear the signal shortlist filter");

  const api = read("../../lib/api/herd-signals.ts");
  assert.match(api, /risk_state: params\.riskState/, "Watchlist risk filter must be sent to the live API");
  assert.match(api, /\"attention\"/, "API wrapper must allow the Alerts tab's watchlist sentinel");
  for (const field of ["risk_state", "risk_reasons", "own_motion_delta_pct", "group_motion_delta_pct", "group_temp_delta_c"]) {
    assert.match(api, new RegExp(field), `${field} must be exposed on HerdSignalItem`);
  }
});
