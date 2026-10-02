#!/usr/bin/env node
// check-loadwise-stock-valuation.mjs
//
// LOAD WISE VALUES UNSOLD ANIMALS BY WEIGHT x SALES CONFIG ₹/KG (maintainer decision 2026-10-02,
// docs/decisions/loadwise-stock-valuation.md). Sold animals are what they sold for; animals still
// on farm are each one's latest weight x the ₹/kg of its stage, SPECIES and gender on Sales
// Config's Farm valuation; the position is sold + stock - landed cost. The rule replaced one that
// priced a load's remaining animals at that load's own average sold price -- so ONE animal sold for
// ₹16,720 priced all 76 left on load 129 -- and this guard keeps it from coming back.
//
// WHAT IT CHECKS:
//   1. sold-price-values-stock     -- no production procurement Go names the retired per-animal
//                                     basis (AvgSoldPrice, RemainingValue, PriceBasis,
//                                     OverallAvgSoldPrice, UnsoldPriceBasis, loadwiseOverallAvgSQL,
//                                     unsold_stock_price_rupees), and the Load wise screen reads no
//                                     avg_sold_price / remaining_value / price_basis wire field.
//   2. valuation-sql-copied        -- the Sales Config pricing SQL lives ONLY in
//                                     backend/internal/farmvaluation. Any other production Go whose
//                                     SQL reads sales_valuation_assumptions is a second copy of the
//                                     rule (the editor's own read/write, valuation_repository.go,
//                                     is the one exception: it stores the row, it prices nothing).
//   3. shared-rule-not-spliced     -- both pages that price animals (Load wise's
//                                     loadwise_stock_weight.go, Farm value's overview_repository.go)
//                                     splice farmvaluation.PricingCTEs, StageJoinsSQL and
//                                     BucketKeySQL.
//   4. bucket-without-species      -- the bucket key carries the species on both sides: the domain's
//                                     ValuationBucketKey(stage, species, gender) and the SQL
//                                     BucketKeySQL, which must splice the species between stage and
//                                     gender.
//   5. unvalued-stock-reads-as-loss -- FinalizeLoadwise keeps the "no current weight reads ₹0"
//                                     branch, and the seven-case test that pins it still exists.
//   6. realised-split-on-screen    -- the Load wise screen shows profit as one value: no
//                                     realised_profit_loss and no "Realised" copy key.
//
// BLIND SPOTS: names and shapes, not arithmetic. A sold-price basis reintroduced under a new name,
// or a pricing CTE written against another table, slips past rules 1-2; the runtime half is
// TestLoadPositionFollowsTheFarmsSevenCases, TestLoadwiseAssumedValueIsLatestWeightTimesTheSalesConfigPrice
// and TestLoadwiseAndFarmValuePriceOneSheepTheSameMultipleDimensions (each mutation-tested).

import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { tmpdir } from "node:os";

const REPO = resolve(new URL("../..", import.meta.url).pathname);

const PROCUREMENT_DIR = "backend/internal/procurement";
const INTERNAL_DIR = "backend/internal";
const FARMVALUATION_DIR = "backend/internal/farmvaluation";
const FARMVALUATION_SQL = "backend/internal/farmvaluation/sql.go";
const EDITOR_REPO = "backend/internal/sales/adapters/postgres/valuation_repository.go";
const LOADWISE_SQL = "backend/internal/procurement/adapters/postgres/loadwise_stock_weight.go";
const FARM_VALUE_SQL = "backend/internal/sales/adapters/postgres/overview_repository.go";
const SALES_DOMAIN = "backend/internal/sales/domain/valuation_assumptions.go";
const LOADWISE_DOMAIN = "backend/internal/procurement/domain/loadwise.go";
const LOADWISE_DOMAIN_TEST = "backend/internal/procurement/domain/loadwise_test.go";
const LOADWISE_SCREEN = "apps/admin-web/features/procurement/loadwise-section.tsx";

const RETIRED_GO = /\b(AvgSoldPrice|RemainingValue|PriceBasis|OverallAvgSoldPrice|UnsoldPriceBasis|loadwiseOverallAvgSQL|LoadwisePriceBasis\w*)\b|unsold_stock_price_rupees/;
const RETIRED_WIRE = /\b(avg_sold_price|remaining_value|price_basis|overall_avg_sold_price|unsold_price_basis)\b/;

function walk(root, dir, out = []) {
  const abs = join(root, dir);
  if (!existsSync(abs)) return out;
  for (const name of readdirSync(abs)) {
    const rel = join(dir, name);
    const st = statSync(join(root, rel));
    if (st.isDirectory()) walk(root, rel, out);
    else if (name.endsWith(".go") && !name.endsWith("_test.go")) out.push(rel);
  }
  return out;
}

