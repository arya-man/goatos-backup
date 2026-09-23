#!/usr/bin/env node
// Mobile-webview flicker — the class of bug the route sweep structurally cannot see.
//
// Lane 1 takes one settled screenshot per route. Flicker only exists between frames,
// so no per-route screenshot will ever contain it. This runner carries the two checks
// that can see it, and they are deliberately different in kind:
//
//   A. THE CAUSE, statically. `position:sticky` together with `filter`/`backdrop-filter`
//      in one declaration block. No browser, milliseconds, never flaky. This is the
//      check that would have caught the Tasks filter bar on the day its CSS landed.
//      Implemented in apps/admin-web/scripts/lib/compositing-checks.mjs and also wired
//      into the route sweep, so it runs whether or not this runner does.
//
//   B. THE SYMPTOM, temporally. Film the phone-width page while a person-like journey
//      scrolls it and opens a panel, then look for a picture that changes and changes
//      straight back, repeatedly. Implemented in
//      apps/admin-web/scripts/lib/flicker-detector.mjs (pure) and flicker-capture.mjs
//      (the CDP screencast driver).
//
// `--video` runs check B over a recording instead of a live page. That is not a
// convenience: it is how the detector is held to ground truth. A detector that cannot
// find the bug in the video of the bug is worth nothing, so that path is a test.
//
// Everything here is read-only. It loads pages, scrolls them and opens panels. It
// submits nothing.
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { checkCompositingHazards, compositingSummary } from "../../apps/admin-web/scripts/lib/compositing-checks.mjs";
import { detectFlicker } from "../../apps/admin-web/scripts/lib/flicker-detector.mjs";
import { assertSweepPermitted } from "./sweep-safety.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
export const RECEIPT_RELATIVE = "mobile-flicker/mobile-flicker-receipt.json";

// The phone viewport lane 1 already uses. Kept identical on purpose: a finding that
// only reproduces at some other width is not a finding about the lane people trust.
export const PHONE = Object.freeze({
  label: "mobile",
  width: 390,
  height: 844,
  deviceScaleFactor: 3,
  name: "an Android phone",
});

// The screens carrying a pinned-and-blurred element that people are on all day.
//
// This list is NOT the scope of the live check any more. `--live` films the whole
// resolved route table at both viewports; this subset is what `--focus` narrows to
// when someone is chasing one screen. Keeping it as the default is what made the
// live path "4 routes, phone-only" — an example standing in for the scope.
export const DEEP_FILM_ROUTES = Object.freeze([
  { name: "tasks", path: "/tasks", pageName: "The Tasks page" },
  { name: "tasks-list", path: "/tasks?t_view=list", pageName: "The Tasks list" },
  { name: "herd-register", path: "/herd/register", pageName: "The herd register" },
  { name: "vaccination-plan", path: "/vaccination/plan", pageName: "The vaccination plan" },
]);
/** @deprecated kept so an existing import does not break; the live path no longer uses it as its scope. */
export const FILMED_ROUTES = DEEP_FILM_ROUTES;

/**
 * The fixture ids a sweep was given, from the environment.
 *
 * Offline by design: this reads a JSON blob someone hands the run, it never looks
 * anything up. A missing id is a named gap in the receipt, not a live query.
 */
/**
 * How many headless browsers are already alive.
 *
 * §8: `pgrep -f` matches your own command string, so this looks for the BROWSER
 * binaries by name — never for anything containing this script's own path.
 */
export function browsersRunning() {
  const probe = spawnSync("pgrep", ["-c", "-f", "headless_shell|chrome-linux/chrome"], { encoding: "utf8" });
  const n = Number(String(probe.stdout ?? "").trim());
  return Number.isFinite(n) ? n : 0;
}

/** Refuse before a browser is opened, never after. */
function permitSweep(pageLoads) {
  return assertSweepPermitted({ env: process.env, browsersRunning: browsersRunning(), pageLoads });
}

export function fixturesFromEnv(env = process.env) {
  const raw = env.GOATOS_SMOKE_FIXTURES;
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch (error) {
    throw new Error(`GOATOS_SMOKE_FIXTURES is not readable JSON, so the sweep would have silently fallen back to 136 routes: ${String(error?.message ?? error)}`);
  }
}

/**
 * The routes `--live` will film.
 *
 * This exists as a function, not as an expression inside the CLI block, for the
 * reason §8 gives: a self-test that cannot reach the deciding line proves nothing
 * about it. The scope decision is here so the self-test can hold it.
 */
