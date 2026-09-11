#!/usr/bin/env node

// sales-pages-guard — THE SALES SPLIT (maintainer decisions 2026-09-11, PR 238).
//
// The Sales board (/sales) was divided into Sold (/sales/sold: what has sold, deals ledger
// LAST) and Farm value (/sales/farm-value: the live-herd valuation) and RETIRED; /sales only
// redirects. Five rules fell out of that day's review and instructions, and each was
// already broken once or nearly so, which is why they are machine-checked:
//
// RULE 1 — A PAGE THAT OWNS ITS PARK CHOICE HIDES THE SHELL'S. Every admin-web page that
// renders `<SalesFarmToggle` carries the park choice in its own farm chips on the `farm`
// parameter, so its PAGE_PATH must be in `PAGES_OWNING_PARK_SCOPE` in mesha-shell.tsx, or
// the top bar shows a second selector the page never reads (the P2 in the PR 238 review:
// the lock named the retired "/sales" and neither new page). The retired path itself must
// NOT be listed -- nothing renders there.
//
// RULE 2 — THE BOARD STAYS RETIRED. No `sales.tsx` board component, no `page("sales", ...)`
// contract, no `sales-board` nav leaf; the /sales route file only redirects to Sold.
//
// RULE 3 — THE LEDGER IS LAST ON SOLD ("the deals table keep it at last").
//
// RULE 4 — FARM VALUE CARDS SAY MALE AND FEMALE, NOTHING ELSE ("don't show missing",
// "remove how many we weighed count"): no `sex_missing_count`, no `weighed_animals`.
//
// RULE 5 — THE VISUAL SMOKE HITS THE PAGES THAT RENDER: /sales/sold and /sales/farm-value
// in both the live smoke route list and its coverage test, so a regression on either
// page cannot hide behind the redirect (the PR 238 review's test gap).
//
// BLIND SPOTS (stated per the guard-honesty rule; review owns these): every rule is a
// source-text check. It cannot see a park control composed by a different component, a
// ledger rendered through an indirection the regex does not name, or a runtime-only
// reintroduction of the retired page. It is admin-web + backend contract text only.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const SHELL = "apps/admin-web/components/mesha-shell.tsx";
const FEATURES = "apps/admin-web/features";
const BOARD = "apps/admin-web/features/procurement/sales.tsx";
const BOARD_ROUTE = "apps/admin-web/app/(admin)/sales/page.tsx";
const SOLD = "apps/admin-web/features/procurement/sales-sold.tsx";
const FARM_VALUE = "apps/admin-web/features/procurement/sales-farm-value.tsx";
const SERVICE = "backend/internal/adminui/app/service.go";
const SMOKE = "apps/admin-web/scripts/smoke-visual-live.mjs";
const SMOKE_COVERAGE = "apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs";

function walk(dir) {
  const out = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) out.push(...walk(full));
    else if (/\.tsx?$/.test(entry)) out.push(full);
  }
  return out;
}

/** RULE 1. `pages` is a map of file -> source for every feature file; `shell` is mesha-shell.tsx. */
export function parkScopeFailures(shell, pages) {
  const failures = [];
  const lock = /const PAGES_OWNING_PARK_SCOPE = \[([\s\S]*?)\];/.exec(shell);
  if (!lock) {
    failures.push(`${SHELL}: PAGES_OWNING_PARK_SCOPE is gone; the shell can no longer hide its park selector on pages that own the choice`);
    return failures;
  }
  const locked = new Set([...lock[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]));
  if (locked.has("/sales")) {
    failures.push(`${SHELL}: PAGES_OWNING_PARK_SCOPE lists the retired "/sales"; nothing renders there, list the pages that do`);
  }
  for (const [file, source] of Object.entries(pages)) {
    if (!/<SalesFarmToggle\b/.test(source)) continue;
    const path = /const PAGE_PATH = "([^"]+)";/.exec(source);
    if (!path) {
      failures.push(`${file}: renders SalesFarmToggle but declares no PAGE_PATH, so the shell cannot know to hide its park selector here`);
      continue;
    }
    if (!locked.has(path[1])) {
      failures.push(`${file}: renders its own farm chips on ${path[1]} but that path is not in PAGES_OWNING_PARK_SCOPE (${SHELL}); the top bar will show a second park selector`);
    }
  }
  return failures;
}

