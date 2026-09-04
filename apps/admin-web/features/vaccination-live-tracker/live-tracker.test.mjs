import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const board = readFileSync(new URL("./live-tracker-board.tsx", import.meta.url), "utf8");
const kpis = readFileSync(new URL("./live-tracker-kpis.tsx", import.meta.url), "utf8");
const operators = readFileSync(new URL("./live-tracker-operators.tsx", import.meta.url), "utf8");
const sheds = readFileSync(new URL("./live-tracker-sheds.tsx", import.meta.url), "utf8");
const combo = readFileSync(new URL("./live-tracker-combo.tsx", import.meta.url), "utf8");
const rail = readFileSync(new URL("./live-tracker-rail.tsx", import.meta.url), "utf8");
const poller = readFileSync(new URL("./live-poller.tsx", import.meta.url), "utf8");
const params = readFileSync(new URL("./params.ts", import.meta.url), "utf8");
const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");

// Assertions about what the page RENDERS must not be satisfied or defeated by prose in a comment —
// several of these comments name the exact mock defect they exist to avoid reproducing.
function code(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1");
}

test("the board makes exactly one backend read", () => {
  const calls = board.match(/getVaccinationLiveTracker\(/g) ?? [];
  assert.equal(calls.length, 1, "the page must not fan out into several vaccination reads");
  assert.match(board, /export function loadLiveTracker/);
});

test("all three render branches exist — error, empty, full", () => {
  assert.match(board, /if \(!result\.ok\)/, "a failed read must render its own visible error card");
  assert.match(board, /result\.error\.message/, "the backend message must reach the screen");
  assert.match(board, /state\.error_title/);
  assert.match(board, /state\.empty_title/, "an empty-but-ok day still renders every section");
  assert.match(board, /action\.reset_filters/);
});

test("every one of the mock's six KPI tiles is present, at the documented grain", () => {
  for (const key of [
    "kpi.scheduled.label",
    "kpi.proofs.label",
    "kpi.scans.label",
    "kpi.remaining.label",
    "kpi.combo.label",
    "kpi.attention.label",
  ]) {
    assert.ok(kpis.includes(key), `missing KPI tile ${key}`);
  }
  assert.match(kpis, /kpis\.combo_animals/, "combo tile is ANIMAL grain and must read the animal count");
  assert.match(kpis, /kpis\.scheduled_administrations/, "scheduled tile is ADMINISTRATION grain");
});

test("the KPI cross-filter the mock implied is disabled with a visible reason, not silently inert", () => {
  assert.match(kpis, /aria-disabled="true"/);
  assert.match(kpis, /kpi\.cross_filter_disabled/);
});

test("mock sections all render: operators, sheds, combo, activity, attention, verification", () => {
  assert.match(operators, /section\.operators\.title/);
  assert.match(sheds, /section\.sheds\.title/);
  assert.match(combo, /section\.combo\.title/);
  assert.match(rail, /section\.activity\.title/);
  assert.match(rail, /section\.attention\.title/);
  assert.match(rail, /section\.verification\.title/);
});

test("no synthetic animal id is ever rendered — real tags only", () => {
  for (const [name, source] of Object.entries({ combo, rail, operators, sheds })) {
    assert.ok(!/GT-/.test(code(source)), `${name} must not render the mock's fabricated GT-##### id`);
  }
  assert.match(combo, /row\.primary_tag/);
  assert.match(combo, /row\.secondary_tag/, "a dual-tagged animal must surface its second tag");
});

test("combo rows open the shared Goat Passport local overlay", () => {
  assert.match(combo, /LocalOverlayLink/);
  assert.match(board, /LiveTrackerPassportDrawer/);
});

test("the three unbacked attention affordances name the missing capability, in visible text", () => {
  // They shipped labelled "Nudge sent" / "Escalates" / "Finishes at current pace" — bare factual
  // ASSERTIONS about events that were never recorded, with the reason reachable only through a hover
  // title on a non-focusable span (invisible on touch and to assistive tech). A director reading
  // "Nudge sent" against an idle operator is told the intervention already happened.
  assert.match(rail, /section\.attention\.nudge_label/);
  assert.match(rail, /section\.attention\.escalate_label/);
  assert.match(rail, /section\.attention\.pace_label/);
  const disabled = rail.match(/aria-disabled="true"/g) ?? [];
  assert.ok(disabled.length >= 3, "each unbacked affordance stays visible and inert");
  // The reason is rendered as text, the way the combo card's truncated branch already does it.
  const railCode = code(rail);
  for (const key of ["section.attention.nudge", "section.attention.escalation", "section.attention.pace"]) {
    assert.ok(
      new RegExp(`title=\\{copy\\(pageContract, "${key.replace(/\./g, "\\.")}"\\)\\}`).test(railCode) === false,
      `${key} must not be reachable only through a hover title`,
    );
    assert.ok(railCode.includes(`"${key}"`), `${key} must still reach the screen`);
  }
  assert.match(rail, /lt-truncnote/, "the reasons render as visible text, not as tooltips");
});

test("the board reports obligation CLOSURE, not just proof arrival", () => {
  // All 298 proof videos had landed in stg on 2026-08-12 while 9 of 298 obligations were completed.
  // With progress derived from proof arrival the tiles read zero remaining and every shed row read
  // `done` — the board announced a finished drive on a drive that was still open.
  assert.match(operators, /row\.closed_administrations/, "the operator table must show closure");
  assert.match(sheds, /row\.closed_administrations/, "the shed table must show closure");
  assert.match(kpis, /kpis\.remaining/);
  assert.match(kpis, /kpis\.awaiting_close/, "proofed-but-not-closed work must be visible, not implied");
  // The progress bar must track the same fact Remaining and the status pill are derived from.
  const format = readFileSync(new URL("./format.ts", import.meta.url), "utf8");
  assert.match(format, /export function progressTone\(state: string\)/,
    "the bar tone must READ the backend's row state, not recompute a different threshold");
  assert.match(sheds, /progressTone\(row\.state\)/);
  assert.match(operators, /progressTone\(row\.state\)/);
});

test("work with no operator assignment is named, not silently missing from the operator table", () => {
  // Those administrations are counted in the Scheduled tile and rendered in the shed board, but they
  // have no operator to be attributed to, so the Operators column simply summed short of the tile
  // above it. On a partially-planned stg day the gap is 95 of 199.
  assert.match(board, /data\.unassigned_administrations/);
  assert.match(operators, /unassignedAdmins/);
  assert.match(operators, /section\.operators\.unassigned_note/);
});

test("no internal identifier is ever rendered as user-visible text", () => {
  // The filter vocabulary is compiled from the day's OWN rows, so a selection with no work on this
  // drive day is absent from choices. The old fallback printed the raw uuid in the active chip while
  // the <select> beside it, having no matching <option>, rendered "All parks".
  const filters = readFileSync(new URL("./live-tracker-filters.tsx", import.meta.url), "utf8");
  assert.ok(
    !/\?\?\s*filter\.selected/.test(code(filters)),
    "an unlisted selection must render a copy string, never the raw value",
  );
  assert.match(filters, /filter\.unlisted_selection/);
  assert.match(filters, /filter\.truncated_note/, "a capped filter vocabulary must say so");
  assert.match(board, /data\.filter_options\.truncated/);
});

test("a failed read never fabricates a measurement", () => {
  // The error branch rendered a pulsing LIVE pill reading "0 parks running" — asserting that no park
  // is vaccinating, when in fact nothing is known because the read failed.
  const errorBranch = board.slice(board.indexOf("if (!result.ok)"), board.indexOf("const data = result.data"));
  assert.match(errorBranch, /activeParks=\{null\}/, "unknown is null, not zero");
  assert.match(board, /activeParks: number \| null/);
  assert.match(board, /activeParks == null \? null :/, "no chip at all when the number is unknown");
});

test("a past drive day is not rendered as running, and is not polled", () => {
  assert.match(board, /data\.is_live_day/);
  assert.match(board, /chip\.parks_active_one/, "a finished drive gets a neutral, past-tense chip");
  assert.match(board, /generatedAt && isLiveDay \? <LivePoller/, "a closed drive day must stop polling");
});

test("the empty state never claims the whole day when a scope is narrowing it", () => {
  // hasFilter deliberately excludes the top-bar park, so with a park selected the board asserted
  // "No vaccination drive work on this day" while the other park was running.
  assert.match(params, /hasNarrowing/);
  assert.match(params, /hasNarrowing: Boolean\(parkId \|\|/);
  assert.match(board, /params\.hasNarrowing/);
  assert.match(board, /state\.empty_filtered_title/);
});

test("every capped list on this page declares its own truncation — including the feed and the rail", () => {
  assert.match(rail, /activity\.next_cursor != null/, "the activity feed must declare that it is capped");
  assert.match(rail, /section\.activity\.truncated_note/);
  assert.match(rail, /section\.attention\.truncated_note/);
  assert.match(board, /data\.attention_total/);
  const drawer = readFileSync(new URL("./live-tracker-passport-drawer.tsx", import.meta.url), "utf8");
  assert.match(drawer, /open\.length > DRAWER_ROW_LIMIT/, "the passport drawer must disclose dropped rows");
  assert.match(drawer, /drawer\.passport\.more_suffix/);
  assert.match(drawer, /<thead>/, "the drawer's obligations table must label its columns");
});

test("the combo header never invents a combination no animal received", () => {
  // Joining every distinct vaccine label with " + " into the mock's single combo slot rendered a
  // four-antigen combo on a day carrying two different two-antigen combos.
  assert.ok(
    !/vaccine_labels\.join\(" \+ "\)/.test(code(combo)),
    "one chip per real antigen, never the union of every label on the day",
  );
  assert.match(combo, /combo\.vaccine_labels\.map/);
});

test("the feed rate is measured, never the mock's hardcoded value", () => {
  assert.ok(!/~\d+\/min/.test(code(rail)), "the mock's hardcoded rate must not be reproduced");
  assert.match(rail, /uniqueScanRate\(activity\.items\)/, "the primary badge must be recent unique animals scanned per minute");
  assert.match(rail, /live\.scan_rate_suffix/);
  assert.match(rail, /activity\.observed_per_min/, "raw event rate may remain as secondary context");
  assert.match(rail, /live\.feed_rate_unavailable/, "no measurable window must say so rather than guess");
});

test("the activity rail groups proof and scan rows by animal identity", () => {
  assert.match(rail, /activityAnimalRows\(activity\.items\)/);
  assert.match(rail, /item\.goat_id \|\| item\.scanned_identifier \|\| item\.event_id/);
  assert.match(rail, /row\.kinds\.has\("scan_capture"\)/);
  assert.match(rail, /row\.kinds\.has\("proof_video"\)/);
});

test("the feed is announced to assistive technology", () => {
  assert.match(rail, /aria-live="polite"/);
});

test("polling re-runs the server tree and never stacks requests", () => {
  assert.match(poller, /router\.refresh\(\)/);
  assert.ok(!/useQuery|react-query/.test(code(poller)), "this page must not become the app's only client fetcher");
  assert.match(poller, /pendingRef\.current/, "a refresh in flight must suppress the next tick");
  assert.match(poller, /visibilityState/, "a hidden tab must not keep polling");
  assert.match(poller, /goat_passport/, "an open drawer must not be refreshed out from under the reader");
});

test("timestamps render in the business timezone, not the viewer's", () => {
  const format = readFileSync(new URL("./format.ts", import.meta.url), "utf8");
  assert.match(format, /Asia\/Kolkata/);
  assert.match(format, /Intl\.DateTimeFormat/);
});

test("scope-aware files never read a top-bar scope key or hand-roll a query string", () => {
  for (const [name, source] of Object.entries({ board, params })) {
    assert.ok(!/new URLSearchParams\(/.test(code(source)), `${name} must build links through scopeHref`);
    assert.ok(!/one\(sp, ?["']park["']\)/.test(code(source)), `${name} must read park through parseScope`);
  }
  assert.match(params, /parseScope/);
  assert.match(params, /scopeHref/);
});

test("dense tables pin their cells to one line, per the mock-anatomy rule", () => {
  assert.match(css, /table\.lt-operator-table th[\s\S]{0,400}white-space:nowrap/);
  assert.match(css, /table\.lt-shed-table th[\s\S]{0,400}white-space:nowrap/);
});

test("live tracker styles are scoped so they cannot restyle other boards", () => {
  const block = css.slice(css.indexOf("/vaccination/live-tracker — ported verbatim"));
  const unscoped = block
    .split("\n")
    .filter((line) => /^\.(feed|frow|legend|num|kpi|tag)\b/.test(line.trim()));
  assert.deepEqual(unscoped, [], "generic mock class names must stay under .lt-page");
});

test("counted labels do not read as broken singulars", () => {
  // "1 extra attempts" and "0 All combo animals →" both shipped in an early render against real stg
  // data. Small wrongness in a headline is expensive: it makes a reader distrust every other number.
  assert.match(sheds, /row\.extra_attempt_count > 1/, "the count only leads the label above one");
  const comboCode = code(combo);
  const truncatedBranch = comboCode.slice(comboCode.indexOf("rows_truncated ?"));
  const disabledBranch = truncatedBranch.slice(truncatedBranch.indexOf(") : ("));
  assert.ok(
    !/combo\.animal_count/.test(disabledBranch),
    "the untruncated button must not repeat the animal count the card header already shows",
  );
});

test("the Full Schedule button's fragment matches a section id that is actually rendered", () => {
  // The board emitted "#full-vaccine-schedule", which is the BACKEND TABLE CONTRACT id
  // (adminui service.go), not a DOM anchor. Nothing on /vaccination carries it, so the button
  // navigated and scrolled nowhere. Pinning the fragment against the rendering file is the only
  // thing that stops this silently rotting again.
  const anchor = board.match(/export const FULL_SCHEDULE_ANCHOR = "([a-z0-9-]+)"/)?.[1];
  assert.ok(anchor, "the schedule anchor must be named once, not inlined into a string concat");
  assert.match(board, /view: "schedule"/, "the href must open the vaccination page's schedule component");
  assert.match(board, /schedule_year: String\(vaccinationScheduleYear\(params\.sp\)\)/, "the href must preserve the schedule year selector");
  assert.match(board, /"#" \+\s+FULL_SCHEDULE_ANCHOR/, "the href must be built from that constant");
  assert.match(board, /<a href=\{scheduleHref\}/, "cross-page hash navigation should use a native anchor");
  const schedule = readFileSync(
    new URL("../preventive-care-vaccination/full-vaccine-schedule.tsx", import.meta.url),
    "utf8",
  );
  assert.ok(
    schedule.includes(`id="${anchor}"`),
    `no <section id="${anchor}"> exists in full-vaccine-schedule.tsx — the Full Schedule button is a dead link`,
  );
});

test("the combo truncation control is never rebound to a single animal's drawer", () => {
  // "All 200 combo animals" opening ONE animal's passport is the exact house-rule failure: an
  // affordance the backend cannot power must be disabled with a visible reason, never silently
  // rebound to a different action.
  assert.ok(
    !/truncatedHref/.test(code(board)),
    "the board must not synthesise a destination for the truncated branch",
  );
  assert.ok(!/truncatedHref/.test(code(combo)), "the combo card must not accept a truncated destination");
  assert.ok(
    !/rows\[0\]/.test(code(board)),
    "no control may be wired to the FIRST combo row as a stand-in for the whole list",
  );
  assert.match(combo, /section\.combo\.truncated_reason/, "the truncated branch carries its own visible reason");
  const comboCode = code(combo);
  const truncatedBlock = comboCode.slice(comboCode.indexOf("action.all_combo_animals") - 400);
  assert.ok(
    !/LocalOverlayLink[\s\S]{0,200}action\.all_combo_animals/.test(truncatedBlock),
    "the all-combo-animals control must not be a link in either branch",
  );
});

test("the error branch offers a way back — one transient read failure must not freeze the board", () => {
  // LivePoller unmounts when generatedAt is null, and router.refresh() is this page's only refresh
  // path, so without a retry the board stayed frozen on the error card until a manual reload.
  const errorBranch = board.slice(board.indexOf("if (!result.ok)"), board.indexOf("const data = result.data"));
  assert.match(errorBranch, /liveTrackerHref\(params\)/, "the error card must link back to this page");
  assert.match(errorBranch, /action\.retry/);
});

test("every counted label has a singular branch", () => {
  // "1 operators · 1 parks" is the NORMAL case under a park filter, and "1 sheds" / "1 animals
  // today" are all reachable. Small wrongness in a headline makes a reader distrust every number.
  for (const [name, source, keys] of [
    ["operators", operators, ["section.operators.count_suffix_one", "section.operators.park_suffix_one"]],
    ["combo", combo, ["section.combo.count_suffix_one"]],
    ["rail", rail, ["section.verification.sheds_suffix_one"]],
  ]) {
    for (const key of keys) {
      assert.ok(source.includes(key), `${name} must branch on ${key}`);
    }
  }
  assert.match(sheds, /row\.extra_attempt_count > 1/, "the count only leads the label above one");
});

test("no counted figure renders as a bare unlabelled integer", () => {
  // "0/8 · 137 · 15:32" gave the reader no way to tell whether 137 was minutes, animals or scans.
  assert.match(rail, /section\.attention\.elapsed_suffix/, "the attention elapsed figure carries a unit");
  assert.ok(
    !/`\s*·\s*\$\{row\.elapsed_minutes\}`/.test(code(rail)),
    "elapsed_minutes must never be interpolated without its unit",
  );
});

test("the operator idle duration reaches the screen — the datum is already on the wire", () => {
  // The mock's cell is "idle 2h+"; the duration IS the cell, because "idle" alone gives a director
  // nothing to act on. idle_minutes was returned by the backend and rendered nowhere.
  assert.match(operators, /row\.idle_minutes/);
  assert.match(operators, /section\.operators\.idle_prefix/);
});

test("silently dropped rows are impossible — both boards declare their own truncation", () => {
  // The KPI tiles are folded from the untruncated rollup, so past the caps the Scheduled tile
  // legitimately exceeds the visible table sums. That is only readable if the page says so.
  assert.match(operators, /truncated/, "the operator board must render a truncation note");
  assert.match(operators, /section\.operators\.truncated_note/);
  assert.match(sheds, /section\.sheds\.truncated_note/);
  assert.match(kpis, /kpi\.truncated_note/, "a truncated rollup makes the headline itself wrong");
  assert.match(board, /data\.operators_truncated/);
  assert.match(board, /data\.sheds_truncated/);
  assert.match(board, /data\.cells_truncated/);
});

test("the live tick is observed, not asserted", () => {
  // The mock's tick is opacity:0 by default and flashes only when a tile value actually bumps. A
  // permanently rendered tick claims liveness even on a PAUSED board.
  const tick = readFileSync(new URL("./live-tick.tsx", import.meta.url), "utf8");
  assert.match(tick, /"use client"/, "the tick has to compare against the previous value client-side");
  assert.match(tick, /previous/, "the tick fires on a change, never unconditionally");
  assert.match(css, /\.lt-page \.lt-tick\{[^}]*opacity:0/, "the tick must be hidden by default");
  assert.match(css, /\.lt-page \.lt-tick\.on\{[^}]*opacity:1/);
});

test("row status pills carry the mock's pulsing dot and never cast an unknown tone", () => {
  // Tag's Tone union has no "live" member; `as Tone` silenced tsc while `.t-live` painted the same
  // red wash as `.t-dng` with no dot, making "active now", "idle" and "not started" identical.
  const liveTag = readFileSync(new URL("./live-state-tag.tsx", import.meta.url), "utf8");
  assert.match(liveTag, /<i \/>/, "the live pill must emit the mock's dot element");
  for (const [name, source] of Object.entries({ operators, sheds })) {
    assert.ok(!/as Tone/.test(code(source)), `${name} must not cast a backend tone into the design-system union`);
    assert.match(source, /LiveStateTag/, `${name} must render its status through the live-aware pill`);
  }
});

test("every cross-page link on this board carries the scope its own numbers were read under", () => {
  // The verification counts are park-scoped and /verify reads parseScope, so a bare "/verify" landed
  // the reader on a company-scoped queue whose totals contradicted the card they clicked.
  assert.ok(!/href="\/verify"/.test(code(rail)), "Open Verify must not bypass scopeHref");
  assert.match(board, /scopeHref\("\/verify"/);
});

test("Clear all is only offered when there is something it can clear", () => {
  // liveTrackerResetHref re-emits the top-bar park scope, so offering the chip for a bare park
  // selection produced a button that navigated to the identical URL and changed nothing.
  assert.match(board, /const clearAllHref = params\.hasFilter \? resetHref : null;/);
});

test("no dead markup ships to production", () => {
  for (const attr of ["data-live-tracker-path", "data-live-tracker-filters"]) {
    assert.ok(!board.includes(attr), `${attr} is read by nothing — it must not be rendered`);
  }
});

test("PAUSED survives the Suspense remount every filter change triggers", () => {
  // page.tsx keys the Suspense boundary on JSON.stringify(sp), so any filter change remounts the
  // poller. The interval already survived because it lives in storage; the pause state did not, and
  // a board deliberately paused to read a row resumed under the reader.
  assert.match(poller, /LIVE_STORAGE_KEY/);
  assert.match(poller, /useSyncExternalStore\(subscribeLive/);
  assert.ok(!/useState\(true\)/.test(code(poller)), "the live flag must not be remount-local state");
});