export async function liveRoutes({ focus = false, fixtures = {}, root = repo } = {}) {
  if (focus) return DEEP_FILM_ROUTES;
  const { resolveRoutes } = await import("../../apps/admin-web/scripts/lib/smoke-route-catalogue.mjs");
  const { resolved, assumed } = resolveRoutes(root, { fixtures });
  return [...resolved, ...assumed];
}

/**
 * Coverage of a sweep, as a fraction with a reason on every gap.
 *
 * Pure, so the self-test can hold it to account without a browser. Nothing in here
 * lists hits: the only outputs are the denominator, the numerator, and a sentence
 * for each page that is in the first and not the second.
 */
/** Holes that need nothing but the clock; a route made only of these is never a gap. */
export const CLOCK_HOLE_TEXT = /smokeWideWindow|getFullYear/;

/** Sentences that explain a group instead of a page. None of these may reach a receipt. */
export const BULK_EXCUSE = /^(unreachable|not covered|n\/a|unknown|skipped)\.?$|the path is built from a fixture looked up at run time/i;

export function sweepCoverage({ all, resolved, assumed = [], unresolved, viewports, rows }) {
  const pagesNotJudged = [];
  const judgedPages = [];
  const seen = new Set();
  const assumedNames = new Set(assumed.map((r) => r.name));
  for (const row of rows) {
    seen.add(`${row.viewport}:${row.route}`);
    if (assumedNames.has(row.route)) continue; // already named as not judged, above
    const why = row.parked ?? row.error ?? null;
    if (why) pagesNotJudged.push({ route: row.route, viewport: row.viewport, why });
    else judgedPages.push(row);
  }
  // A page the sweep never reached at all is the most dangerous gap, because nothing
  // in the run mentions it. Name every one.
  for (const viewport of viewports) {
    for (const route of resolved) {
      if (!seen.has(`${viewport}:${route.name}`)) {
        pagesNotJudged.push({ route: route.name, viewport, why: "the sweep stopped before it reached this page" });
      }
    }
    for (const route of unresolved) {
      for (const gap of route.gaps) pagesNotJudged.push({ route: route.name, viewport, why: gap.why });
    }
    // A route built from an id nobody checked is not a judged page. Counting it
    // made the receipt read best exactly when the fixtures were worst.
    for (const route of assumed) pagesNotJudged.push({ route: route.name, viewport, why: route.why });
  }
  const pagesExpected = all.length * viewports.length;
  return {
    routesInLaneOne: all.length,
    routesResolved: resolved.length,
    routesAssumed: assumed.length,
    viewports: [...viewports],
    pagesExpected,
    pagesJudged: judgedPages.length,
    pagesNotJudged,
    // The one number a person should read. Never a list of hits.
    fraction: `${judgedPages.length}/${pagesExpected} pages judged`,
    overlaysJudged: judgedPages.reduce((n, r) => n + r.judged, 0),
    overlaysNotJudged: rows.flatMap((r) => (r.skipped ?? []).map((s) => ({ route: r.route, viewport: r.viewport, overlay: s.id, why: s.why }))),
  };
}

// ---------------------------------------------------------------------------
// Check A
// ---------------------------------------------------------------------------
export function runStaticCheck(root = repo) {
  const result = checkCompositingHazards(root);
  return {
    name: "pinned-and-blurred",
    ok: result.ok,
    summary: compositingSummary(result),
    scanned: result.scanned,
    scrolling: result.scrolling,
    // Only these may be spoken about as something a phone will show; the rest are
    // switched off at phone width and are a laptop-only combination.
    onPhone: result.onPhone,
    overlays: result.overlays,
  };
}

// ---------------------------------------------------------------------------
// Check B, over a recording (ground truth)
// ---------------------------------------------------------------------------
function ffprobeTimestamps(video) {
  const out = execFileSync(
    "ffprobe",
    ["-v", "error", "-select_streams", "v:0", "-show_entries", "frame=pts_time", "-of", "csv=p=0", video],
    { encoding: "utf8", maxBuffer: 1 << 28 },
  );
  return out.trim().split("\n").filter(Boolean).map(Number);
}

/**
 * Decode a recording straight to downsampled grayscale. Going through ffmpeg's own
 * scaler rather than decoding PNGs keeps this dependency-free and fast, and it is the
 * same block-average the live capture applies.
 */
