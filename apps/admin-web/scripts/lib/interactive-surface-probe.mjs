// The plan and the refusals for the supervised probe run.
//
// Split from the runner on purpose: everything here is PURE and tested offline, so the only
// untested code in the probe is the part that drives a browser. Which surfaces to open, in what
// order, at which widths, and which hosts are refused, are all decided here.
//
// THE RUN IS NOT MINE. This file makes one possible; it opens nothing.

import { REQUIRED_VIEWPORTS } from "./interactive-surfaces.mjs";

/**
 * Hosts this probe must never touch. stg IS production data, and the 2026-09-23 outage was
 * automation pointed at the live API. The check is an ALLOW-list on the host anyway -- the
 * block-list below is second, so a host nobody thought to list still cannot be reached.
 */
export const FORBIDDEN_HOSTS = [
  "dashboard.mesha.sg",
  "api.goatos.mesha.sg",
  "stg-api.dashboard.mesha.sg",
  "goatos-api-stg",
];

const LOCAL_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]", "::1", "0.0.0.0"]);

/**
 * Pure: a refusal reason, or null when the target is a local stack.
 * Deliberately strict -- an unparseable URL is refused, not guessed at.
 */
export function targetRefusal(rawUrl) {
  let url;
  try {
    url = new URL(String(rawUrl));
  } catch {
    return `refusing ${JSON.stringify(String(rawUrl))}: that is not a URL this probe can check`;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    return `refusing ${url.protocol}//: this probe speaks http to a local stack only`;
  }
  const host = url.hostname.toLowerCase();
  for (const forbidden of FORBIDDEN_HOSTS) {
    if (host === forbidden || host.endsWith(`.${forbidden}`) || host.includes(forbidden)) {
      return `refusing ${host}: that is a deployed host, and stg carries production data`;
    }
  }
  if (!LOCAL_HOSTS.has(host)) {
    return `refusing ${host}: this probe runs against a local stack only (${[...LOCAL_HOSTS].join(", ")})`;
  }
  return null;
}

/**
 * The ordered plan. Grouped by ROUTE so one page load serves every surface on it, and by
 * VIEWPORT inside that so the window is resized once rather than per surface. Strictly serial:
 * there is no concurrency parameter to set, because there is no concurrency.
 *
 * @param {Array<{key,kind,path,routes?:string[]}>} entries ledger entries needing a reading
 * @param {number} repeats independent readings per surface; two is the floor for stability
 */
export function buildProbePlan(entries, { repeats = 2 } = {}) {
  if (repeats < 2) throw new Error("a single reading cannot tell a stable label from a timestamp; use at least two");
  const byRoute = new Map();
  for (const entry of entries) {
    for (const route of entry.routes ?? []) {
      if (!byRoute.has(route)) byRoute.set(route, []);
      byRoute.get(route).push(entry);
    }
  }
  const steps = [];
  for (const route of [...byRoute.keys()].sort()) {
    for (const viewport of REQUIRED_VIEWPORTS) {
      const surfaces = byRoute
        .get(route)
        .slice()
        .sort((a, b) => a.key.localeCompare(b.key))
        .map((entry) => ({ key: entry.key, kind: entry.kind, where: entry.where }));
      // `repeats` belongs to the STEP, not to each surface: one page load reads every surface on
      // it, and the second reading comes from loading the page again. Reloading between surfaces
      // instead cost 3,256 reloads for the same evidence.
      steps.push({ route, viewport: Number(viewport), repeats, surfaces });
    }
  }
  return steps;
}

/** What the plan costs, so the person running it knows before they start. */
export function planSummary(steps) {
  const routes = new Set(steps.map((s) => s.route));
  const openings = steps.reduce((n, s) => n + s.surfaces.length * s.repeats, 0);
  return {
    routes: routes.size,
    pageLoads: steps.length,
    surfaceOpenings: openings,
    // What the run actually costs in page loads, which is what takes the time.
    pageLoadsWithRepeats: steps.reduce((n, s) => n + s.repeats, 0),
  };
}

/**
 * Did the browser land on the page we asked for?
 *
 * THE NASTY CASE IS NOT THE 404. A route that does not exist is loud. A route that REDIRECTS
 * somewhere real is quiet: the page loads, the probe finds a perfectly good screen, and records
 * its readings against the route nobody served. §7 records exactly this -- a stale bearer token
 * produced a full page of false negatives, because /version returns 200 unauthenticated and every
 * route redirected to a sign-in page that rendered beautifully.
 *
 * Two readings of a sign-in page AGREE with each other, and agreeing is how a value is promoted.
 * So without this check a redirect would not merely lose a reading -- it would write the sign-in
 * page's controls into the ledger as the thing /people owes, and every later run would accuse the
 * real /people of missing them.
 *
 * @param {string} requested the route we asked for, e.g. "/people"
 * @param {string} landed the URL the browser ended on
 * @param {number|null} status the HTTP status of the main document, null when unknown
 * @returns {string|null} a reason this is not the page we asked for, or null
 */
export function navigationRefusal(requested, landed, status) {
  if (status === null || status === undefined) return "the page did not report a status, so there is no way to tell what was served";
  if (status >= 400) return `${requested} answered ${status}, so nothing on the screen is the page that was asked for`;
  let landedPath;
  try {
    landedPath = new URL(String(landed)).pathname;
  } catch {
    return `the browser ended on ${JSON.stringify(String(landed))}, which is not a URL`;
  }
  const normalise = (p) => (p.length > 1 ? p.replace(/\/+$/, "") : p);
  if (normalise(landedPath) !== normalise(requested)) {
    return (
      `asked for ${requested} and ended on ${landedPath} — the screen rendered, but it is not the ` +
      `one this reading is about, and its controls must never be recorded as that route's`
    );
  }
  return null;
}

