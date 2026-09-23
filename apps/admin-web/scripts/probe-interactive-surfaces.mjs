#!/usr/bin/env node
//
// The supervised probe run. ONE command; the coordinator runs it, not an agent.
//
//   npm --prefix apps/admin-web run probe:interactive-surfaces -- \
//     --base http://127.0.0.1:3300 --principal "<who is signed in>"
//
// WHAT IT DOES. Walks the plan built offline by lib/interactive-surface-probe.mjs -- route by
// route, 1440 then 390, every surface on that route, each read twice -- and writes ONE receipt of
// what it SAW. It grades nothing and promotes nothing: a later offline pass reads the receipt,
// keeps only readings that agree, and turns those into expectations. That split is the point. The
// run records; the gate decides.
//
// WHAT IT REFUSES, before it opens anything:
//   * any host that is not a local stack -- allow-list, so a deployed host nobody listed is still
//     refused. dashboard.mesha.sg, api.goatos.mesha.sg and stg-api are named as well, because stg
//     IS production data and pointing automation at it is what took production down on 2026-09-23.
//   * a second copy of itself, via a run lock. Parallel browser sweeps are the incident.
//   * running with no --principal. On a role-agnostic page a reading with no principal describes
//     nobody, and a receipt that cannot say whose screen it recorded is worthless.
//
// It opens ONE browser, ONE page, serially. There is no concurrency option because there is no
// concurrency; adding one is the incident.