export function framesFromVideo(video, { step = 8 } = {}) {
  const probe = execFileSync(
    "ffprobe",
    ["-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=p=0:s=x", video],
    { encoding: "utf8" },
  ).trim().split("\n")[0];
  const [sourceWidth, sourceHeight] = probe.split("x").map(Number);
  const width = Math.max(1, Math.floor(sourceWidth / step));
  const height = Math.max(1, Math.floor(sourceHeight / step));
  const timestamps = ffprobeTimestamps(video);
  const raw = execFileSync(
    "ffmpeg",
    ["-v", "error", "-i", video, "-vf", `scale=${width}:${height}`, "-pix_fmt", "gray", "-vsync", "0", "-f", "rawvideo", "-"],
    { maxBuffer: 1 << 30 },
  );
  const size = width * height;
  const count = Math.floor(raw.length / size);
  const frames = [];
  for (let i = 0; i < count; i += 1) {
    frames.push({
      t: timestamps[i] ?? i / 30,
      width,
      height,
      gray: new Uint8Array(raw.subarray(i * size, (i + 1) * size)),
      step,
      sourceWidth,
      sourceHeight,
    });
  }
  return frames;
}

/** GIF + filmstrip cut straight out of the recording, at the times it went wrong. */
export function evidenceFromVideo(video, result, outDir, name) {
  mkdirSync(outDir, { recursive: true });
  const event = result.events?.[0];
  if (!event) return { gif: null, filmstrip: null, note: "no flicker to show" };
  const pad = 0.12;
  const from = Math.max(0, event.startT - pad);
  const duration = event.endT - event.startT + pad * 2;
  const gif = path.join(outDir, `${name}-flicker.gif`);
  const filmstrip = path.join(outDir, `${name}-flicker-filmstrip.png`);
  try {
    execFileSync("ffmpeg", [
      "-v", "error", "-y", "-ss", String(from), "-t", String(duration), "-i", video,
      "-vf", "scale=300:-1:flags=lanczos,fps=8", "-loop", "0", gif,
    ], { stdio: "ignore" });
  } catch { /* the receipt still carries the numbers */ }
  try {
    execFileSync("ffmpeg", [
      "-v", "error", "-y", "-ss", String(from), "-t", String(duration), "-i", video,
      "-vf", "scale=210:-1,tile=6x1:margin=6:padding=4:color=0x1b1f1d", "-frames:v", "1", filmstrip,
    ], { stdio: "ignore" });
  } catch { /* same */ }
  return {
    gif: existsSync(gif) ? gif : null,
    filmstrip: existsSync(filmstrip) ? filmstrip : null,
    note: existsSync(gif) ? "" : "ffmpeg could not write the animation on this machine",
  };
}

async function runVideo(video, outDir) {
  if (spawnSync("ffprobe", ["-version"], { stdio: "ignore" }).status !== 0) {
    throw new Error("ffprobe is not installed, so a recording cannot be read here");
  }
  const frames = framesFromVideo(video);
  const result = detectFlicker(frames);
  const evidence = evidenceFromVideo(video, result, outDir, "recording");
  return { source: "recording", video: path.basename(video), result, evidence };
}

// ---------------------------------------------------------------------------
// Check B, live
// ---------------------------------------------------------------------------
async function runLive({ baseUrl, bearerToken, outDir, routes }) {
  // Playwright and pngjs are admin-web's dependencies and only resolve for a module
  // that lives under admin-web, so the browser work stays there and this runner only
  // asks for it.
  const { filmRoutes } = await import("../../apps/admin-web/scripts/lib/flicker-capture.mjs");
  const { SWEEP_VIEWPORTS } = await import("../../apps/admin-web/scripts/lib/flicker-capture.mjs");
  const runs = [];
  // Both viewports, always. Filming only the phone was the live path's other half of
  // "4 routes, phone-only": flicker at 1440 is a viewport nobody was looking at.
  for (const viewport of SWEEP_VIEWPORTS) {
    const done = await filmRoutes({ baseUrl, bearerToken, outDir, routes, phone: { ...viewport, name: viewport.label === "mobile" ? "an Android phone" : "a laptop" } });
    for (const run of done) runs.push({ ...run, viewport: viewport.label });
  }
  return { source: "live", viewports: SWEEP_VIEWPORTS.map((v) => v.label), baseUrl, runs };
}