/** RULE 2. */
export function boardRetiredFailures({ boardExists, boardRoute, service }) {
  const failures = [];
  if (boardExists) failures.push(`${BOARD} exists; the Sales board was retired on 2026-09-11 and its blocks live on Sold and Farm value`);
  if (!/redirect\(/.test(boardRoute) || /requireAdminWebPageContract|from "@\/features\/procurement"/.test(boardRoute)) {
    failures.push(`${BOARD_ROUTE}: /sales must only redirect to /sales/sold, never render a page`);
  }
  if (/page\("sales",\s*"\/sales"/.test(service)) failures.push(`${SERVICE}: a "sales" page contract is served again; the board is retired`);
  if (/navLeaf\("sales-board"/.test(service)) failures.push(`${SERVICE}: the sales-board nav leaf is back; the Sales group reads Sold, Farm value, Purchase and Born, Vendors, Sales Config`);
  return failures;
}

/** RULE 3. */
export function ledgerLastFailures(sold) {
  const ledger = sold.lastIndexOf("sales-deals-table");
  const lastBlock = sold.lastIndexOf("evidence.audit.title");
  if (ledger < 0) return [`${SOLD}: the deals ledger (sales-deals-table) is missing from Sold`];
  if (lastBlock < 0 || ledger < lastBlock) return [`${SOLD}: the deals ledger must be the LAST block on Sold, after the sale evidence`];
  return [];
}

/** RULE 4. */
export function farmValueCardFailures(farmValue) {
  const failures = [];
  if (/sex_missing_count|value\.sex\.missing/.test(farmValue)) failures.push(`${FARM_VALUE}: renders the missing-sex count; the cards say Male and Female only`);
  if (/weighed_animals|value\.weighed"/.test(farmValue)) failures.push(`${FARM_VALUE}: renders the weighed-animal count; it was removed from the fattening card`);
  if (!/bucket\.male_count/.test(farmValue) || !/bucket\.female_count/.test(farmValue)) failures.push(`${FARM_VALUE}: the male/female split is gone from the valuation cards`);
  return failures;
}

/** RULE 5. */
export function smokeFailures(smoke, coverage) {
  const failures = [];
  for (const [file, source] of [[SMOKE, smoke], [SMOKE_COVERAGE, coverage]]) {
    for (const route of ["/sales/sold", "/sales/farm-value"]) {
      if (!source.includes(`"${route}?`) && !source.includes(`"${route}"`)) failures.push(`${file}: no smoke route for ${route}; a regression there hides behind the /sales redirect`);
    }
  }
  return failures;
}

function selfTest() {
  const shell = `const PAGES_OWNING_PARK_SCOPE = [\n  "/counts/breakdown",\n  "/sales/sold",\n  "/sales/farm-value",\n];`;
  const sold = `const PAGE_PATH = "/sales/sold";\n<SalesFarmToggle pageContract={pageContract} />\n copy(pageContract, "evidence.audit.title")\n <table className="sales-deals-table">`;
  const farmValue = `const PAGE_PATH = "/sales/farm-value";\n<SalesFarmToggle />\n {num(bucket.male_count)} {num(bucket.female_count)}`;
  const pages = { [SOLD]: sold, [FARM_VALUE]: farmValue };
  const boardRoute = `import { redirect } from "next/navigation";\nredirect("/sales/sold");`;
  const service = `navLeaf("sales-sold", "Sold", "/sales/sold", nil)\npage("sales-sold", "/sales/sold", ...)`;
  const smoke = `{ name: "sales-sold", path: "/sales/sold?scope_mode=company" },\n{ name: "sales-farm-value", path: "/sales/farm-value?scope_mode=company" },`;
  const cases = [
    ["clean fixture passes", [
      ...parkScopeFailures(shell, pages),
      ...boardRetiredFailures({ boardExists: false, boardRoute, service }),
      ...ledgerLastFailures(sold), ...farmValueCardFailures(farmValue), ...smokeFailures(smoke, smoke),
    ].length, 0],
    // The exact PR 238 defect: the lock names the retired path and neither page that renders.
    ["retired /sales in the lock and a page missing caught", parkScopeFailures(shell.replace('"/sales/sold"', '"/sales"'), pages).length, 2],
    ["a new page with farm chips but no PAGE_PATH caught", parkScopeFailures(shell, { x: "<SalesFarmToggle />" }).length, 1],
    ["lock removed caught", parkScopeFailures("const other = 1;", pages).length, 1],
    ["board component back caught", boardRetiredFailures({ boardExists: true, boardRoute, service }).length, 1],
    ["board route rendering a page caught", boardRetiredFailures({ boardExists: false, boardRoute: `import { SalesPage } from "@/features/procurement";`, service }).length, 1],
    ["board contract and leaf back caught", boardRetiredFailures({ boardExists: false, boardRoute, service: `navLeaf("sales-board", "Sales", "/sales", nil)\npage("sales", "/sales", "/sales", ...)` }).length, 2],
    ["ledger before the evidence caught", ledgerLastFailures(`<table className="sales-deals-table">\n copy(pageContract, "evidence.audit.title")`).length, 1],
    ["missing-sex count back caught", farmValueCardFailures(farmValue + " {bucket.sex_missing_count}").length, 1],
    ["weighed count back caught", farmValueCardFailures(farmValue + " {bucket.weighed_animals}").length, 1],
    ["split removed caught", farmValueCardFailures(`const PAGE_PATH = "/sales/farm-value";`).length, 1],
    ["smoke route missing caught", smokeFailures(smoke.replace(/\{ name: "sales-farm-value"[^\n]*\n?/, ""), smoke).length, 1],
  ];
  let failed = false;
  for (const [name, actual, expected] of cases) {
    if (actual !== expected) {
      console.error(`self-test FAIL: ${name} (expected ${expected} failure(s), got ${actual})`);
      failed = true;
    }
  }
  if (failed) process.exit(1);
  console.log("sales-pages-guard self-test: PASS");
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }
  const read = (rel) => readFileSync(resolve(repo, rel), "utf8");
  const pages = Object.fromEntries(walk(resolve(repo, FEATURES)).map((f) => [f.slice(repo.length + 1), readFileSync(f, "utf8")]));
  const failures = [
    ...parkScopeFailures(read(SHELL), pages),
    ...boardRetiredFailures({ boardExists: existsSync(resolve(repo, BOARD)), boardRoute: read(BOARD_ROUTE), service: read(SERVICE) }),
    ...ledgerLastFailures(read(SOLD)),
    ...farmValueCardFailures(read(FARM_VALUE)),
    ...smokeFailures(read(SMOKE), read(SMOKE_COVERAGE)),
  ];
  if (failures.length > 0) {
    console.error("sales-pages-guard: FAIL");
    for (const failure of failures) console.error(`  - ${failure}`);
    process.exit(1);
  }
  console.log("sales-pages-guard: PASS");
}

main();