import { mkdirSync, writeFileSync, openSync, closeSync, unlinkSync, existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { RECEIPT_VERSION, ledgerDrift, scanInteractiveSurfaces } from "./lib/interactive-surfaces.mjs";
import { readSourceFiles } from "./check-interactive-surfaces.mjs";
import { buildProbePlan, describeLock, navigationRefusal, planSummary, principalRefusal, targetRefusal } from "./lib/interactive-surface-probe.mjs";
import { contractRevisionRefusal } from "./lib/reading-comparison.mjs";

const adminWeb = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const LEDGER = path.join(adminWeb, "scripts/interactive-surface-ledger.json");
const LOCK = path.join(adminWeb, ".interactive-surface-probe.lock");

function arg(name) {
  const index = process.argv.indexOf(`--${name}`);
  return index >= 0 ? process.argv[index + 1] : undefined;
}

function bearerHeaders() {
  const token = process.env.GOATOS_BEARER_TOKEN;
  return token ? { authorization: `Bearer ${token}` } : {};
}

// Set once this process owns the lock, so every exit path releases it. A refusal AFTER the lock
// was taken -- the API being down, for instance -- used to leave it behind, and the next run then
// refused for the WRONG reason. That direction is silence, which is safe, but it teaches an
// operator to delete locks, and deleting a live one starts a second sweep beside a first.
let holdsLock = false;

function releaseLock() {
  if (!holdsLock) return;
  holdsLock = false;
  try {
    if (existsSync(LOCK)) unlinkSync(LOCK);
  } catch {
    /* nothing useful to do while exiting */
  }
}

function die(message) {
  releaseLock();
  console.error(message);
  process.exit(2);
}

async function main() {
  const base = arg("base") ?? "";
  const principal = (arg("principal") ?? "").trim();
  const out = arg("out") ?? path.join(adminWeb, `.probe-receipts/receipt-${Date.now()}.json`);

  const refusal = targetRefusal(base);
  if (refusal) die(`${refusal}\nPass --base http://127.0.0.1:3300 (your local stack).`);
  const whoRefusal = principalRefusal(principal);
  if (whoRefusal) {
    die(
      `${whoRefusal}\n` +
        "Pass --principal with the role or person the browser is signed in as. Note this is NOT " +
        "verified: a wrong-but-plausible label is not caught here or later.",
    );
  }

  const ledger = JSON.parse(readFileSync(LEDGER, "utf8"));
  // The ledger must still describe the source it was written from. An entry whose surface has
  // moved carries a selector and an expected value derived from the OLD line: if that selector
  // matches some other element, two agreeing readings of the WRONG element get promoted and every
  // later run accuses a correct page. A wrong expectation, not a missing one, so refuse first.
  const drift = ledgerDrift(scanInteractiveSurfaces(readSourceFiles()), ledger.entries);
  if (drift.length) {
    console.error(`refusing: the ledger no longer describes the source (${drift.length} differences).`);
    for (const line of drift.slice(0, 5)) console.error(`- ${line}`);
    if (drift.length > 5) console.error(`- ...and ${drift.length - 5} more`);
    die("Run `npm --prefix apps/admin-web run refresh:interactive-surfaces` and re-read what changed before probing.");
  }
  let lock;
  try {
    lock = openSync(LOCK, "wx");
    holdsLock = true;
    writeFileSync(LOCK, `pid=${process.pid} started=${new Date().toISOString()}\n`);
  } catch {
    const held = (() => {
      try {
        return readFileSync(LOCK, "utf8");
      } catch {
        return "";
      }
    })();
    const alive = (pid) => {
      try {
        process.kill(pid, 0);
        return true;
      } catch {
        return false;
      }
    };
    die(`refusing: ${LOCK} exists.\n${describeLock(held, alive)}`);
  }

  // Only now the checks that need something running. Everything above is free and offline, and
  // ordering them after this one meant a stopped stack hid a stale ledger.
  // The contract revision is the BACKEND BUILD SHA, obtained the way every sweep already obtains
  // it, and refused rather than defaulted. `unknown` on a receipt makes two different builds look
  // like one, which is worse than not running -- readings would be compared across a contract
  // change nobody could see.
  const apiBase = arg("api-base") ?? base.replace(/:\d+$/, ":8080");
  const apiRefusal = targetRefusal(apiBase);
  if (apiRefusal) die(`${apiRefusal}\n(--api-base is the local API this probe reads /version from.)`);
  let apiBuildSha = "";
  try {
    const version = await fetch(new URL("/version", apiBase).toString(), { headers: bearerHeaders() }).then((r) => r.json());
    apiBuildSha = String(version?.build_sha ?? "");
  } catch (error) {
    die(`could not read ${apiBase}/version for the build identity: ${error.message}\nStart the local stack first.`);
  }
  const revisionRefusal = contractRevisionRefusal(apiBuildSha);
  if (revisionRefusal) die(`${revisionRefusal}\nThe API must report a real build_sha before a reading is worth recording.`);

  const entries = ledger.entries.filter(
    (entry) => entry.status === "not-checked" && (entry.routes ?? []).length > 0,
  );
  const plan = buildProbePlan(entries);
  const summary = planSummary(plan);
  console.log(
    `probe plan: ${summary.routes} routes, ${summary.pageLoads} page loads, ${summary.surfaceOpenings} surface openings, serial.`,
  );

  const observations = [];
  let browser;
  try {
    const { chromium } = await import("playwright");
    browser = await chromium.launch({ headless: true });
    const page = await browser.newPage();
    for (const step of plan) {
      await page.setViewportSize({ width: step.viewport, height: step.viewport === 390 ? 844 : 900 });
      const readingsFor = new Map(step.surfaces.map((surface) => [surface.key, []]));
      // One page load per reading, not one per surface: the second reading exists to prove the
      // value is stable, and reloading between every surface buys nothing for 30x the time.
      let landedElsewhere = null;
      for (let pass = 0; pass < step.repeats; pass += 1) {
        const response = await page.goto(new URL(step.route, base).toString(), { waitUntil: "networkidle", timeout: 30_000 });
        // A redirect is quiet: the page loads and looks fine. Recording its controls as this
        // route's is how a sign-in page becomes the thing /people owes.
        landedElsewhere = navigationRefusal(step.route, page.url(), response?.status() ?? null);
        if (landedElsewhere) break;
        for (const surface of step.surfaces) {
          readingsFor.get(surface.key).push(
            // Read what a PERSON meets: the accessible names of the controls on this surface, in
            // the order a reader meets them. Never a field path, never a test id (contract §2).
            await page
              .evaluate(() => {
                const root = document.querySelector("[role=dialog], [role=alertdialog], form, [role=menu]");
                if (!root) return null;
                return [...root.querySelectorAll("button, a, input, select, textarea, label, h1, h2, h3")]
                  .filter((el) => el.getClientRects().length > 0)
                  .map((el) => (el.getAttribute("aria-label") || el.textContent || "").replace(/\s+/g, " ").trim())
                  .filter(Boolean);
              })
              .catch(() => null),
          );
        }
      }
      for (const surface of step.surfaces) {
        observations.push({
          id: `${step.route}|${step.viewport}|${surface.key}`,
          // Carried, not dropped: a route that could not be reached is a "not checked" the ledger
          // can read, never an absent observation that looks like nobody tried.
          notReached: landedElsewhere ?? undefined,
          route: step.route,
          viewport: step.viewport,
          surfaceKey: surface.key,
          subject: `the controls a person meets on the ${surface.kind} at ${surface.where}`,
          readings: landedElsewhere ? [] : readingsFor.get(surface.key),
        });
      }
      console.log(
        landedElsewhere
          ? `NOT CHECKED on ${step.route} at ${step.viewport}px — ${landedElsewhere}`
          : `read ${step.surfaces.length} surfaces on ${step.route} at ${step.viewport}px`,
      );
    }
  } finally {
    await browser?.close();
    closeSync(lock);
    releaseLock();
  }

  mkdirSync(path.dirname(out), { recursive: true });
  writeFileSync(
    out,
    `${JSON.stringify(
      {
        version: RECEIPT_VERSION,
        runId: `probe-${new Date().toISOString()}`,
        principal,
        base,
        // The agreed contract revision: coarser than a per-contract hash, real, and never a
        // placeholder. Defined once in lib/reading-comparison.mjs.
        contractRevision: apiBuildSha,
        apiBuildSha,
        observations,
      },
      null,
      2,
    )}\n`,
  );
  console.log(`receipt written to ${out} — ${observations.length} observations. Nothing was graded; run the gate next.`);
}

// A crash is an exit path too, and so is a signal: a lock that outlives its process is the thing
// that gets deleted by hand next time.
for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, () => die(`stopped by ${signal}`));
main().catch((error) => die(String(error?.stack ?? error)));
