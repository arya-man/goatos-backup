#!/usr/bin/env node

// sidebar-typography-guard — TWO NAVIGATION-COPY RULES (maintainer, 2026-09-02).
//
// RULE 1 — ONE SIDEBAR LABEL SIZE. The left rail stacks three kinds of row in one
// column: flat lens links (`.nav` — Control Tower, Verify), vertical group headers
// (`.ggrp` — Counts, Weight, Sales) and their leaves (`.leaf` — Sales board,
// Purchase and Born). They are all navigation labels, so they are all ONE size. They
// had drifted to 14 / 11 / 13px, which made a single list read as three lists
// stacked and made a group header look like a caption for the row above it rather
// than a peer of it. `Verify` is the reference row; every label is 14px.
//
// Only SIZE is unified here. Weight, letter-spacing and text-transform stay
// per-row on purpose — a group header is still bolder than its leaves.
//
// RULE 2 — PURCHASE AND BORN IS NOT SHOUTED. The generic `.crumb` rule uppercases
// every breadcrumb, which rendered the Sales load page's own name as
// "SALES · PURCHASE AND BORN". A load is bought or born; those are ordinary farm
// words, not a system code, and the page turns the transform off for its own
// crumb. Two halves must both hold, because either one alone is inert:
//   (a) the CSS override `.sales-loads-page .crumb { text-transform: none }`
//   (b) the page root in sales-loads.tsx actually carrying `sales-loads-page`
// A class nothing renders, or a rendered class with no rule behind it, is exactly
// the silently-scaffolded shape this repo keeps catching after the fact.
//
// BLIND SPOTS (stated per the guard-honesty rule; review owns these):
//   - it reads the DECLARED font-size in mesha-theme.css, not the computed one.
//     A later `.side .leaf{font-size:...}` override, an inline style, or a
//     media-query variant is out of reach — the responsive block is deliberately
//     not scanned, since a narrow-viewport tweak is a legitimate exception.
//   - it does not check weight, colour, spacing or the mock's own values.
//   - it is admin-web only; Android's bottom bar/drawer has its own rules.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const CSS = "apps/admin-web/app/mesha-theme.css";
const PAGE = "apps/admin-web/features/procurement/sales-loads.tsx";

// The three sidebar label selectors, each matched at its own top-level rule so a
// descendant rule (`.layout.rail .side .nav`) cannot be mistaken for the base one.
const LABEL_RULES = [".nav", ".ggrp", ".leaf"];

function ruleBody(css, selector) {
  // Match `<selector>{...}` where the selector stands alone — not `.parked .ggrp`,
  // not `.nav.on`, not `.ggrp .ic`.
  const re = new RegExp(String.raw`(^|[\n,])\s*` + selector.replace(".", String.raw`\.`) + String.raw`\s*\{([^}]*)\}`, "m");
  const match = re.exec(css);
  return match ? match[2] : null;
}

function declaredFontSize(body) {
  const match = /(?:^|;)\s*font-size\s*:\s*([^;}]+)/.exec(body);
  return match ? match[1].trim() : null;
}

function sizeFailures(css) {
  const failures = [];
  const sizes = new Map();
  for (const selector of LABEL_RULES) {
    const body = ruleBody(css, selector);
    if (body === null) {
      failures.push(`${CSS}: no base rule found for sidebar label selector \`${selector}\` — the guard cannot prove the sizes agree`);
      continue;
    }
    const size = declaredFontSize(body);
    if (size === null) {
      failures.push(`${CSS}: \`${selector}\` declares no font-size — sidebar labels state their size explicitly so drift is visible in the diff`);
      continue;
    }
    sizes.set(selector, size);
  }
  const distinct = new Set(sizes.values());
  if (distinct.size > 1) {
    const shown = [...sizes].map(([sel, size]) => `${sel}=${size}`).join(", ");
    failures.push(`${CSS}: sidebar label rules disagree on font-size (${shown}). All navigation labels render at one size — the size of \`Verify\`.`);
  }
  return failures;
}

function crumbFailures(css, page) {
  const failures = [];
  const body = ruleBody(css, ".sales-loads-page .crumb");
  if (body === null || !/text-transform\s*:\s*none/.test(body)) {
    failures.push(`${CSS}: missing \`.sales-loads-page .crumb { text-transform: none }\` — without it the generic .crumb rule renders the page as "PURCHASE AND BORN"`);
  }
  if (!/className=\{?["'`][^"'`]*\bsales-loads-page\b/.test(page)) {
    failures.push(`${PAGE}: page root no longer carries the \`sales-loads-page\` class — the crumb override matches nothing and the label goes back to capitals`);
  }
  return failures;
}

function selfTest() {
  const good = `
  .nav{display:flex;color:var(--sidebar-ink);font-size:14px;font-weight:550}
  .ggrp{display:flex;font-size:14px;font-weight:700;text-transform:uppercase}
  .leaf{display:flex;font-size:14px;font-weight:500}
  .sales-loads-page .crumb{text-transform:none;letter-spacing:normal}
`;
  const goodPage = `return (<div className="screen on sales-loads-page">`;

  const cases = [
    ["clean fixture passes", [...sizeFailures(good), ...crumbFailures(good, goodPage)].length, 0],
    // A leaf left at its old 13px is the exact drift this rule exists to stop.
    ["drifted leaf size caught", sizeFailures(good.replace("font-size:14px;font-weight:500", "font-size:13px;font-weight:500")).length, 1],
    // A rule that states no size at all reads as "inherits" and hides the drift.
    ["missing font-size caught", sizeFailures(good.replace("font-size:14px;font-weight:700;", "font-weight:700;")).length, 1],
    // The deceptive halves: rule without class, and class without rule.
    ["crumb rule removed caught", crumbFailures(good.replace(/\.sales-loads-page[^\n]*\n/, ""), goodPage).length, 1],
    ["page class removed caught", crumbFailures(good, `return (<div className="screen on">`).length, 1],
    // A descendant rule must not be mistaken for the base one: `.parked .ggrp`
    // alone leaves the real `.ggrp` unproven.
    ["descendant-only rule not counted as the base", sizeFailures(good.replace(/^  \.ggrp\{.*$/m, "  .parked .ggrp{font-size:14px}")).length, 1],
  ];

  let failed = false;
  for (const [name, actual, expected] of cases) {
    if (actual !== expected) {
      console.error(`self-test FAIL: ${name} (expected ${expected} failure(s), got ${actual})`);
      failed = true;
    }
  }
  if (failed) process.exit(1);
  console.log("sidebar-typography-guard self-test: PASS");
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }
  const css = readFileSync(resolve(repo, CSS), "utf8");
  const page = readFileSync(resolve(repo, PAGE), "utf8");
  const failures = [...sizeFailures(css), ...crumbFailures(css, page)];
  if (failures.length > 0) {
    console.error("sidebar-typography-guard: FAIL");
    for (const failure of failures) console.error(`  - ${failure}`);
    process.exit(1);
  }
  console.log("sidebar-typography-guard: PASS");
}

main();