const read = (root, rel) => (existsSync(join(root, rel)) ? readFileSync(join(root, rel), "utf8") : null);
// Comment lines carry history ("the retired AvgSoldPrice rule") and are not code.
const code = (src) =>
  src
    .split("\n")
    .filter((l) => !/^\s*(\/\/|--|\*)/.test(l))
    .join("\n");

export function check(root) {
  const f = [];
  const add = (rule, file, msg) => f.push(`${rule}: ${file}: ${msg}`);

  // 1. sold-price-values-stock
  for (const rel of walk(root, PROCUREMENT_DIR)) {
    const m = code(read(root, rel)).match(RETIRED_GO);
    if (m) add("sold-price-values-stock", rel, `names the retired sold-price basis "${m[0]}"`);
  }
  const screen = read(root, LOADWISE_SCREEN);
  if (screen !== null) {
    const m = code(screen).match(RETIRED_WIRE);
    if (m) add("sold-price-values-stock", LOADWISE_SCREEN, `reads the retired wire field "${m[0]}"`);
  }

  // 2. valuation-sql-copied
  for (const rel of walk(root, INTERNAL_DIR)) {
    if (rel.startsWith(FARMVALUATION_DIR + "/") || rel === EDITOR_REPO) continue;
    if (/sales_valuation_assumptions/.test(code(read(root, rel)))) {
      add("valuation-sql-copied", rel, "reads sales_valuation_assumptions itself; price animals through farmvaluation's shared fragments");
    }
  }

  // 3. shared-rule-not-spliced
  for (const rel of [LOADWISE_SQL, FARM_VALUE_SQL]) {
    const src = read(root, rel);
    if (src === null) {
      add("shared-rule-not-spliced", rel, "file is missing");
      continue;
    }
    for (const sym of ["farmvaluation.PricingCTEs", "farmvaluation.StageJoinsSQL(", "farmvaluation.BucketKeySQL("]) {
      if (!code(src).includes(sym)) add("shared-rule-not-spliced", rel, `does not splice ${sym}`);
    }
  }

  // 4. bucket-without-species
  const dom = read(root, SALES_DOMAIN);
  if (dom === null || !/func ValuationBucketKey\(stage, species, gender string\) string \{\s*return stage \+ "_" \+ species \+ "_" \+ gender/.test(dom)) {
    add("bucket-without-species", SALES_DOMAIN, "ValuationBucketKey must join stage, species and gender");
  }
  const fv = read(root, FARMVALUATION_SQL);
  if (fv === null || !/\|\| '_' \|\| %\[2\]s \|\| '_' \|\|/.test(fv) || !/sp := SpeciesSQL\(species\)/.test(fv)) {
    add("bucket-without-species", FARMVALUATION_SQL, "BucketKeySQL must splice the normalized species between stage and gender");
  }

  // 5. unvalued-stock-reads-as-loss
  const lw = read(root, LOADWISE_DOMAIN);
  if (lw === null || !/if row\.Remaining > 0 && row\.AssumedValue == nil && row\.ProfitLoss != nil \{\s*zero := 0\.0\s*row\.ProfitLoss = &zero/.test(lw)) {
    add("unvalued-stock-reads-as-loss", LOADWISE_DOMAIN, "a load holding animals that cannot be valued must read ₹0, never sold value minus the whole cost");
  }
  const lwt = read(root, LOADWISE_DOMAIN_TEST);
  if (lwt === null || !/func TestLoadPositionFollowsTheFarmsSevenCases\(/.test(lwt)) {
    add("unvalued-stock-reads-as-loss", LOADWISE_DOMAIN_TEST, "TestLoadPositionFollowsTheFarmsSevenCases is the runtime pin and must exist");
  }

  // 6. realised-split-on-screen
  if (screen !== null && /realised_profit_loss|loadwise\.realised\.label/.test(code(screen))) {
    add("realised-split-on-screen", LOADWISE_SCREEN, "profit is one value on Load wise; do not show the realised split");
  }
  return f;
}

// ---- self-test: a clean tree passes; each rule fires on its own adversarial fixture ----
function selfTest() {
  const good = {
    [FARMVALUATION_SQL]: "package farmvaluation\nconst x = `FROM public.sales_valuation_assumptions`\nfunc BucketKeySQL(stageNorm, species, sex string) string {\n\tsp := SpeciesSQL(species)\n\treturn `... || '_' || %[2]s || '_' || ...`\n}\n",
    [EDITOR_REPO]: "package postgres\nconst r = `SELECT buckets FROM public.sales_valuation_assumptions`\n",
    [LOADWISE_SQL]: "package postgres\nvar q = farmvaluation.PricingCTEs + farmvaluation.StageJoinsSQL(a, b) + farmvaluation.BucketKeySQL(a, b, c)\n",
    [FARM_VALUE_SQL]: "package postgres\n// was sales_valuation_assumptions inline\nvar q = farmvaluation.PricingCTEs + farmvaluation.StageJoinsSQL(a, b) + farmvaluation.BucketKeySQL(a, b, c)\n",
    [SALES_DOMAIN]: 'package domain\nfunc ValuationBucketKey(stage, species, gender string) string {\n\treturn stage + "_" + species + "_" + gender\n}\n',
    [LOADWISE_DOMAIN]: "package domain\n// the retired AvgSoldPrice rule priced stock off one sale\nfunc f() {\n\t\tif row.Remaining > 0 && row.AssumedValue == nil && row.ProfitLoss != nil {\n\t\t\tzero := 0.0\n\t\t\trow.ProfitLoss = &zero\n\t\t}\n}\n",
    [LOADWISE_DOMAIN_TEST]: "package domain\nfunc TestLoadPositionFollowsTheFarmsSevenCases(t *testing.T) {}\n",
    [LOADWISE_SCREEN]: "export const x = load.assumed_value;\n",
  };
  const bad = [
    ["sold-price-values-stock", { [LOADWISE_DOMAIN]: good[LOADWISE_DOMAIN] + "func g() { row.AvgSoldPrice = nil }\n" }],
    ["sold-price-values-stock", { "backend/internal/procurement/adapters/postgres/x.go": "package postgres\nconst q = `SELECT unsold_stock_price_rupees FROM t`\n" }],
    ["sold-price-values-stock", { [LOADWISE_SCREEN]: "export const x = load.remaining_value;\n" }],
    ["valuation-sql-copied", { "backend/internal/procurement/adapters/postgres/copy.go": "package postgres\nconst q = `SELECT b.price_per_kg FROM public.sales_valuation_assumptions va`\n" }],
    ["shared-rule-not-spliced", { [LOADWISE_SQL]: "package postgres\nvar q = farmvaluation.PricingCTEs\n" }],
    ["bucket-without-species", { [SALES_DOMAIN]: 'package domain\nfunc ValuationBucketKey(stage, gender string) string { return stage + "_" + gender }\n' }],
    ["bucket-without-species", { [FARMVALUATION_SQL]: "package farmvaluation\nfunc BucketKeySQL(stageNorm, species, sex string) string {\n\treturn `... || '_' || ...`\n}\n" }],
    ["unvalued-stock-reads-as-loss", { [LOADWISE_DOMAIN]: "package domain\nfunc f() { row.ProfitLoss = profitLoss(a, b, c) }\n" }],
    ["unvalued-stock-reads-as-loss", { [LOADWISE_DOMAIN_TEST]: "package domain\n" }],
    ["realised-split-on-screen", { [LOADWISE_SCREEN]: 'export const x = copy(pc, "loadwise.realised.label");\n' }],
  ];
  const build = (files) => {
    const dir = mkdtempSync(join(tmpdir(), "lw-valuation-guard-"));
    for (const [rel, body] of Object.entries(files)) {
      mkdirSync(dirname(join(dir, rel)), { recursive: true });
      writeFileSync(join(dir, rel), body);
    }
    return dir;
  };
  let failed = false;
  const clean = build(good);
  const cleanFindings = check(clean);
  rmSync(clean, { recursive: true, force: true });
  if (cleanFindings.length) {
    console.error("loadwise-stock-valuation-guard self-test: the clean fixture was flagged:\n  " + cleanFindings.join("\n  "));
    failed = true;
  }
  for (const [rule, override] of bad) {
    const dir = build({ ...good, ...override });
    const findings = check(dir);
    rmSync(dir, { recursive: true, force: true });
    if (!findings.some((x) => x.startsWith(rule + ":"))) {
      console.error(`loadwise-stock-valuation-guard self-test: ${rule} did not fire on its adversarial fixture`);
      failed = true;
    }
  }
  if (failed) process.exit(1);
  console.log(`loadwise-stock-valuation-guard self-test: PASS (${bad.length} adversarial fixtures)`);
}

if (process.argv.includes("--self-test")) {
  selfTest();
} else {
  const findings = check(REPO);
  if (findings.length) {
    console.error("loadwise-stock-valuation-guard: FAIL");
    for (const x of findings) console.error("  - " + x);
    console.error("\nSee docs/decisions/loadwise-stock-valuation.md");
    process.exit(1);
  }
  console.log("loadwise-stock-valuation-guard: PASS");
}