/**
 * A principal is a LABEL, and this is the one input the probe cannot verify: nothing here knows
 * who the browser is really signed in as. What it can do is refuse a label that is obviously not
 * an answer, on the same reasoning as the build sha -- a placeholder makes two different people
 * look like one, and every reading taken under it is filed against a screen nobody can identify.
 *
 * STATE THE LIMIT PLAINLY: passing a real-looking but WRONG principal is not caught here, and
 * nothing downstream catches it either. The ledger only checks that the entry and the receipt
 * AGREE about the label, which two matching lies satisfy.
 */
export const PLACEHOLDER_PRINCIPALS = new Set([
  "", "unknown", "dev", "test", "tester", "user", "me", "admin", "someone", "n/a", "na", "tbd", "x", "-",
]);

export function principalRefusal(principal) {
  const value = String(principal ?? "").trim();
  if (!value) {
    return (
      "refusing to run with no principal: this product compiles a different set of controls per " +
      "permission set, so a reading that cannot say whose screen it is describes nobody"
    );
  }
  if (PLACEHOLDER_PRINCIPALS.has(value.toLowerCase())) {
    return `refusing the principal ${JSON.stringify(value)}: that names no one, and readings filed under it can never be compared to another run's`;
  }
  if (value.length < 3) {
    return `refusing the principal ${JSON.stringify(value)}: too short to identify anybody later`;
  }
  return null;
}

/**
 * A lock left behind by a run that was killed blocks the next one forever. That direction is
 * SILENCE, which is the safe one -- refusing to start can only cost time, while auto-removing a
 * lock can start a second sweep beside a first, which is the 2026-09-23 incident. So this never
 * removes anything; it only says whether the process that made the lock is still alive, so a
 * person can decide.
 */
export function describeLock(contents, isAlive) {
  const pid = Number(String(contents ?? "").match(/pid=(\d+)/)?.[1] ?? 0);
  if (!pid) return "a lock is present but does not say which process made it; remove it only if you are sure nothing is open";
  return isAlive(pid)
    ? `process ${pid} is still running — it is a live probe, not a leftover. This does not queue.`
    : `process ${pid} is gone, so this is a leftover from a killed run. Check for stray browsers before removing the lock: stopping a parent does not stop them.`;
}

/**
 * A cheap fingerprint of the source the ledger was written from.
 *
 * MEASURED, not guessed. The probe is 104 page loads over 52 routes, and the ledger is only
 * checked against source at the START -- so a source edit mid-run means later readings are graded
 * against a ledger the earlier ones did not share, with nothing saying which were which.
 *
 *   full re-check (read 623 files + marker scan)   1,130 ms  ->  117.6 s over the run
 *   stat-only fingerprint (623 files)                  14 ms  ->    1.4 s over the run
 *
 * So the fingerprint runs before EVERY step and the expensive re-check runs only when it moves.
 * 1.4 s is not a window worth leaving open. Each observation records the fingerprint in force
 * when it was read, so a later reader can tell exactly which readings shared which source state
 * rather than being told the run "probably" did not drift.
 *
 * @param {Array<{path:string,mtimeMs:number,size:number}>} stats
 */
export function fingerprintOf(stats) {
  let hash = 5381;
  for (const { path, mtimeMs, size } of [...stats].sort((a, b) => a.path.localeCompare(b.path))) {
    for (const ch of `${path}:${Math.trunc(mtimeMs)}:${size}|`) {
      hash = ((hash * 33) ^ ch.charCodeAt(0)) >>> 0;
    }
  }
  return hash.toString(16).padStart(8, "0");
}

/**
 * Who the run was ACTUALLY signed in as, reduced to the thing that decides what a screen shows.
 *
 * --principal is a label a person types, and a wrong-but-plausible one was undetectable: the
 * ledger only checked that entry and receipt AGREE about it, which two matching lies satisfy.
 * `GET /app/me` (contracts/openapi/app-api.yaml:503) returns `actor_id` and `grants`, and a
 * GrantSummary carries role + scope_type + scope_id. Those grants ARE what the page contract is
 * compiled against, so they, not the label, are what makes two runs comparable.
 *
 * Read from the contract, not from a call: nothing here has ever reached a running API.
 *
 * @param {{actor_id?:string, grants?:Array<{role?:string,scope_type?:string,scope_id?:string,status?:string}>}} me
 */
export function identityOf(me) {
  const actorId = String(me?.actor_id ?? "").trim();
  if (!actorId) return { error: "/app/me returned no actor_id, so there is no way to say whose screen was read" };
  const active = (me?.grants ?? []).filter((g) => (g?.status ?? "active") === "active");
  if (active.length === 0) {
    return { error: "/app/me returned no active grants; a principal with no grants sees no controls, so nothing read under it describes a screen anyone uses" };
  }
  // Canonical and order-independent: the same person read twice must reduce to the same string.
  const grants = [...new Set(active.map((g) => `${g.role ?? "?"}@${g.scope_type ?? "?"}:${g.scope_id ?? "?"}`))].sort();
  return { actorId, grants };
}

/** Do two runs describe the same screen? The grants decide, never the label. */
export function identityRefusal(recorded, expected) {
  const a = [...(recorded ?? [])].sort().join(",");
  const b = [...(expected ?? [])].sort().join(",");
  if (!a || !b) return "one of these runs recorded no grants, so they cannot be compared";
  if (a !== b) {
    return `these readings were taken under different grants (${a} vs ${b}); on a role-agnostic page that is a different screen, whatever the two runs called themselves`;
  }
  return null;
}