// ---------------------------------------------------------------------------
// The full surface: every route, every dialog, both viewports
// ---------------------------------------------------------------------------
async function runSweep({ baseUrl, bearerToken, outDir, limit, fixtures }) {
  const { resolveRoutes } = await import("../../apps/admin-web/scripts/lib/smoke-route-catalogue.mjs");
  const { sweepViewport, SWEEP_VIEWPORTS } = await import("../../apps/admin-web/scripts/lib/flicker-capture.mjs");
  const { all, resolved, assumed, unresolved } = resolveRoutes(repo, { fixtures });
  // An assumed route is still VISITED — a page that renders is worth filming —
  // but it is never counted as judged, because nothing checked the id behind it.
  const sweepable = [...resolved, ...assumed];
  const routes = limit ? sweepable.slice(0, limit) : sweepable;
  const rows = [];
  for (const viewport of SWEEP_VIEWPORTS) {
    const done = await sweepViewport({
      baseUrl, bearerToken, outDir, routes, viewport,
      onRoute: (row) => {
        const found = row.findings.length ? ` FOUND ${row.findings.length}` : "";
        console.log(`swept ${row.viewport}:${row.route} overlays=${row.overlays} judged=${row.judged} skipped=${row.skipped.length}${row.parked ? " parked" : ""}${found}`);
      },
    });
    rows.push(...done);
  }
  const viewports = SWEEP_VIEWPORTS.map((v) => v.label);
  return {
    source: "sweep",
    baseUrl,
    viewports,
    // A fraction, with a sentence on every page that is not in the numerator.
    coverage: sweepCoverage({
      all,
      resolved: limit ? routes.filter((r) => !r.unverified) : resolved,
      assumed,
      unresolved: limit ? [...unresolved, ...sweepable.slice(limit).map((r) => ({ ...r, gaps: [{ why: `--limit ${limit} stopped the sweep before this page` }] }))] : unresolved,
      viewports,
      rows,
    }),
    rows,
  };
}

// ---------------------------------------------------------------------------
// Receipt
// ---------------------------------------------------------------------------
export function buildReceipt({ statik, temporal, headless }) {
  const runs = temporal?.runs ?? [];
  const flickering = runs.filter((r) => r.result?.flicker);
  // A solid panel that showed the page through it is a defect on its own evidence:
  // the browser said the element is opaque, so one flash is already wrong. It does not
  // need the repetition the whole-screen shimmer detector insists on.
  const showedThrough = runs.filter((r) => (r.overlay?.findings ?? []).length);
  const sweepFindings = (temporal?.rows ?? []).filter((row) => (row.findings ?? []).length);
  // A run that never opened the page it was aimed at is parked with its reason, never
  // counted as a clean page. A lane that quietly checks nothing is the worst outcome
  // available: it goes green exactly when it is blind.
  const parked = runs.filter((r) => r.parked || r.error);
  const filmed = runs.filter((r) => r.result);
  // A temporal run that judged nothing is NOT a clean screen. It used to fall through
  // to "pass" because there were no findings — which is exactly the green a blind lane
  // produces. Say "not checked" and refuse the pass.
  const temporalAsked = Boolean(temporal);
  const temporalJudged = filmed.length + (temporal?.coverage?.pagesJudged ?? 0);
  const temporalVerdict = !temporalAsked
    ? "not checked: no temporal run was asked for on this invocation"
    : temporalJudged === 0
      ? "not checked: the temporal run reached no page it could judge, so it proves nothing about flicker"
      : "checked";
  return {
    lane: "mobile-flicker",
    generatedAt: new Date().toISOString(),
    device: PHONE,
    // Check A: the cause. Sticky plus a filter is a failure; a fixed overlay is a note.
    staticCheck: statik,
    // Check B: the symptom.
    temporal: temporal ?? null,
    filmed: filmed.length,
    overlaysShowedThrough: showedThrough.length + sweepFindings.length,
    coverage: temporal?.coverage ?? null,
    temporalVerdict,
    coverageLine: temporal?.coverage?.fraction ?? "",
    parked: parked.map((r) => ({ route: r.route, why: r.parked ?? r.error })),
    // Said out loud in the receipt so nobody reads a green temporal result as proof
    // the screen is fine on a real phone. This is a GPU compositing artefact, and a
    // headless browser does not composite the way a phone's GPU does.
    headlessCaveat: headless
      ? "Check B ran in headless Chromium on a laptop. Headless composites differently from a phone GPU, so a clean run here is not proof a real phone is clean. Check A is the one that holds on this evidence."
      : "",
    status: statik?.ok
      && flickering.length === 0
      && showedThrough.length === 0
      && sweepFindings.length === 0
      // A sweep that judged nothing may not report pass. §2: a check that did not run
      // renders no verdict, in either direction.
      && !(temporalAsked && temporalJudged === 0)
      ? "pass"
      : temporalAsked && temporalJudged === 0 && flickering.length === 0 && showedThrough.length === 0 && sweepFindings.length === 0
        ? "not-checked"
        : "fail",
  };
}

