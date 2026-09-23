// May this sweep run, and where is it allowed to point?
//
// On 2026-09-23 the automation took production down: ~14 agents, 18 headless
// browsers, all aimed at the live API, which has 20 concurrent slots. The rules
// that came out of it live in run-oci.sh — one flock, a refusal to start while
// any browser is alive, concurrency capped at 4, 150ms between requests.
//
// `check-mobile-flicker.mjs --sweep` was written after those rules and outside
// them. It opens its own browser, walks 146 routes at two widths — 292 page
// loads — and, worst of all, defaulted to https://dashboard.mesha.sg when no
// base url was given. Nothing stopped two of them running at once. §1 records
// the guarantee as "a second sweep refuses and exits 2"; this one would not.
//
// Two halves, and both are here:
//
//   1. THE LIVE FARM IS NEVER THE DEFAULT. Saying nothing gets you nothing, not
//      production. That default is how the last one happened.
//   2. ONE LOCK, NOT A SECOND ONE. This does not open its own lock file — a
//      second lock is a second authority and the two would not see each other.
//      It refuses unless it is already running inside run-oci.sh's flock, which
//      is the lock §1 is about.
//
// Pure enough to test: every decision is a function of the environment and two
// injected facts, so the tests need no browser and no network.

/** Hosts that serve the farm. A sweep reaches these only when asked, out loud. */
export const PROTECTED_HOSTS = Object.freeze([
  "dashboard.mesha.sg",
  "api.goatos.mesha.sg",
  "stg-api.dashboard.mesha.sg",
]);

/** The env var run-oci.sh sets once it holds the flock. Nothing else may set it. */
export const LOCK_HELD_ENV = "GOATOS_DASHBOARD_LOCK_HELD";

export function isProtectedHost(hostname) {
  const host = String(hostname ?? "").toLowerCase();
  return PROTECTED_HOSTS.some((h) => host === h || host.endsWith(`.${h}`));
}

/** local | protected | other — `other` is a staging or review host nobody named. */
export function classifyTarget(baseUrl) {
  let host;
  try { host = new URL(baseUrl).hostname; } catch { return "unparseable"; }
  if (isProtectedHost(host)) return "protected";
  if (host === "localhost" || host === "127.0.0.1" || host.endsWith(".localhost")) return "local";
  return "other";
}

/**
 * Where may this sweep point, and may it run at all?
 *
 * Throws with a sentence a person can act on. Never returns a default.
 *
 * @param {object} args
 * @param {Record<string,string|undefined>} args.env
 * @param {number} args.browsersRunning  how many headless browsers are already alive
 * @param {number} args.pageLoads        how many pages this run would open
 */
export function assertSweepPermitted({ env = {}, browsersRunning = 0, pageLoads = 0 } = {}) {
  const baseUrl = (env.GOATOS_ADMIN_WEB_BASE_URL ?? "").trim();
  if (!baseUrl) {
    throw new Error(
      "This sweep opens a browser against whatever it is pointed at, so it will not guess. "
      + "Set GOATOS_ADMIN_WEB_BASE_URL to the site you want swept. It used to default to the live farm dashboard, which is how the 2026-09-23 outage happened.",
    );
  }
  const kind = classifyTarget(baseUrl);
  if (kind === "unparseable") {
    throw new Error(`GOATOS_ADMIN_WEB_BASE_URL is not a web address this sweep can read (${baseUrl}).`);
  }
  if (kind === "protected") {
    if (env.GOATOS_SWEEP_ALLOW_PRODUCTION !== "1") {
      throw new Error(
        `${baseUrl} is the live farm. This sweep would open ${pageLoads || "hundreds of"} pages on it. `
        + "If that is genuinely what you want, ask for it out loud by setting GOATOS_SWEEP_ALLOW_PRODUCTION=1 — and read §1 of the automation handover first.",
      );
    }
  }
  // The lock covers every target, not just the farm: two sweeps of a staging box
  // still queue behind each other's requests, and the point of §1 is that a
  // second sweep REFUSES rather than queues.
  if (env[LOCK_HELD_ENV] !== "1") {
    throw new Error(
      "This sweep must run inside the automation run lock, so a second one refuses instead of queueing. "
      + "Start it through tools/dashboard-automation/run-oci.sh, which takes the flock, refuses to start while a browser is already alive, caps concurrency and paces requests. "
      + "Do not add a second lock here: two locks do not see each other.",
    );
  }
  if (browsersRunning > 0) {
    throw new Error(
      `${browsersRunning} headless browser(s) are already running. Stopping the agent that started them does not stop them, `
      + "which is how the first attempt to stop the 2026-09-23 sweep did nothing. Close them before sweeping.",
    );
  }
  return { baseUrl, target: kind };
}

/** Milliseconds to wait between page loads, from the same knob run-oci.sh sets. */
export function requestDelayMs(env = {}) {
  const raw = Number(env.GOATOS_SMOKE_REQUEST_DELAY_MS ?? 150);
  return Number.isFinite(raw) && raw >= 0 ? raw : 150;
}
