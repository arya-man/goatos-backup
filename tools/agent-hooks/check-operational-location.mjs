#!/usr/bin/env node

// check-operational-location.mjs — enforces that product surfaces answer
// "where is this animal / where is this work?" with the OPERATIONAL LOCATION:
//
//   OperationalLocation = park + physical_shed + optional partition_label
//
// Physical sheds stay normalized (goats.shed_id is always the PARENT building,
// goat_shed_partitions.partition_label holds '1' / '2' / 'Part 3'), and that
// normalization is correct — see docs/decisions/scale-anti-patterns.md.
// The bug this guard prevents is the *other* half: read models, APIs and UI
// collapsing back to the parent shed, so a CEO sees "Castro = 202" and an
// operator sees "Yashoda" when the animals actually live in Castro 1/2/3 and
// Yashoda 1/2/3/10. That silently produces wrong destinations, wrong expected
// animal counts, and wrong proof labels on the ground.
//
// Checks:
//   whole-leak      user-facing label built from the 'whole' matching sentinel.
//                   'whole' is a comparison key only; a CEO must never read
//                   "Yashoda whole". Non-partitioned sheds render bare.
//   shifting-contract
//                   the shifting submit request must accept
//                   destination_partition_label, not destination_shed_id alone,
//                   or Castro 1 -> Castro 2 is unexpressible.
//   location-type-as-partition
//                   location_type is an ENUM (shed/partition kind). Weighing
//                   once assigned it where a partition LABEL belongs. It is
//                   never a partition label.
//   counts-grain    a counts/breakdown aggregation that GROUPs BY shed without
//                   any partition dimension collapses partitions into the
//                   parent and makes the dropdown lie.
//   alias-locations a selectable-location catalog must derive partitions from
//                   goat_shed_partitions, never from `locations` rows. Rows
//                   named "Castro 1"/"Godel 1 - Part 3" exist there with
//                   status='inactive' and 0 animals; surfacing them shows fake
//                   empty sheds and lets an operator move goats onto a dead id.
//
// Modes:
//   (default)     scan the tree, fail on any violation.
//   --self-test   run the built-in adversarial fixtures.
//
// Exception (must be COMPLETE — owner, issue, scope, expiry all present):
//   operational-location:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>

import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const IGNORE_RE =
  /operational-location:ignore:\s*owner=\S+\s+issue=\S+\s+scope=[^\n]*?\s+expiry=\d{4}-\d{2}-\d{2}/;

// The one legitimate home of the sentinel: the shared primitive that defines and
// collapses it, plus the SQL normalizer it mirrors.
const SENTINEL_OWNERS = [
  "backend/internal/platform/oploc/",
  "tools/agent-hooks/check-operational-location.mjs",
];