function writeReceipt(receipt, runDir) {
  const file = path.join(runDir, RECEIPT_RELATIVE);
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, `${JSON.stringify(receipt, null, 2)}\n`);
  return file;
}

// ---------------------------------------------------------------------------
function parseArgs(argv) {
  const parsed = { selfTest: false, video: null, live: false, sweep: false, limit: 0, outDir: null, staticOnly: false, focus: false };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--static") parsed.staticOnly = true;
    else if (arg === "--live") parsed.live = true;
    else if (arg === "--sweep") parsed.sweep = true;
    // Narrow the live path to the handful of screens someone is chasing. Without it
    // --live films the whole resolved table: the scope is every route, not an example.
    else if (arg === "--focus") parsed.focus = true;
    else if (arg === "--limit") parsed.limit = Number(argv[++i]);
    else if (arg === "--video") parsed.video = argv[++i];
    else if (arg === "--out") parsed.outDir = argv[++i];
  }
  return parsed;
}

export function selfTest() {
  const assert = (condition, message) => { if (!condition) throw new Error(`self-test: ${message}`); };

  // Check A finds the three pinned-and-blurred elements that are on main today, and
  // does not fire on a filter that is not pinned to anything.
  const statik = runStaticCheck();
  assert(statik.scrolling.length >= 3, `check A must find the pinned-and-blurred elements, found ${statik.scrolling.length}`);
  for (const selector of [".top", ".navback", ".lt-page .lt-fbar"]) {
    assert(statik.scrolling.some((f) => f.selector === selector), `check A must flag ${selector}`);
  }
  // ...and must not claim the one that is switched off below 640px is a phone problem.
  assert(!statik.onPhone.some((f) => f.selector === ".lt-page .lt-fbar"),
    "an element made static at phone width must not be reported as a phone problem");
  assert(statik.onPhone.some((f) => f.selector === ".top"),
    "an element that stays pinned and blurred at phone width must be reported as one");
  assert(!statik.scrolling.some((f) => f.selector === ".vr-results-loading"),
    "check A must not flag an element that has a filter but is not pinned");
  assert(statik.overlays.some((f) => f.selector === ".veil"),
    "a fixed overlay must be reported separately, not counted as a scrolling hazard");

  // A receipt with no temporal run must still be a complete receipt.
  const receipt = buildReceipt({ statik: { ok: true }, temporal: null, headless: false });
  assert(receipt.status === "pass", "a clean static check with no filming is a pass");
  assert(receipt.lane === "mobile-flicker", "the receipt must name its lane");
  const failing = buildReceipt({
    statik: { ok: true },
    temporal: { runs: [{ result: { flicker: true } }] },
    headless: true,
  });
  assert(failing.status === "fail", "flicker found must fail the receipt");
  assert(failing.headlessCaveat.includes("not proof"), "a headless run must carry its caveat in the receipt");

  // A run that never opened the page it aimed at is parked with its reason and is
  // never counted as a page that was checked and found clean.
  const blind = buildReceipt({
    statik: { ok: true },
    temporal: { runs: [{ route: "tasks", parked: "not signed in" }, { route: "herd", error: "timed out" }] },
    headless: true,
  });
  assert(blind.filmed === 0, "a parked run is not a filmed page");

  // A solid panel that showed through fails the receipt on its own, with no help from
  // the whole-screen detector — that is the point of having a second check.
  const seeThrough = buildReceipt({
    statik: { ok: true },
    temporal: { runs: [{ route: "tasks", result: { flicker: false }, overlay: { findings: [{ label: "Filters", events: [{ seconds: 0.2 }] }] } }] },
    headless: true,
  });
  assert(seeThrough.status === "fail", "a panel that showed the page through it must fail the receipt");
  assert(seeThrough.overlaysShowedThrough === 1, "and be counted");
  assert(blind.parked.length === 2 && blind.parked.every((row) => row.why), "every parked run must carry a reason");

  return assert;
}

