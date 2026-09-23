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
import { RECEIPT_VERSION } from "./lib/interactive-surfaces.mjs";
import { buildProbePlan, planSummary, targetRefusal } from "./lib/interactive-surface-probe.mjs";
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

function die(message) {
  console.error(message);
  process.exit(2);
}

async function main() {
  const base = arg("base") ?? "";
  const principal = (arg("principal") ?? "").trim();
  const out = arg("out") ?? path.join(adminWeb, `.probe-receipts/receipt-${Date.now()}.json`);

  const refusal = targetRefusal(base);
  if (refusal) die(`${refusal}\nPass --base http://127.0.0.1:3300 (your local stack).`);
  if (!principal) {
    die(
      "refusing to run with no --principal.\n" +
        "Admin-web compiles a different set of controls per permission set, so a reading that does " +
        "not say whose screen it is describes nobody.",
    );
  }

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

  let lock;
  try {
    lock = openSync(LOCK, "wx");
  } catch {
    die(`refusing: ${LOCK} exists, so a probe is already running.\nIt does not queue. Remove the lock only if you are sure nothing is open.`);
  }

  const entries = JSON.parse(readFileSync(LEDGER, "utf8")).entries.filter(
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
      for (let pass = 0; pass < step.repeats; pass += 1) {
        await page.goto(new URL(step.route, base).toString(), { waitUntil: "networkidle", timeout: 30_000 });
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
          route: step.route,
          viewport: step.viewport,
          surfaceKey: surface.key,
          subject: `the controls a person meets on the ${surface.kind} at ${surface.where}`,
          readings: readingsFor.get(surface.key),
        });
      }
      console.log(`read ${step.surfaces.length} surfaces on ${step.route} at ${step.viewport}px`);
    }
  } finally {
    await browser?.close();
    closeSync(lock);
    if (existsSync(LOCK)) unlinkSync(LOCK);
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

main().catch((error) => {
  if (existsSync(LOCK)) unlinkSync(LOCK);
  die(String(error?.stack ?? error));
});
