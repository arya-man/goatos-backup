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
  {
    id: "oploc-display-wire-name",
    // The operational-location label has exactly ONE wire name across Go, OpenAPI,
    // Kotlin and TypeScript: `operational_location_display`. Shipping the same
    // concept as a bare `display` splits the contract three ways and the break is
    // SILENT: kotlinx/serde give an absent key its default (""), so the client
    // renders an empty label rather than failing. Observed 2026-08-06 on
    // /app/counts/shifting/destinations -- backend emitted `display`, OpenAPI and
    // CountsDestinationShedDto declared `operational_location_display`, and the
    // only reason nothing looked broken was a SECOND defect (the screen re-derived
    // the label locally, see backend-owned-oploc-label) masking the first.
    //
    // Scoped by a partition sibling so this cannot fire on the many legitimate
    // `display` fields elsewhere: a field is an operational-location label only if
    // a partition_label / partitionLabel is declared within the same struct-ish
    // window.
    test: (line, file, lines, i) => {
      const declaresBareDisplay =
        /json:"display[,"]/.test(line) ||
        /@SerialName\(\s*["']display["']\s*\)/.test(line) ||
        /^\s{2,}display:\s*$/.test(line);
      if (!declaresBareDisplay) return false;
      // NO partition-sibling gate. The first draft required a partition_label within
      // +/-12 lines and that repeated, one check over, the exact blind spot this
      // guard's own ADR warns about: a surface that forgot partitions entirely names
      // them nowhere, so the gate only ever caught code that already half-remembered
      // the rule. Scope instead by NAME: `display` is a banned spelling for a
      // location label, so require a location-ish sibling (shed/park/location) rather
      // than a partition one. That still spares the many unrelated `display` fields.
      const window = lines.slice(Math.max(0, i - 12), i + 13).join("\n");
      if (!/shed|park|location|partition/i.test(window)) return false;
      // Already carrying the canonical name somewhere in the same block -> the
      // bare `display` is an extra alias, still wrong, but do not double-report
      // when the canonical field is the one on this very line.
      return !/operational_location_display/.test(line);
    },
    msg: "operational-location label must ship as `operational_location_display` on every surface (Go json tag, OpenAPI property, Kotlin @SerialName, TS); a bare `display` silently deserializes to \"\" on clients that expect the canonical name",
  },
  {
    id: "backend-owned-oploc-label",
    // AGENTS.md golden rule: the backend owns visible labels; clients render them.
    // oploc.Display() already composes "Yashoda" / "Castro 2" / "Godel 1 - Part 3"
    // and ships it, so a client that rebuilds the string from name + partition is
    // duplicating a business rule into a second (and third) language where it can
    // drift. Observed 2026-08-06: AddBirthScreen rendered a bare shed name for
    // partition-bearing options, so one shed appeared 10 identical times.
    //
    // The violation is a SHED-identified dropdown option or selected label built
    // from a bare `.name`. Park options legitimately have no partition, so the id
    // expression must be shed-ish for this to fire.
    test: (line, file, lines, i) => {
      if (!/\.(kt|ts|tsx)$/.test(file)) return false;
      // `[^,()]` (not `[^,)]`) is load-bearing: allowing `(` let the CORRECT form
      // `DropdownOption(it.shedId, operationalLocationLabel(it.name, it.partitionLabel))`
      // match on its inner `it.name,` and flagged compliant code. Caught by the
      // a/AddBirthOk.kt fixture -- keep that fixture.
      const bareShedOption =
        /DropdownOption\s*\(\s*[^,()]*[Ss]hed[A-Za-z]*\s*,\s*[^,()]*\.name\s*[,)]/.test(line);
      const bareShedSelectedLabel =
        /selected(?:Shed)?Label\s*=\s*[A-Za-z_]*[Ss]hed[A-Za-z_]*\??\.name\b/.test(line);
      // NO "is a partition mentioned nearby?" gate here, deliberately. The first
      // draft of this check required one and was therefore blind to the exact
      // defect it was written for: AddBirthScreen.kt names `partitionLabel` ZERO
      // times -- omitting partitions entirely IS the bug, so scoping on a nearby
      // partition mention only ever catches code that already half-remembered the
      // rule. A shed-identified option is in scope on its own; park options stay
      // out because the id expression must be shed-ish to match.
      return bareShedOption || bareShedSelectedLabel;
    },
    msg: "shed option/label rendered from a bare `.name`; render the backend's operational_location_display or every partition of a shed shows the same text",
  },
  {
    id: "shed-only-selection-key",
    // The LABEL and the KEY are separate defects and the key is the dangerous one.
    // Destination feeds return one row PER PARTITION, so shed_id is not unique --
    // on live STG, 18 shed ids cover 130 rows and one shed appears 10 times.
    // `firstOrNull { it.shedId == state.shedId }` therefore resolves EVERY partition
    // of a shed to its first row: the screen can show 10 correct labels and still
    // select partition 1 for all of them. Fixing only the visible label leaves this
    // silently wrong, which is why it gets its own check rather than riding along.
    // Correct form: ShiftingScreen.kt's
    //   it.shedId == destinationShedId && it.partitionLabel == destinationPartitionLabel
    test: (line, file) => {
      if (!/\.(kt|ts|tsx)$/.test(file)) return false;
      if (!/\b(firstOrNull|find)\s*[({]/.test(line)) return false;
      if (!/\.shedId\s*==|shed_id\s*===/.test(line)) return false;
      // A composite key that also compares the partition is the correct form.
      return !/partition/i.test(line);
    },
    msg: "shed option looked up by shedId alone; destination rows are one PER PARTITION so shedId is not unique - match on (shedId, partitionLabel) or every partition resolves to the first row",
  },
  {
    id: "bare-shed-movement-label",
    // The shifting approve/execute consumer side of the same defect: a movement's
    // source/destination label built from a bare shed name, so a Castro 1 -> Castro 2
    // move renders "Castro -> Castro". Distinct from backend-owned-oploc-label
    // because it is neither a DropdownOption nor a `selectedLabel =`.
    test: (line, file) => {
      if (!/\.(kt|ts|tsx)$/.test(file)) return false;
      // The shed variable itself must carry the source/destination prefix. Making that prefix
      // OPTIONAL produced a false positive on herd-actions.ts's CSV shed-IMPORT preview
      // (`sourceLabel = [shedCode, shedName].join(" · ")`), which builds a label for an imported
      // shed row -- no movement, no partition, nothing to fix. A guard that flags correct code
      // gets baselined and then ignored, so the prefix is required.
      return /\b(source|destination)Label\s*=\s*[^=]*\b(source|destination)[Ss]hedName\b/.test(line);
    },
    msg: "movement source/destination label built from a bare shed name; a Castro 1 -> Castro 2 move renders as \"Castro -> Castro\". Render the backend's operational_location_display for each end",
  },
  {
    id: "location-write-without-partition",
    // A *Request schema that targets a shed but cannot name a partition. This is
    // SCHEMA-SCOPED on purpose: the first attempt was a file-level "does
    // admin-api.yaml contain partition_label anywhere" REQUIRED check, which passed
    // vacuously because one unrelated READ schema mentions it -- a textbook
    // guard-false-green. Bound the resource: find the enclosing `    Xxx:` schema
    // header, and only search THAT block.
    test: (line, file, lines, i) => {
      if (!/contracts\/openapi\/.*\.yaml$/.test(file)) return false;
      if (!/^ {8}shed_id:\s*$/.test(line)) return false;
      let header = null;
      let start = 0;
      for (let j = i; j >= 0; j--) {
        const m = /^ {4}([A-Za-z][A-Za-z0-9]*):\s*$/.exec(lines[j]);
        if (m) {
          header = m[1];
          start = j;
          break;
        }
      }
      if (!header || !/Request$/.test(header)) return false;
      let end = lines.length;
      for (let j = start + 1; j < lines.length; j++) {
        if (/^ {4}[A-Za-z][A-Za-z0-9]*:\s*$/.test(lines[j])) {
          end = j;
          break;
        }
      }
      const block = lines.slice(start, end).join("\n");
      // Narrowed to writes that PLACE AN ANIMAL. A shed-scoped write is not
      // automatically a partition defect: feed config and feed session completions
      // (FeedDirection/FeedDistribution/FeedPacking/FeedConfig*) legitimately target
      // a whole shed and have no per-pen concept. Before this filter the check was
      // 6/9 false positives, which is how a guard earns a baseline entry and then
      // gets ignored. Animal-placement signal = the schema names goats.
      const placesAnAnimal =
        /Goat|Birth|Shifting|Move|Animal/i.test(header) ||
        /\bgoat_ids?\b|\bdam_id\b|\blitter_size\b/i.test(block);
      if (!placesAnAnimal) return false;
      return !/partition/i.test(block);
    },
    msg: "a *Request schema targets shed_id but declares no partition; with additionalProperties:false a move/create into Castro 2 is unexpressible (RecordShiftingEventRequest is the correct template)",
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
    // --- oploc-display-wire-name -------------------------------------------
    // the real 2026-08-06 defect: bare `display` beside a partition_label sibling
    [
      "b/app_destinations_handler.go",
      `type shed struct {\n\tPartitionLabel *string \`json:"partition_label,omitempty"\`\n\tDisplay string \`json:"display"\`\n}`,
      "oploc-display-wire-name",
    ],
    // canonical name in the same struct -> clean
    [
      "b/ok_handler.go",
      `type shed struct {\n\tPartitionLabel *string \`json:"partition_label,omitempty"\`\n\tOperationalLocationDisplay string \`json:"operational_location_display"\`\n}`,
      null,
    ],
    // ADVERSARIAL: a `display` field with NO partition anywhere near it is some
    // other concept entirely and must not be flagged.
    ["b/unrelated.go", `type card struct {\n\tDisplay string \`json:"display"\`\n}`, null],
    // Kotlin client side of the same drift
    [
      "a/CountsDto.kt",
      `data class D(\n  @SerialName("partition_label") val partitionLabel: String? = null,\n  @SerialName("display") val display: String = "",\n)`,
      "oploc-display-wire-name",
    ],
    // --- backend-owned-oploc-label -----------------------------------------
    // the real AddBirthScreen defect: shed option labelled by bare name
    [
      "a/AddBirthScreen.kt",
      `val partitionLabel = shed.partitionLabel\noptions = sheds.map { CountsDropdownOption(it.shedId, it.name) }`,
      "backend-owned-oploc-label",
    ],
    // CORRECT = render the BACKEND's shipped label. The previous version of this
    // fixture used operationalLocationLabel(it.name, it.partitionLabel) and called
    // that clean, which certified ADR defect #2 (client-side re-derivation) as the
    // right answer -- the guard was encoding a weaker rule than the doc it enforces.
    [
      "a/AddBirthOk.kt",
      `options = sheds.map { CountsDropdownOption(it.shedId + "/" + it.partitionLabel, it.operationalLocationDisplay) }`,
      null,
    ],
    // ADVERSARIAL: PARK options have no partition concept -> must stay clean even
    // when a partition label appears elsewhere in the same file.
    [
      "a/ParkPicker.kt",
      `val partitionLabel = shed.partitionLabel\noptions = parks.map { CountsDropdownOption(it.parkId, it.name) }`,
      null,
    ],
    // REGRESSION FIXTURE: a shed option in a file that never mentions a partition
    // is the WORST case, not a clean one. This fixture previously expected null and
    // encoded the guard's own blind spot -- AddBirthScreen.kt has zero partition
    // mentions, which is precisely why its dropdown showed one shed ten times.
    ["a/NoParts.kt", `options = sheds.map { CountsDropdownOption(it.shedId, it.name) }`, "backend-owned-oploc-label"],
    // selected-label form of the same defect
    ["a/Selected.kt", `selectedLabel = selectedShed?.name`, "backend-owned-oploc-label"],
    // --- shed-only-selection-key -------------------------------------------
    [
      "a/BirthKey.kt",
      `val selectedShed = sheds.firstOrNull { it.shedId == state.shedId }`,
      "shed-only-selection-key",
    ],
    // correct: the composite key ShiftingScreen already uses
    [
      "a/ShiftKeyOk.kt",
      `val sel = sheds.firstOrNull { it.shedId == destinationShedId && it.partitionLabel == destinationPartitionLabel }`,
      null,
    ],
    // ADVERSARIAL: a firstOrNull on something that is not a shed id stays clean.
    ["a/OtherKey.kt", `val p = parks.firstOrNull { it.parkId == state.parkId }`, null],
    // --- bare-shed-movement-label ------------------------------------------
    [
      "a/Pending.kt",
      `destinationLabel = destinationShedName.takeIf { it.isNotBlank() } ?: UNKNOWN`,
      "bare-shed-movement-label",
    ],
    // correct: renders the backend-composed label for the movement end
    [
      "a/PendingOk.kt",
      `destinationLabel = destinationOperationalLocationDisplay.takeIf { it.isNotBlank() } ?: UNKNOWN`,
      null,
    ],
    // ADVERSARIAL: a CSV shed-IMPORT preview row label is not a movement. Real case
    // (herd-actions.ts) that this check wrongly flagged until the source/destination prefix on the
    // shed variable was made mandatory.
    [
      "a/herd-actions.ts",
      `const sourceLabel = [shedCode, shedName].filter(Boolean).join(" · ") || undefined;`,
      null,
    ],
    // --- location-write-without-partition ----------------------------------
    [
      "contracts/openapi/admin-api.yaml",
      `    MoveGoatRequest:\n      additionalProperties: false\n      properties:\n        park_id:\n          type: string\n        shed_id:\n          type: string\n    NextSchema:\n      type: object`,
      "location-write-without-partition",
    ],
    // correct: the shifting template, which names a partition
    [
      "contracts/openapi/app-api.yaml",
      `    RecordShiftingEventRequest:\n      properties:\n        shed_id:\n          type: string\n        destination_partition_label:\n          type: string\n    NextSchema:\n      type: object`,
      null,
    ],
    // ADVERSARIAL: a READ schema (not *Request) with shed_id is not a write path.
    [
      "contracts/openapi/app-api.yaml",
      `    ShedSummary:\n      properties:\n        shed_id:\n          type: string\n    NextSchema:\n      type: object`,
      null,
    ],
    // ADVERSARIAL (the vacuous-pass case): a sibling schema mentioning partition
    // must NOT excuse the write schema that lacks one.
    [
      "contracts/openapi/admin-api.yaml",
      `    SomeRead:\n      properties:\n        partition_label:\n          type: string\n    MoveGoatRequest:\n      properties:\n        shed_id:\n          type: string\n    NextSchema:\n      type: object`,
      "location-write-without-partition",
    ],
    // ADVERSARIAL: a shed-scoped write that does NOT place an animal (feed config,
    // feed session completion) has no per-pen concept and must stay clean.
    [
      "contracts/openapi/app-api.yaml",
      `    UpsertFeedConfigShedFactorRequest:\n      properties:\n        shed_id:\n          type: string\n        factor:\n          type: number\n    NextSchema:\n      type: object`,
      null,
    ],
    // ...but a birth, which places an animal, is caught by its dam_id/litter signal
    // even though its name is "RecordBirthEventRequest".
    [
      "contracts/openapi/app-api.yaml",
      `    RecordBirthEventRequest:\n      properties:\n        shed_id:\n          type: string\n        dam_id:\n          type: string\n    NextSchema:\n      type: object`,
      "location-write-without-partition",
    ],
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