export async function selfTestWide() {
  const assert = (condition, message) => { if (!condition) throw new Error(`self-test: ${message}`); };
  const { resolveRoutes } = await import("../../apps/admin-web/scripts/lib/smoke-route-catalogue.mjs");
  const { SWEEP_VIEWPORTS } = await import("../../apps/admin-web/scripts/lib/flicker-capture.mjs");
  const viewports = SWEEP_VIEWPORTS.map((v) => v.label);

  // THE REAL DECISION, not a copy of it. `--live` with no --focus films the whole
  // resolved table at both viewports; the 4-route phone-only list is what --focus
  // narrows to. Asserted through the same function the CLI calls.
  const wide = await liveRoutes({ fixtures: fixturesFromEnv() });
  const narrow = await liveRoutes({ focus: true });
  // NOT a floor equal to today's route count. A ratchet pinned to the exact
  // current number has no headroom in one direction and no meaning in the
  // other: add a route needing an id and it goes red for something that is not
  // a defect. What must hold is that --live films the WHOLE table minus only
  // the routes that are genuinely gaps.
  const table = await import("../../apps/admin-web/scripts/lib/smoke-route-catalogue.mjs")
    .then((m) => m.resolveRoutes(repo, { fixtures: fixturesFromEnv() }));
  assert(wide.length === table.resolved.length + table.assumed.length,
    `--live must film every route that has an address, ${wide.length} of ${table.resolved.length + table.assumed.length}`);
  assert(wide.length > DEEP_FILM_ROUTES.length * 10,
    "the live path must not have quietly gone back to an example");
  assert(narrow.length === DEEP_FILM_ROUTES.length, "--focus narrows to the deep-film subset");
  assert(viewports.includes("laptop") && viewports.includes("mobile"),
    `both viewports must be swept, this sweeps ${viewports.join(", ")}`);

  // Coverage is a fraction over the WHOLE table, and every page outside the numerator
  // carries its own sentence.
  const { all, resolved, assumed, unresolved } = resolveRoutes(repo, { fixtures: fixturesFromEnv() });
  // Accounting, not a floor: every route lands in exactly one bucket, and a
  // route whose holes are all CLOCK holes is always resolved — that is the
  // property that would break if the bulk excuse came back, and it stays true
  // however many routes the table grows to.
  assert(resolved.length + assumed.length + unresolved.length === all.length,
    "every route is accounted for exactly once");
  assert(unresolved.every((r) => r.gaps.every((g) => !CLOCK_HOLE_TEXT.test(g.hole ?? ""))),
    "a route whose address needs nothing but the clock must never be reported as a gap");
  assert(resolved.length > unresolved.length,
    `most of the table must resolve from the clock alone, ${resolved.length} did of ${all.length}`);

  // Junk ids must NOT lift the count. They used to take it to a clean 146 of 146.
  const junk = resolveRoutes(repo, { fixtures: { goatId: "NOT-A-REAL-ID", toxinSopId: "placeholder", workflowRowId: "7", calendarEventId: "" } });
  assert(junk.resolved.length === resolved.length,
    `ids that are not records must not resolve a single route, they resolved ${junk.resolved.length - resolved.length} more`);
  assert(junk.assumed.length === 0, "a junk id is refused outright, never assumed");
  // A well-formed id nobody checked is an ASSUMPTION, reported as not judged.
  const shaped = resolveRoutes(repo, { fixtures: { goatId: "3f2504e0-4f89-41d3-9a0c-0305e82c3301" } });
  assert(shaped.assumed.some((r) => r.name === "goat-passport"),
    "a well-formed but unchecked id produces an assumed route, never a resolved one");
  assert(!shaped.resolved.some((r) => r.name === "goat-passport"), "and it is not counted as resolved");
  assert(shaped.assumed[0].why.includes("never checked against a real record"), "and it says so");
  // The shed page must never resolve to a shed CALLED placeholder. Stated as
  // the property itself, so a run that supplies a real shed path still passes.
  const shedPage = [...resolved, ...assumed].find((r) => r.name === "vaccination-shed-execution-detail");
  assert(!shedPage || !/\bplaceholder\b/.test(shedPage.path),
    `the vaccination shed page must not be filmed at a placeholder address (${shedPage?.path})`);
  const shedUnprovisioned = resolveRoutes(repo, { fixtures: {} }).unresolved
    .find((r) => r.name === "vaccination-shed-execution-detail");
  assert(shedUnprovisioned, "with no shed supplied it is a named gap, not a page nobody opens");
  const rows = [
    { route: resolved[0].name, viewport: "laptop", judged: 2, skipped: [], findings: [] },
    { route: resolved[1].name, viewport: "laptop", judged: 0, skipped: [{ id: "opened-1", why: "nothing solid opened" }], findings: [], parked: "this page needs a signed-in session and the run did not have one" },
  ];
  const coverage = sweepCoverage({ all, resolved, assumed, unresolved, viewports, rows });
  assert(coverage.pagesExpected === all.length * viewports.length,
    `the denominator must be every route at every viewport, it is ${coverage.pagesExpected}`);
  assert(coverage.pagesJudged === 1, "only a page that was actually judged counts");
  assert(/^1\/\d+ pages judged$/.test(coverage.fraction), `coverage must read as a fraction, it reads "${coverage.fraction}"`);
  assert(coverage.pagesNotJudged.length === coverage.pagesExpected - 1,
    `every page outside the numerator must be named, ${coverage.pagesNotJudged.length} of ${coverage.pagesExpected - 1} were`);
  assert(coverage.pagesNotJudged.every((g) => g.why && g.why.length > 15 && g.route && g.viewport),
    "every gap names its page, its viewport and why in a sentence");
  // SHAPE, never a count of distinct reasons. Demanding at least three
  // different unchecked reasons is a property of an UNDER-provisioned run:
  // hand the sweep the ids it asks for and the count falls, so the gate
  // punished the fix and taught the next person to provision it worse.
  const reasons = new Set(coverage.pagesNotJudged.map((g) => g.why));
  for (const why of reasons) {
    assert(!BULK_EXCUSE.test(why), `a gap may not be explained in bulk: "${why}"`);
  }
  assert([...reasons].some((r) => r.includes("signed-in session")), "a parked page keeps the reason it was parked for");
  assert([...reasons].some((r) => r.includes("stopped before it reached")), "a page the sweep never reached is named, not omitted");
  // Only assert the missing-id sentence when a route is ACTUALLY missing an id.
  // The gap names its page and what is missing in FARM words; the machine name
  // of the missing thing rides a field beside it. §2 bans property names and
  // code from a finding, and the first version of these sentences printed both.
  for (const route of unresolved) {
    for (const gap of route.gaps) {
      assert(gap.why.includes(route.name), `a gap must name its own page: "${gap.why}"`);
      assert(!/[`$]|encodeURIComponent|\$\{/.test(gap.why), `a finding may not print code at a person: "${gap.why}"`);
      assert("needs" in gap, `${route.name} must carry the machine name of what it needs in a field, not in the sentence`);
    }
  }
  // And no two pages may share one sentence: two toxin pages did.
  const gapSentences = unresolved.flatMap((r) => r.gaps.map((g) => g.why));
  assert(new Set(gapSentences).size === gapSentences.length,
    "every gap needs its own sentence; two pages sharing one is a bulk excuse in disguise");
  assert(coverage.overlaysNotJudged.some((o) => o.why === "nothing solid opened" && o.route && o.viewport),
    "a dialog that was not judged is named with its page");

  // A sweep that judged nothing renders NO verdict. This is the one that would have
  // let a blind lane report green.
  const blind = buildReceipt({
    statik: { ok: true },
    temporal: { runs: [], coverage: { ...coverage, pagesJudged: 0 }, rows: [] },
    headless: true,
  });
  assert(blind.status === "not-checked", `a sweep that judged nothing must not pass, it said "${blind.status}"`);
  assert(blind.temporalVerdict.startsWith("not checked"), "and must say so in words");
  const some = buildReceipt({ statik: { ok: true }, temporal: { runs: [], coverage, rows: [] }, headless: true });
  assert(some.status === "pass" && some.temporalVerdict === "checked", "a sweep that judged a page and found nothing is a pass");
  assert(some.coverageLine === coverage.fraction, "the receipt carries the fraction, not a list of hits");
  const noTemporal = buildReceipt({ statik: { ok: true }, temporal: null, headless: false });
  assert(noTemporal.temporalVerdict.startsWith("not checked"), "a static-only run says the temporal check did not run");

  console.log(`dashboard mobile flicker: wide self-test passed (${resolved.length} of ${all.length} routes resolved, ${assumed.length} assumed, ${unresolved.length} named as gaps, x ${viewports.length} viewports)`);
}

function selfTestDone() {
  console.log("dashboard mobile flicker: self-test passed");
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const args = parseArgs(process.argv.slice(2));
  const { resolveRoutes } = await import("../../apps/admin-web/scripts/lib/smoke-route-catalogue.mjs");
  if (args.selfTest) {
    selfTest();
    await selfTestWide();
    selfTestDone();
  } else {
    const runDir = args.outDir ?? path.join(repo, "outputs", "mobile-flicker", new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-"));
    const outDir = path.join(runDir, "mobile-flicker");
    const statik = runStaticCheck();
    console.log(`pinned-and-blurred: ${statik.summary}`);
    for (const f of statik.scrolling) console.log(`  ${f.file}:${f.line}  ${f.selector}  (${f.position} + ${f.filterProperty})`);

    let temporal = null;
    let headless = false;
    if (args.video) {
      temporal = { source: "recording", runs: [] };
      const one = await runVideo(args.video, outDir);
      one.pageName = "The Tasks page";
      one.route = "tasks";
      one.whileDoing = "";
      temporal.runs.push(one);
      const r = one.result;
      console.log(`recording: ${r.flicker ? "FLICKER" : "no flicker"} — ${r.events.length} event(s) over ${r.spanSeconds}s, ${r.settledChanges} change(s) that stayed changed, ${r.easedReturns} smooth animation(s)`);
      for (const e of r.events) console.log(`  ${e.startT}s..${e.endT}s  ${e.frames} frame(s), ${Math.round(e.peak * 100)}% of the screen, jumped in one frame (${e.abruptness})`);
      if (r.extent) console.log(`  changed area: ${r.extent.width}x${r.extent.height} (${r.extent.coverage}% of the screen), about every ${r.cadenceSeconds}s`);
      if (one.evidence.gif) console.log(`  gif: ${one.evidence.gif}`);
      if (one.evidence.filmstrip) console.log(`  filmstrip: ${one.evidence.filmstrip}`);
    } else if (args.sweep && !args.staticOnly) {
      headless = true;
      const table = resolveRoutes(repo, { fixtures: fixturesFromEnv() });
      const { baseUrl } = permitSweep((table.resolved.length + table.assumed.length) * 2);
      temporal = await runSweep({
        baseUrl,
        bearerToken: process.env.GOATOS_BEARER_TOKEN ?? "",
        outDir,
        limit: args.limit,
        fixtures: fixturesFromEnv(),
      });
      const c = temporal.coverage;
      console.log(`${c.fraction} (${c.routesResolved} of ${c.routesInLaneOne} routes resolved, ${c.routesAssumed} built from ids nobody checked, x ${c.viewports.join(" + ")}), ${c.overlaysJudged} overlays judged`);
      // Every gap, with its own sentence. Grouped so a person reads reasons, not rows.
      const byReason = new Map();
      for (const gap of c.pagesNotJudged) byReason.set(gap.why, (byReason.get(gap.why) ?? 0) + 1);
      for (const [why, n] of [...byReason].sort((a, b) => b[1] - a[1])) console.log(`  not judged x${n}: ${why}`);
    } else if (args.live && !args.staticOnly) {
      headless = true;
      const filming = await liveRoutes({ focus: args.focus, fixtures: fixturesFromEnv() });
      const { baseUrl } = permitSweep(filming.length * 2);
      temporal = await runLive({
        baseUrl,
        bearerToken: process.env.GOATOS_BEARER_TOKEN ?? "",
        outDir,
        routes: filming,
      });
      for (const run of temporal.runs) {
        if (run.error) { console.log(`${run.viewport}:${run.route}: could not be filmed — ${run.error}`); continue; }
        console.log(`${run.viewport}:${run.route}: ${run.capturedFrames} frames at ${run.framesPerSecond}/s — ${run.result.flicker ? "FLICKER" : "no flicker"} (${run.result.reason})`);
      }
    }

    const receipt = buildReceipt({ statik, temporal, headless });
    const file = writeReceipt(receipt, runDir);
    console.log(`receipt: ${file}`);
    if (receipt.status !== "pass") process.exitCode = 1;
  }
}
