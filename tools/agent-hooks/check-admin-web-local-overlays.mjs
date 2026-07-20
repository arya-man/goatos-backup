#!/usr/bin/env node
// Prevents same-page drawers from being implemented as Next.js route navigations.
// A route navigation re-runs Server Components and authenticated reads before the
// overlay mounts, which creates flicker and double-click behavior under latency.

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const legacyBaseline = new Map([
  ["apps/admin-web/features/calendar/calendar-event-drawer.tsx", 1],
  ["apps/admin-web/features/config/config-console.tsx", 1],
  ["apps/admin-web/features/control-tower/index.tsx", 1],
  ["apps/admin-web/features/counts/herd-register.tsx", 1],
  ["apps/admin-web/features/operations-audit/audit-log.tsx", 1],
  ["apps/admin-web/features/operations-dlq/index.tsx", 1],
  ["apps/admin-web/features/preventive-care-vaccination/record-verify-drawer.tsx", 1],
  ["apps/admin-web/features/preventive-care-vaccination/supplier-warmup-context.tsx", 1],
  ["apps/admin-web/features/process-integrity/protocol-adherence.tsx", 1],
  ["apps/admin-web/features/procurement/source-entry-board.tsx", 1],
  ["apps/admin-web/features/vaccination-execution/execution-board.tsx", 1],
  ["apps/admin-web/features/vaccination-sheds/shed-detail.tsx", 1],
  ["apps/admin-web/features/verification-review/verification-review-drawer.tsx", 1],
]);

const routeDrivenVeilLink = /<Link\b(?:(?!\/>)[\s\S]){0,800}?className\s*=\s*["']veil["'](?:(?!\/>)[\s\S]){0,800}?\/>/g;

function routeDrivenOverlayCounts(files, readText) {
  const counts = new Map();
  for (const file of files) {
    const matches = readText(file).match(routeDrivenVeilLink) ?? [];
    if (matches.length > 0) counts.set(file, matches.length);
  }
  return counts;
}

function compareWithBaseline(actual, baseline) {
  const findings = [];
  for (const [file, count] of actual) {
    const allowed = baseline.get(file) ?? 0;
    if (count > allowed) findings.push(`${file}: adds ${count - allowed} Link-driven drawer overlay(s); use LocalOverlayLink plus client-local drawer state`);
  }
  for (const [file, allowed] of baseline) {
    const count = actual.get(file) ?? 0;
    if (count < allowed) findings.push(`${file}: legacy overlay count fell from ${allowed} to ${count}; reduce the baseline in this guard`);
  }
  return findings;
}

function selfTest() {
  const badNew = "apps/admin-web/features/new-page.tsx";
  const old = "apps/admin-web/features/old-page.tsx";
  const fixtures = new Map([
    [badNew, '<Link\n href={closeHref}\n replace\n className="veil"\n aria-label="Close"\n />\n<aside className="drawer on" />'],
    [old, '<Link href={closeHref} className="veil" />'],
    ["apps/admin-web/features/good.tsx", '<LocalOverlayLink href={href}>Open</LocalOverlayLink>\n<button className={`scrim${open ? " on" : ""}`} />'],
  ]);
  const actual = routeDrivenOverlayCounts([...fixtures.keys()], (file) => fixtures.get(file) ?? "");
  const findings = compareWithBaseline(actual, new Map([[old, 1]]));
  if (findings.length !== 1 || !findings[0].includes(badNew)) {
    throw new Error(`self-test: expected one new-overlay finding, got ${JSON.stringify(findings)}`);
  }

  fixtures.set(old, `${fixtures.get(old)}\n<Link href={closeHref} className="veil" />`);
  const increased = compareWithBaseline(routeDrivenOverlayCounts([...fixtures.keys()], (file) => fixtures.get(file) ?? ""), new Map([[old, 1], [badNew, 1]]));
  if (increased.length !== 1 || !increased[0].includes(old)) {
    throw new Error(`self-test: expected an increased-baseline finding, got ${JSON.stringify(increased)}`);
  }

  const stale = compareWithBaseline(new Map(), new Map([[old, 1]]));
  if (stale.length !== 1 || !stale[0].includes("reduce the baseline")) {
    throw new Error(`self-test: expected stale-baseline finding, got ${JSON.stringify(stale)}`);
  }
  console.log("admin-web local-overlay guard: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const files = execFileSync("rg", ["--files", "apps/admin-web/features", "-g", "*.tsx"], { cwd: repo, encoding: "utf8" })
  .split("\n")
  .map((file) => file.trim())
  .filter(Boolean);
const actual = routeDrivenOverlayCounts(files, (file) => readFileSync(resolve(repo, file), "utf8"));
const findings = compareWithBaseline(actual, legacyBaseline);

const requiredWiring = [
  ["apps/admin-web/components/local-overlay-link.tsx", ["data-local-overlay-navigation", "window.history.pushState", "nextUrl.href === window.location.href", "LOCAL_OVERLAY_URL_CHANGE_EVENT"]],
  ["apps/admin-web/components/mesha-shell.tsx", ["anchor.dataset.localOverlayNavigation"]],
  ["apps/admin-web/features/process-integrity/work-board.tsx", ["LocalOverlayLink", "localOverlay"]],
  ["apps/admin-web/features/process-integrity/action-center-local-drawer.tsx", ["ActionCenterLocalDrawer", "currentHistoryEntryIsLocalOverlay"]],
];
for (const [file, needles] of requiredWiring) {
  let text = "";
  try {
    text = readFileSync(resolve(repo, file), "utf8");
  } catch {
    findings.push(`${file}: required local-overlay production wiring is missing`);
    continue;
  }
  for (const needle of needles) {
    if (!text.includes(needle)) findings.push(`${file}: required local-overlay invariant is missing: ${needle}`);
  }
}

if (findings.length > 0) {
  console.error("admin-web local-overlay guard failed:");
  for (const finding of findings) console.error(`- ${finding}`);
  process.exit(1);
}

console.log(`admin-web local-overlay guard: ok (${files.length} feature files scanned; no new route-driven overlays)`);