const CHECKS = [
  {
    id: "whole-leak",
    // A display/label expression concatenating the raw sentinel.
    // Only a CONCATENATION of the sentinel into a rendered string is a leak.
    // Comparisons (`= 'whole'`, `!== "whole"`) and COALESCE(..., 'whole')
    // defaults are the matching key doing its job — those are correct and must
    // not be flagged, or the guard punishes the very code that gets this right.
    // CLOSURE: detect fmt.Sprintf("%s %s", shed, "whole") where the second arg
    // is a string literal "whole" (bypass: was matching only binary + operators).
    test: (line) => {
      if (!/["'`]whole["'`]/.test(line)) return false;
      if (/COALESCE\s*\([^)]*["'`]whole["'`]/i.test(line)) return false;
      if (/(?:[!=<>]=|=)\s*["'`]whole["'`]|["'`]whole["'`]\s*(?:[!=<>]=|=)/.test(line)) return false;
      if (/NormalizePartition|normalizePartition|WholeSentinel/.test(line)) return false;
      // concatenated into a label/display expression via binary operator OR function argument
      return (
        /(?:\|\||\+)\s*["'`]whole["'`]|["'`]whole["'`]\s*(?:\|\||\+)/.test(line) ||
        /(?:label|display|title|caption|subtitle|chip)\s*[:=]\s*["'`]whole["'`]/i.test(line) ||
        /(?:fmt\.Sprintf|sprintf|printf|format)\s*\([^)]*["'`]whole["'`]/.test(line)
      );
    },
    msg: "user-facing label built from the 'whole' sentinel (via concat or function arg); non-partitioned sheds must render as the bare shed name",
  },
  {
    id: "location-type-as-partition",
    // PRECISION NOTE (rewritten after a false-positive incident): the defect is
    // location_type FLOWING INTO a partition label -- `partitionLabel:
    // shed.location_type`, or a partition variable assigned from it. Merely
    // mentioning both near each other is NOT the bug: `LEFT JOIN
    // goat_shed_partitions gsp` sits next to a `location_type` select in a dozen
    // perfectly correct queries, and the TABLE NAME itself contains "partition",
    // so a loose /partition/ test self-matches and flagged 13 correct call sites.
    // Require an actual assignment/binding between the two on ONE line.
    test: (line) => {
      if (!/location_?[Tt]ype/.test(line)) return false;
      // Exclude prose: OpenAPI descriptions and doc text that explain the rule.
      if (/^\s*(#|\/\/|--|\*)/.test(line)) return false;
      if (/NOT the same field|is an enum|carries no partition/i.test(line)) return false;
      // partition-label-ish identifier receiving location_type, either order.
      return (
        /partition_?[lL]abel\s*[:=][^=]{0,40}location_?[Tt]ype/.test(line) ||
        /location_?[Tt]ype\s*(?:as|AS)\s+partition_?_?label/i.test(line)
      );
    },
    msg: "location_type is an enum, not a partition label; use partition_label",
  },
  {
    id: "counts-grain",
    // Check for GROUP BY shed_id without partition across multiple lines in counts module.
    // CLOSURE: multi-line GROUP BY where partition mention might be on another line.
    // Use a bounded window (next ~3 lines) to catch cases where GROUP BY spans lines.
    test: (line, file, lines, lineIndex) => {
      if (!/\/counts\//.test(file)) return false;
      if (!/GROUP\s+BY/i.test(line)) return false;
      // Look for shed_id in the GROUP BY and surrounding ~3 lines context
      const window = lines.slice(lineIndex, Math.min(lineIndex + 3)).join(" ");
      const hasGroupByShedId = /GROUP\s+BY[^;]*shed_id/i.test(window);
      const hasPartition = /partition/i.test(window);
      return hasGroupByShedId && !hasPartition;
    },
    msg: "counts aggregation groups by shed without a partition dimension; partitions collapse into the parent shed",
  },
  {
    id: "alias-locations",
    // CLOSURE: status filter might be in a CTE, subquery, or earlier WHERE clause.
    // Scan a bounded window (~4 lines) to detect the filter across lines.
    test: (line, file, lines, lineIndex) => {
      if (!(/(destination|selectable|picker|catalog|dropdown)/i.test(line) &&
            (/FROM\s+locations|JOIN\s+locations/i.test(line)))) {
        return false;
      }
      // Look in a bounded window for the status filter that excludes inactive rows
      const window = lines.slice(Math.max(0, lineIndex - 2), Math.min(lines.length, lineIndex + 4)).join(" ");
      return !/status\s*(!=|<>)\s*'inactive'|status\s*=\s*'active'/i.test(window);
    },
    msg: "selectable-location query reads `locations` without excluding inactive partition-alias rows (0-animal fake sheds)",
  },
  {
    id: "empty-partition-catalog",
    // Detect partition catalogs built only from goat_shed_partitions (which hides empty partitions)
    // instead of the authoritative shed_partitions table (migration 000112).
    // PRECISION NOTE (this check was rewritten after a false-positive incident):
    // reading goat_shed_partitions to resolve A GOAT'S OWN partition is CORRECT and
    // is what most call sites legitimately do -- vaccination execution, obligation
    // reads, counts joins, even a seed's `DELETE FROM goat_shed_partitions`. The
    // defect is narrower: ENUMERATING THE SET of partitions a shed has (a catalog)
    // from a PER-GOAT table, which silently omits any partition holding zero
    // animals (CBE "Yashoda 5"). So the signal is a DISTINCT enumeration of
    // partition_label, not the mere presence of the table.
    //
    // The earlier version tested the context word /partition/ against the same
    // line -- but the table name `goat_shed_partitions` CONTAINS "partition", so
    // every join matched itself and the check flagged 21 correct call sites.
    test: (line, file, lines, lineIndex) => {
      const window = lines
        .slice(Math.max(0, lineIndex - 4), Math.min(lines.length, lineIndex + 5))
        .join(" ");
      // Must read the per-goat table...
      if (!/FROM\s+goat_shed_partitions|JOIN\s+goat_shed_partitions/i.test(window)) return false;
      // ...and be ENUMERATING distinct partition labels (a catalog), not resolving one goat's.
      if (!/DISTINCT[^;]{0,120}partition_label|DISTINCT\s+partition\b/i.test(window)) return false;
      // Already sourcing the authoritative catalog -> fine.
      if (/FROM\s+shed_partitions|JOIN\s+shed_partitions/i.test(window)) return false;
      return true;
    },
    msg: "partition catalog built from goat_shed_partitions only hides empty partitions; use shed_partitions (migration 000112) as the authoritative partition source",
  },
];

// Files that must CONTAIN a token (absence is the violation).
const REQUIRED = [
  {
    id: "shifting-contract",
    file: "contracts/openapi/app-api.yaml",
    token: "destination_partition_label",
    msg: "shifting submit contract lacks destination_partition_label; partition-to-partition moves (Castro 1 -> Castro 2) cannot be expressed",
  },
];

function scannable(file) {
  if (!/\.(go|ts|tsx|mjs|kt|sql|yaml)$/.test(file)) return false;
  if (/_test\.go$|\.test\.(mjs|ts|tsx)$|Test\.kt$/.test(file)) return false;
  if (/\/(node_modules|generated|migrations|\.git)\//.test(file)) return false;
  return true;
}

function isComment(line) {
  return /^\s*(\/\/|#|--|\*)/.test(line);
}

function scanContent(file, content) {
  const problems = [];
  const lines = content.split("\n");
  // This guard's own source is exempt from EVERY check: its self-test fixtures
  // deliberately contain each banned pattern (and each correct counter-example),
  // so scanning itself is guaranteed self-indictment.
  if (file.endsWith("tools/agent-hooks/check-operational-location.mjs")) return problems;
  const owned = SENTINEL_OWNERS.some((p) => file.includes(p));
  lines.forEach((line, i) => {
    if (isComment(line) || IGNORE_RE.test(line)) return;
    for (const check of CHECKS) {
      if (check.id === "whole-leak" && owned) continue;
      if (check.test(line, file, lines, i)) {
        problems.push(`${file}:${i + 1}: [${check.id}] ${check.msg}`);
      }
    }
  });
  return problems;
}

function selfTest() {
  const fixtures = [
    // [file, content, expected check id or null]
    ["a/label.ts", `const label = shed + " " + "whole";`, "whole-leak"],
    ["a/ok.ts", `if (partition !== "whole") return partition;`, null],
    // the matching key doing its job — must stay clean
    ["a/sqlcmp.go", `q := "WHERE effective.partition_label = 'whole' OR x"`, null],
    [
      "a/coalesce.go",
      `q := "regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part', '')"`,
      null,
    ],
    ["a/sqlconcat.go", `q := "SELECT shed.name || ' ' || 'whole' AS label"`, "whole-leak"],
    ["a/w.ts", `partitionLabel: campaignShed?.location_type ?? ""`, "location-type-as-partition"],
    [
      "backend/internal/counts/q.go",
      `q := "SELECT x FROM goats g GROUP BY g.shed_id, g.park_id"`,
      "counts-grain",
    ],
    [
      "backend/internal/counts/ok.go",
      `q := "SELECT x FROM goats g GROUP BY g.shed_id, gsp.partition_label"`,
      null,
    ],
    [
      "b/dest.go",
      `const destinationSQL = "SELECT id FROM locations WHERE tenant_id = $1"`,
      "alias-locations",
    ],
    [
      "b/dest_ok.go",
      `const destinationSQL = "SELECT id FROM locations WHERE status != 'inactive'"`,
      null,
    ],
    [
      "a/ignored.ts",
      `const label = "whole"; // operational-location:ignore: owner=ravi issue=X scope=fixture expiry=2027-01-01`,
      null,
    ],
    // the primitive itself is allowed to name the sentinel
    ["backend/internal/platform/oploc/oploc.go", `const label = "whole"`, null],
    // CLOSURE BYPASS 1: whole-leak via fmt.Sprintf argument
    ["a/sprintf.go", `label := fmt.Sprintf("%s %s", shed, "whole")`, "whole-leak"],
    // CLOSURE BYPASS 2: counts-grain multi-line GROUP BY
    [
      "backend/internal/counts/q2.go",
      `q := "SELECT x FROM goats g\nGROUP BY g.shed_id"`,
      "counts-grain",
    ],
    // CLOSURE: counts-grain ok when partition is on another line
    [
      "backend/internal/counts/ok2.go",
      `q := "SELECT x FROM goats g\nGROUP BY g.shed_id, gsp.partition_label"`,
      null,
    ],
    // CLOSURE BYPASS 3: alias-locations with status filter in WHERE (ok)
    [
      "b/dest_ok2.go",
      `const destinationSQL = "SELECT id FROM locations\nWHERE status != 'inactive'"`,
      null,
    ],
    // KNOWN BLIND SPOT (deliberately NOT closed): location_type laundered through
    // an intermediate variable across lines. Closing this needs dataflow, not
    // regex; every regex attempt flagged correct code instead (see the precision
    // note on the check). Documented in the header rather than faked.
    [
      "a/multiline.go",
      `x := campaignShed?.location_type\npartitionLabel := x`,
      null,
    ],
    // The direct form IS caught.
    [
      "a/direct.ts",
      `partitionLabel: campaignShed?.location_type ?? ""`,
      "location-type-as-partition",
    ],
    // FALSE-POSITIVE REGRESSIONS. Each of these is CORRECT code that an earlier
    // version of this guard wrongly failed. They must stay clean forever.
    ["a/join.go", `q := "LEFT JOIN goat_shed_partitions gsp"`, null],
    [
      "a/join2.go",
      `q := "SELECT l.location_type FROM x LEFT JOIN goat_shed_partitions gsp ON gsp.goat_id = g.goat_id"`,
      null,
    ],
    ["a/seed.go", `{"goat_shed_partitions", "DELETE FROM goat_shed_partitions WHERE tenant_id = $1"},`, null],
    [
      "a/prose.yaml",
      `            string "whole". NOT the same field as location_type, which is an`,
      null,
    ],
    // A park-grain aggregate with no shed_id in the GROUP BY is not a partition
    // collapse; counts-grain must not fire on it.
    [
      "backend/internal/counts/adapters/postgres/x.go",
      `  GROUP BY g.park_id, park.location_code, park.name,\n           park.status`,
      null,
    ],
    // CLOSURE BYPASS 5: empty-partition catalog from goat_shed_partitions only
    [
      "a/partition_catalog.go",
      `const catalogSQL = "SELECT DISTINCT partition FROM goat_shed_partitions"`,
      "empty-partition-catalog",
    ],
    // CLOSURE: partition catalog ok when using shed_partitions
    [
      "a/partition_catalog_ok.go",
      `const catalogSQL = "SELECT DISTINCT partition FROM shed_partitions"`,
      null,
    ],
  ];

  let failed = 0;
  for (const [file, content, expected] of fixtures) {
    const got = scanContent(file, content);
    const hit = got.length > 0;
    if (expected && !got.some((p) => p.includes(`[${expected}]`))) {
      console.error(`SELF-TEST FAIL: ${file} should trip ${expected}, got: ${got.join(", ") || "nothing"}`);
      failed++;
    }
    if (!expected && hit) {
      console.error(`SELF-TEST FAIL: ${file} should be clean, got: ${got.join(", ")}`);
      failed++;
    }
  }
  if (failed > 0) {
    console.error(`operational-location guard self-test: ${failed} failure(s)`);
    process.exit(1);
  }
  console.log("operational-location guard self-test: PASS");
}

function main() {
  if (process.argv.includes("--self-test")) return selfTest();

  const files = execSync("git ls-files", { cwd: repo, maxBuffer: 64 * 1024 * 1024 })
    .toString()
    .split("\n")
    .filter(Boolean)
    .filter(scannable);

  const problems = [];
  for (const file of files) {
    let content;
    try {
      content = readFileSync(resolve(repo, file), "utf8");
    } catch {
      continue;
    }
    problems.push(...scanContent(file, content));
  }

  for (const req of REQUIRED) {
    let content = "";
    try {
      content = readFileSync(resolve(repo, req.file), "utf8");
    } catch {
      problems.push(`${req.file}: [${req.id}] contract file missing`);
      continue;
    }
    if (!content.includes(req.token)) {
      problems.push(`${req.file}: [${req.id}] ${req.msg}`);
    }
  }

  if (problems.length > 0) {
    console.error("operational-location guard FAILED:\n" + problems.join("\n"));
    console.error(
      "\nOperationalLocation = park + physical_shed + optional partition_label." +
        "\nSee docs/architecture/operational-read-model-contract.md"
    );
    process.exit(1);
  }
  console.log(`operational-location guard: PASS (${files.length} files)`);
}

main();
