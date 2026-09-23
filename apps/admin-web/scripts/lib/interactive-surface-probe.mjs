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
