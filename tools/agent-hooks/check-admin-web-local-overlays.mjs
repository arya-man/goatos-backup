#!/usr/bin/env node
// Prevents same-page drawers from being implemented as Next.js route navigations.
// A route navigation re-runs Server Components and authenticated reads before the
// overlay mounts, which creates flicker and double-click behavior under latency.

import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const routeDrivenVeilLink = /<Link\b(?:(?!\/>)[\s\S]){0,800}?className\s*=\s*["']veil["'](?:(?!\/>)[\s\S]){0,800}?\/>/g;
const routeDrivenNamedOverlayLink = /<(?:Link|a)\b(?:(?!>)[\s\S]){0,500}?href\s*=\s*\{[^}\n]*(?:drawer|overlay)[^}\n]*\}(?:(?!>)[\s\S]){0,500}?>/gi;
const routeDrivenScheduleOpen = /<a\b(?:(?!>)[\s\S]){0,500}?href\s*=\s*\{(?:shedDrawerHref|drawerPageHref)\([^}]*\}\s*(?:(?!>)[\s\S]){0,500}?>/g;
const routeDrivenScheduleClose = /<ScheduleDrawerCloseForm\b(?:(?!>)[\s\S]){0,500}?href\s*=/g;

// Detect ripgrep availability; fallback to pure-Node directory walk if not installed
function rgAvailable() {
  try {
    execFileSync("rg", ["--version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

// Pure-Node recursive directory walker to find .tsx files (fallback for systems without ripgrep)
function findTsxFilesNodeWalk(baseDir) {
  const files = [];
  const visited = new Set();

  function walk(dir) {
    const realPath = resolve(dir);
    // Prevent infinite loops from symlinks
    if (visited.has(realPath)) return;
    visited.add(realPath);

    try {
      for (const entry of readdirSync(dir, { withFileTypes: true })) {
        const fullPath = resolve(dir, entry.name);
        if (entry.isDirectory()) {
          // Skip node_modules and .git
          if (entry.name === "node_modules" || entry.name === ".git") continue;
          walk(fullPath);
        } else if (entry.isFile() && entry.name.endsWith(".tsx")) {
          files.push(fullPath);
        }
      }
    } catch {
      // Skip directories we cannot read
    }
  }

  walk(baseDir);
  return files;
}

function routeDrivenOverlayFindings(files, readText) {
  const findings = [];
  for (const file of files) {
    const source = readText(file);
    const checks = [
      [routeDrivenVeilLink, "uses a Next Link as a drawer veil/close control"],
      [routeDrivenNamedOverlayLink, "uses a Next/native link whose target is named as a drawer/overlay route"],
      [routeDrivenScheduleOpen, "uses a native route navigation to open or paginate a schedule drawer"],
      [routeDrivenScheduleClose, "uses route navigation to close a schedule drawer"],
    ];
    for (const [pattern, message] of checks) {
      pattern.lastIndex = 0;
      const count = source.match(pattern)?.length ?? 0;
      if (count > 0) {
        findings.push(`${file}: ${message} (${count}); same-page overlays must use client-local state plus history/hash synchronization`);
      }
    }
  }
  return findings;
}

function selfTest() {
  const badVeil = "apps/admin-web/features/bad-veil.tsx";
  const badSchedule = "apps/admin-web/features/bad-schedule.tsx";
  const fixtures = new Map([
    [badVeil, '<Link\n href={closeHref}\n replace\n className="veil"\n aria-label="Close"\n />\n<aside className="drawer on" />'],
    [badSchedule, '<a href={shedDrawerHref(row)} className="celllink">Open</a>\n<ScheduleDrawerCloseForm href={closeHref} />'],
    ["apps/admin-web/features/good.tsx", '<LocalOverlayLink href={href}>Open</LocalOverlayLink>\n<button className={`scrim${open ? " on" : ""}`} />'],
  ]);
  const findings = routeDrivenOverlayFindings([...fixtures.keys()], (file) => fixtures.get(file) ?? "");
  if (
    findings.length !== 4
    || !findings.some((finding) => finding.includes(badVeil))
    || findings.filter((finding) => finding.includes(badSchedule)).length !== 3
    || findings.some((finding) => finding.includes("good.tsx"))
  ) {
    throw new Error(`self-test: expected all route-driven overlay variants and no local-overlay finding, got ${JSON.stringify(findings)}`);
  }
  console.log("admin-web local-overlay guard: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

let files;
const featuresDir = resolve(repo, "apps/admin-web/features");

if (rgAvailable()) {
  // Use ripgrep if available (faster, more reliable)
  try {
    files = execFileSync("rg", ["--files", "apps/admin-web/features", "-g", "*.tsx"], { cwd: repo, encoding: "utf8" })
      .split("\n")
      .map((file) => file.trim())
      .filter(Boolean);
  } catch {
    // Fallback if rg command fails
    files = findTsxFilesNodeWalk(featuresDir).map((file) => file.slice(repo.length + 1));
  }
} else {
  // Fallback to pure-Node implementation on systems without ripgrep
  files = findTsxFilesNodeWalk(featuresDir).map((file) => file.slice(repo.length + 1));
}
const findings = routeDrivenOverlayFindings(files, (file) => readFileSync(resolve(repo, file), "utf8"));

const requiredWiring = [
  ["apps/admin-web/components/local-overlay-link.tsx", ["data-local-overlay-navigation", "window.history.pushState", "nextUrl.href === window.location.href", "LOCAL_OVERLAY_URL_CHANGE_EVENT", "useLocalOverlaySelection", "popstate", "Escape"]],
  ["apps/admin-web/components/local-overlay-drawer.tsx", ["useLocalOverlaySelection", "className={`scrim", "inert={!drawerOpen}"]],
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

console.log(`admin-web local-overlay guard: ok (${files.length} feature files scanned; zero route-driven overlays)`);
