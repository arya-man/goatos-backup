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
// Checks (ENFORCED):
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
//   missing-partition-column
//                   a user-facing/read-model table with shed_id MUST carry
//                   partition_label when partitions exist. Allowlist legitimate
//                   shed-grain-only tables in SHED_GRAIN_ONLY_TABLES with WHY.
//   sql-display-drift
//                   CASE statements that compose display strings must use or
//                   reference oploc.Display() / PartitionLabel.render() /
//                   operational_location_display(). Hand-rolled CASE duplicates
//                   risk display-logic divergence: hand-rolled might produce
//                   "Castro - 2" (wrong, dashed form for numeric partition) while
//                   canonical produces "Castro 2" (correct, space form; farm's
//                   physical naming).
//   shed-name-keying
//                   GROUP BY / map-key / list-key expressions must use shed_id,
//                   never shed NAME. Names repeat across parks (two Castro, two
//                   Gandhi, two Yashoda); name-keyed grouping merges parks.
//   go-display-drift
//                   Go string concatenation of shed names with partition labels
//                   outside oploc.Display(). Six SQL CASE statements were converted
//                   to Go composition; without this check, N+6 call sites can drift.
//                   Example defect: `display := shedName + " " + partitionLabel`
//                   (produces "Godel 1 1" instead of "Godel 1").
//                   Approved forms: oploc.OperationalLocation{}.Display() or
//                   a matching approved composition helper in backend/internal/platform/oploc/*.
//   ts-display-drift
//                   TypeScript string concatenation of shed names with partition
//                   labels outside lib/operational-location.ts. Same defect shape
//                   and approved form.
//   kt-display-drift
//                   Kotlin string concatenation of shed names with partition labels
//                   outside PartitionLabel.kt. Same defect shape and approved form.
//   duplicate-options
//                   RUNTIME-ONLY: dropdowns rendering duplicate/ambiguous text
//                   when the same partition label appears in multiple parks or
//                   rows from different parks have identical names. Caught only
//                   by integration tests; documented here.
//   weighing-alias-resolution-predicate
//                   Legacy partition aliases in `locations` are inactive shed rows.
//                   Weighing runtime and forward-repair SQL must never match an
//                   active whole shed or another location type as an alias.
//   weighing-create-idempotency-before-hydration
//                   CreateCampaign idempotency must compare the original client
//                   request before mutable alias/catalog hydration. Exact retries
//                   must replay the original result even after catalog repair.
// REMAINING BLIND SPOTS (documented, cannot be caught):
//   - composition split across helper functions (requires dataflow analysis).
//   - composition via template strings with complex expressions.
// See docs/decisions/operational-location-display-composition.md (ADR)
// for the canonical rule — all composition must use the shared primitives.
//
// BLIND SPOTS CLOSED (2026-08-07):
// 1. shed_name/shedName-only identification: schemas that identify a shed ONLY
//    by name (no shed_id/shedId) are now flagged. A name alone is not sufficient
//    when partitions exist; partition context is still required.
// 2. $ref to shed types: schemas that embed shed context via $ref
//    (e.g., shed: { $ref: '#/components/schemas/ShedRef' }) are now checked.
//    Partition/display fields are resolved one level into the referenced schema.
//
// RESIDUAL BLIND SPOTS (cannot be caught by static scan):
// - Transitive $ref chains (shed -> ShedRef -> ShedCore): recursive resolution is not
//   practical with a regex scan; a static check resolves only one hop. If a shed property
//   $refs a type that itself $refs another shed type, the transitive partition check
//   is skipped. Mitigated by: canonical schemas inline their complete property set at
//   the point of use (preferred) or keep $ref one level only (acceptable).
// - Composition in Go/TypeScript outside the shared helper: a hand-built concat is
//   caught by go-display-drift / ts-display-drift / kt-display-drift only if those
//   checks execute; the OpenAPI schema is mute.
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
    // CLOSURE 2026-08-07: React `key=` expressions use 'whole' as a matching key
    // (a disambiguator), not user copy — exclude them from the leak check.
    test: (line) => {
      if (!/["'`]whole["'`]/.test(line)) return false;
      if (/COALESCE\s*\([^)]*["'`]whole["'`]/i.test(line)) return false;
      if (/(?:[!=<>]=|=)\s*["'`]whole["'`]|["'`]whole["'`]\s*(?:[!=<>]=|=)/.test(line)) return false;
      if (/NormalizePartition|normalizePartition|WholeSentinel/.test(line)) return false;
      // React key= expressions are matching keys, not rendered labels — exclude them
      // Pattern: key={...} or key="..." where 'whole' appears inside
      if (/key\s*=\s*[\{"].*["'`]whole["'`]/.test(line)) return false;
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
      // A DISTINCT inside an AGGREGATE is an AGREEMENT check over the goats already in
      // scope ("do all scoped animals sit in the same partition?"), not an enumeration of
      // the partitions a shed HAS. The per-goat table is the correct source for that
      // question and the catalog is irrelevant to it, so this is not the defect.
      // Enumeration looks like `SELECT DISTINCT partition_label` or
      // `array_agg(DISTINCT partition_label)`; agreement looks like
      // `count(DISTINCT partition_label) ... = 1`. Added after the shed-completion and
      // calendar drive-shed summaries tripped this rule for asking the agreement question
      // (2026-08-07).
      const aggregateAgreement =
        /\bcount\s*\(\s*DISTINCT[^)]{0,120}partition_label/i.test(window) &&
        !/SELECT\s+DISTINCT[^;]{0,120}partition_label|array_agg\s*\(\s*DISTINCT[^)]{0,120}partition_label/i.test(window);
      if (aggregateAgreement) return false;
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
  {
    id: "response-shed-missing-partition",
    // THE ABSENCE CHECK. Every other rule in this file keys on partition being
    // MENTIONED somewhere -- a bad separator, a 'whole' leak, a name-keyed GROUP BY.
    // A surface that forgot partitions ENTIRELY names them nowhere, so no other rule
    // can fire on it. That is exactly how /app/vaccination/execution shipped: the
    // OpenAPI schema declared partition_label + operational_location_display, the Go
    // struct carried neither, and a partitioned shed rendered as a bare "Mandela 2"
    // on the operator's phone (2026-08-07).
    //
    // The sibling rule `location-write-without-partition` gates on /Request$/, so it
    // only ever guarded WRITE paths. Reads -- where the display actually happens --
    // were unguarded. This rule is that missing half: a RESPONSE schema declaring a
    // shed identity MUST declare both contract fields, or be allowlisted with a WHY.
    //
    // CLOSURE 2026-08-07 (blind spot 1): treat shed_name/shedName as a shed identity indicator,
    // not just shed_id/shedId. A schema that identifies a shed ONLY by name (e.g., display rows
    // from legacy queries) still needs partition context.
    //
    // CLOSURE 2026-08-07 (blind spot 2): resolve $ref to shed-related schemas (e.g., shed: { $ref: '#/components/schemas/ShedRef' })
    // If a schema property $refs to a type whose name suggests shed/location identity, resolve
    // that referenced schema and check if IT declares partition/display. This catches schemas
    // that embed shed context indirectly. Residual gap: full transitive $ref chains (shed -> ShedRef -> ShedCore)
    // require recursive resolution; a static scan resolves one hop. Documented below.
    test: (line, file, lines, i) => {
      if (!/contracts\/openapi\/.*\.yaml$/.test(file)) return false;
      if (!/^\s{4}[A-Za-z][A-Za-z0-9]*:\s*$/.test(line)) return false;
      const name = line.trim().replace(/:$/, "");
      if (/Request$/.test(name)) return false; // write paths: sibling rule owns them
      if (RESPONSE_PARTITION_EXEMPT.has(name)) return false;
      // Read the schema block: up to the next sibling schema at the same indent.
      const block = [];
      for (let j = i + 1; j < lines.length; j++) {
        if (/^\s{4}[A-Za-z][A-Za-z0-9]*:\s*$/.test(lines[j])) break;
        block.push(lines[j]);
      }
      const body = block.join("\n");

      // BLIND SPOT 1: Check for shed identity via shed_id, shedId, shed_name, or shedName
      const bearsShed = /^\s+(shed_id|shedId|shed_name|shedName):/m.test(body);
      if (!bearsShed) return false;

      let hasPartition = /^\s+(partition_label|partitionLabel):/m.test(body);
      let hasDisplay = /^\s+(operational_location_display|operationalLocationDisplay):/m.test(body);

      // BLIND SPOT 2: Check for $ref to shed-related types and resolve one level
      if (!hasPartition || !hasDisplay) {
        // Look for shed/location $refs (one hop only; full transitive chains not resolved)
        const refMatches = body.match(/^\s+(shed|location|shedRef|locationRef):\s*\n\s+\$ref:\s*["']#\/components\/schemas\/([A-Za-z][A-Za-z0-9]*)["']/m);
        if (refMatches) {
          const refSchemaName = refMatches[2];
          // Only follow refs to shed/location-like schema names (heuristic gate)
          if (/shed|location|ref/i.test(refSchemaName)) {
            // Find the referenced schema and check if it carries partition/display
            const refSchemaPattern = new RegExp(`^    ${refSchemaName}:\\s*$`, 'm');
            const refSchemaIdx = lines.findIndex((l, idx) => idx > i && refSchemaPattern.test(l));
            if (refSchemaIdx >= 0) {
              const refBlock = [];
              for (let j = refSchemaIdx + 1; j < lines.length; j++) {
                if (/^\s{4}[A-Za-z][A-Za-z0-9]*:\s*$/.test(lines[j])) break;
                refBlock.push(lines[j]);
              }
              const refBody = refBlock.join("\n");
              hasPartition = hasPartition || /^\s+(partition_label|partitionLabel):/m.test(refBody);
              hasDisplay = hasDisplay || /^\s+(operational_location_display|operationalLocationDisplay):/m.test(refBody);
            }
          }
        }
      }

      return !(hasPartition && hasDisplay);
    },
    msg: "response schema declares a shed identity (shed_id, shedId, shed_name, shedName, or $ref to shed type) but not partition_label + operational_location_display; a partitioned shed will render bare (add both, or add the schema to RESPONSE_PARTITION_EXEMPT with a stated WHY)",
  },
  {
    id: "shed-name-keying",
    // Detect GROUP BY or map-key expressions using shed NAME instead of shed_id.
    // Names repeat across parks (two Castro, two Gandhi, two Yashoda); name-keyed
    // grouping silently merges rows from different parks.
    // Precision: look for patterns like `GROUP BY shed_name` or `key: shed.name`
    // or `parkId.*physicalShedName` (TypeScript, in template literals) that DON'T use shed_id.
    // CLOSURE 2026-08-07: Canonical helper calls (operationalLocationLabel, Display, PartitionLabel.render,
    // oploc.OperationalLocation) combine park + shed name correctly — exclude them.
    test: (line) => {
      if (/^\s*(#|\/\/|--|\*)/.test(line)) return false; // comments
      // Canonical helper calls are allowed; they handle the park+shed composition correctly
      if (/operationalLocationLabel\s*\(|Display\s*\(|PartitionLabel\.render\s*\(|oploc\.OperationalLocation/.test(line)) return false;

      // Defect 1: GROUP BY shed_name (not shed_id)
      if (/GROUP\s+BY[^;]*\bshed_?[nN]ame\b/i.test(line)) {
        if (!/shed_?[iI]d/.test(line)) return true; // only flag if no shed_id in same line
      }

      // Defect 2: map-key or groupKey combining park and shed NAME (not id).
      // The park+shedName pair ALONE is not a defect: it also appears in ordinary
      // argument lists (`Scan(&parkOptionName, ..., &shedNames)`) and in the regex
      // source of sibling guards, neither of which keys anything. Require a real
      // keying construct on the same line -- a composite key/groupKey assignment, a
      // map index, or a `+`/template concatenation of the two -- so the rule fires on
      // the construction that actually merges rows, not on co-occurrence.
      const pairsParkAndShedName =
        /parkId.*physicalShedName|park[a-zA-Z_]*.*[sS]hed[nN]ame/.test(line);
      const keysSomething =
        /\b(group_?[kK]ey|map_?[kK]ey|cache_?[kK]ey|[kK]ey)\s*(:|=|:=)/.test(line) ||
        /\[[^\]]*[sS]hed[nN]ame[^\]]*\]\s*=/.test(line) ||
        /[sS]hed[nN]ame\s*(\+|\}\$\{|`)/.test(line) ||
        /\+\s*["'`][^"'`]*["'`]\s*\+\s*[a-zA-Z_.]*[sS]hed[nN]ame/.test(line) ||
        // Template-literal composite key: `${item.parkId}|${item.physicalShedName}`.
        // This is the original admin-web execution-board defect and has no `key =`
        // on the line -- the composite IS the value returned to groupBy.
        (/`[^`]*\$\{[^}]*[sS]hed[nN]ame[^}]*\}/.test(line) &&
          /`[^`]*\$\{[^}]*[pP]ark[^}]*\}/.test(line));
      if (pairsParkAndShedName && keysSomething) {
        if (!/shedId|shed_id/.test(line)) return true; // only flag if not using shedId instead
      }

      return false;
    },
    msg: "shed name-keyed grouping or map key; shed names repeat across parks (two Castro, two Gandhi); use shed_id + park instead",
  },
  {
    id: "go-display-drift",
    // Detect Go string concatenation of shed names with partition labels outside
    // the approved helper (oploc.OperationalLocation{}.Display() or a matching
    // wrapper). Six SQL CASE statements were converted to Go composition; each
    // new hand-rolled composition is a drift risk.
    // PRECISION: match variable names actually used in this repo:
    // (shedName|ShedName|shedLabel|ShedLabel) combined with
    // (partitionLabel|PartitionLabel|partition|Partition).
    test: (line, file) => {
      // Only check backend Go code
      if (!/backend\/.*\.go$/.test(file)) return false;
      if (/_test\.go$|^\s*\/\//.test(file) || /^\s*\/\//.test(line)) return false; // test files + comments
      // Must mention a shed-name-like AND partition-label-like variable/field
      const shedNameMatch = /\b(?:shedName|ShedName|shedLabel|ShedLabel)\b/.test(line);
      const partitionMatch = /\b(?:partitionLabel|PartitionLabel|partition|Partition)\b/.test(line);
      if (!shedNameMatch || !partitionMatch) return false;
      // Must be a string concatenation: + or fmt.Sprintf
      if (!/\+\s*["']|fmt\.Sprintf/.test(line)) return false;
      // OK if it's calling the approved Display method or using oploc package
      if (/\.Display\(\)|oploc\.OperationalLocation/.test(line)) return false;
      // OK if the composition is DELEGATED to a wrapper that itself calls oploc.
      // Without this the rule fires on correct code: a line that appends an
      // already-composed location to a subject label ("Death evidence · " + date,
      // shedName, partitionLabel) mentions both variable names and a "+", but does
      // no composing of its own. Two such false positives were live when this was
      // added, and a guard that flags correct code is a guard someone turns off.
      // Named wrappers only -- an arbitrary helper still trips the rule.
      if (/\b(?:appendLocation|composeOperationalLocation|operationalLocationLabel)\s*\(/.test(line)) return false;
      // OK if shed/partition appear only as STRUCT FIELD assignments (`ShedName: x,`
      // `PartitionLabel: y,`) -- that is passing the parts along, not composing them.
      // The "+" that triggered the concatenation test on such lines belongs to an
      // unrelated expression sharing the line (an idempotency key, a log message).
      // This was a live false positive on a one-line struct literal.
      const shedIsField = /\b(?:ShedName|ShedLabel)\s*:/.test(line);
      const partIsField = /\b(?:PartitionLabel|Partition)\s*:/.test(line);
      const shedInExpr = /\b(?:shedName|shedLabel)\b/.test(line);
      if (shedIsField && partIsField && !shedInExpr) return false;
      return true;
    },
    msg: "Go string concatenation of shed name with partition label; use oploc.OperationalLocation{}.Display() instead to prevent drift from the shared primitive",
  },
  {
    id: "ts-display-drift",
    // Detect TypeScript string concatenation of shed names with partition labels
    // outside lib/operational-location.ts. Same defect shape as go-display-drift.
    test: (line, file) => {
      // Only check admin-web TypeScript outside the approved location module
      if (!/apps\/admin-web\/.*\.(ts|tsx)$/.test(file)) return false;
      if (file.includes("lib/operational-location.ts")) return false; // the approved home
      if (/^\s*(\/\/|\/\*)/.test(line)) return false; // comments
      // Must mention shed-name-like AND partition-label-like identifiers
      const shedNameMatch = /\b(?:shedName|ShedName|shedLabel|ShedLabel|physicalShedName)\b/.test(line);
      const partitionMatch = /\b(?:partitionLabel|PartitionLabel|partition|Partition)\b/.test(line);
      if (!shedNameMatch || !partitionMatch) return false;
      // Must be string concatenation: + operator
      if (!(/\+\s+["']|const\s+\w+\s*=\s*\w+\s*\+/.test(line))) return false;
      // OK if calling the approved helper
      if (/\.format\(|OperationalLocation\.|operational.*location/i.test(line)) return false;
      return true;
    },
    msg: "TypeScript string concatenation of shed name with partition label; use lib/operational-location.ts helpers instead",
  },
  {
    id: "kt-display-drift",
    // Detect Kotlin string concatenation of shed names with partition labels
    // outside PartitionLabel.kt. Same defect shape as go-display-drift.
    test: (line, file) => {
      // Only check Android Kotlin outside the approved location module
      if (!/apps\/goatos-android\/.*\.kt$/.test(file)) return false;
      if (file.includes("PartitionLabel.kt")) return false; // the approved home
      if (/^\s*(\/\/)/.test(line)) return false; // comments
      // Must mention shed-name-like AND partition-label-like identifiers
      const shedNameMatch = /\b(?:shedName|ShedName|shedLabel|ShedLabel|physicalShedName)\b/.test(line);
      const partitionMatch = /\b(?:partitionLabel|PartitionLabel|partition|Partition)\b/.test(line);
      if (!shedNameMatch || !partitionMatch) return false;
      // Must be string concatenation: + operator
      if (!(/\+\s*["']|val\s+\w+\s*=\s*\w+\s*\+/.test(line))) return false;
      // OK if calling the approved helper
      if (/\.render\(|PartitionLabel\./i.test(line)) return false;
      return true;
    },
    msg: "Kotlin string concatenation of shed name with partition label; use PartitionLabel.render() instead",
  },
  {
    id: "normalized-label-to-display",
    // CRITICAL BUG CLOSURE 2026-08-07: `partition_label` has TWO stored forms:
    // - `partition_label` (human form: "Part 3", "3")
    // - `normalized_label` (scrubbed matching key: "3")
    // A query selected `normalized_label` and it reached the screen, so an
    // operator saw `Mandela 2 - 3` — ambiguous, since "Mandela 2" is the shed
    // name and "3" is the partition normalized key. This guard flags flows where
    // `normalized_label` reaches a display/label/name field or a display composer.
    // ALLOWED uses: `normalized_label` in JOINs, WHERE, GROUP BY, or as a map key.
    // PRECISION NOTE (2026-08-07 reclassification):
    // - Match SHED PARTITION normalized_label only: on table/field access (gsp.normalized_label, sp.normalized_label, etc.)
    // - Exclude comparison operators in WHERE/JOIN contexts (= , <>, !=, etc. on same line)
    // - Exclude assignment to fields designed to store normalized values (NormalizedSourceLabel, NormalizedLabel, etc.)
    // - Exclude local variable normalization in other domains (vaccine label normalization, etc.)
    test: (line, file) => {
      if (/^\s*(#|\/\/|--|\*)/.test(line)) return false; // comments
      // Must mention normalized_label or normalized*Label variant (case-insensitive)
      if (!/normalized.{0,5}label/i.test(line)) return false;

      // ALLOWED: legitimate uses in JOIN, WHERE, GROUP BY, or map keys on same line
      if (/JOIN\s+|WHERE\s+|GROUP\s+BY|map.?key|cache.?key|\.key\s*[:=]/.test(line)) return false;

      // ALLOWED: comparison operators (=, <>, !=, LIKE, etc.) in matching predicates
      // normalized_label can appear on either side of the operator (with optional table prefix)
      if (/(?:=|<>|!=|LIKE|NOT\s+LIKE|IN|NOT\s+IN|~)\s*(?:\w+\.)?\s*normalized_?label|normalized_?label\s*(?:=|<>|!=|LIKE|NOT\s+LIKE|IN|NOT\s+IN|~)/.test(line)) return false;

      // ALLOWED: assignment to fields designed to hold normalized values (NormalizedSourceLabel, NormalizedLabel, etc.)
      if (/(?:Normalized[A-Z]\w*|normalized_[a-z_]*)\s*=/.test(line)) return false;

      // ALLOWED: vaccine/dose label normalization context (not shed partitions)
      // These files/functions are for vaccine label processing, not partition display
      if (/vaccinelabel|dosecode|antigen|vaccine.{0,20}label/i.test(file)) return false;
      if (/(?:vaccine|dose|antigen).{0,20}Label|displayLabel|humanize/i.test(line)) return false;

      // SQL: selecting into a display/label alias
      if (/AS\s+(?:display|label|name|location)/i.test(line) && /select/i.test(line)) return true;
      // SQL: passing to a display composer function
      if (/(?:Display|operationalLocationDisplay|display.?(?:partition|location))\s*\(/i.test(line)) return true;
      // Go/Kotlin/TS: assigning to a display or label variable
      if (/(?:display|label)\s*[:=]/i.test(line)) return true;
      // OperationalLocation{PartitionLabel: ...}
      if (/PartitionLabel\s*[:=]/i.test(line)) return true;
      // TS/Kotlin: rendering in template literal or concatenation
      if (/`.*\$\{|[+]\s*["']/.test(line)) return true;

      return false;
    },
    msg: "normalized_label (matching key) flows into a display field or display composer; use partition_label (human form) instead. normalized_label is for SQL JOINs, WHERE, GROUP BY, and map keys only.",
  },
  {
    id: "sql-display-drift",
    // Detect CASE statements composing display strings that duplicate partition-label
    // rendering logic instead of calling the shared primitive (oploc.Display() in Go,
    // PartitionLabel.render() in Kotlin, operational_location_display() in SQL).
    // This catches hand-rolled CASE that renders "Castro - Part 2" while the primitive
    // renders "Castro 2", causing display-label mismatches across surfaces.
    test: (line, file, lines, lineIndex) => {
      if (/^\s*(#|\/\/|--|\*)/.test(line)) return false; // comments
      // Bare concatenation, no CASE. The first draft only fired INSIDE a CASE, so the
      // simplest possible drift -- `shed.name || ' - ' || gsp.partition_label` -- walked
      // straight through. That form is the likeliest one a future author reaches for, so
      // it is checked before the CASE path rather than left as the rule's blind spot.
      const bareConcat =
        /\|\||CONCAT\s*\(/i.test(line) &&
        /partition_label|normalized_label/i.test(line) &&
        /\b(?:shed|location)?[._]?name\b/i.test(line) &&
        !/Display\s*\(|render\s*\(|operational_location_display/i.test(line);
      if (bareConcat) return true;
      // MULTI-LINE concatenation. The single-line check above needs the name AND the partition
      // on one line. Real drift does not oblige: a Feed Transport list query composed
      //     ELSE s.name || ' - ' || COALESCE((
      //       SELECT min(sp.partition_label) ...
      // where the concat operator and the partition sit on DIFFERENT lines, so the rule walked
      // past it and the query shipped. If a line concatenates a *_name column and a partition
      // appears in the surrounding window, that is the same defect wearing a line break.
      const concatsAName =
        /\|\||CONCAT\s*\(/i.test(line) &&
        /\b(?:shed|location)?[._]?name\b/i.test(line) &&
        !/Display\s*\(|render\s*\(|operational_location_display/i.test(line);
      // A concat inside a MATCHING predicate is not a display: `loc.name LIKE shed.name || ' %'`
      // joins rows, it never reaches a screen. Excluded explicitly, because the first version of
      // this widened rule flagged exactly that and a guard that cries wolf gets switched off.
      const isMatchPredicate = /\bLIKE\b|\bSIMILAR\s+TO\b|~\*?\s|\bON\b\s|\bWHERE\b|\bAND\b\s+\w+\.\w+\s*=/i.test(line);
      if (concatsAName && !isMatchPredicate) {
        const near = lines
          .slice(Math.max(0, lineIndex - 2), Math.min(lines.length, lineIndex + 10))
          .join(" ");
        if (/partition_label|normalized_label/i.test(near) &&
            !/Display\s*\(|render\s*\(|operational_location_display/i.test(near)) {
          return true;
        }
      }
      if (!/CASE\s+WHEN|WHEN\s+|THEN\s+/.test(line)) return false;
      // Scan a bounded window to find CASE...WHEN...partition...THEN pattern
      const window = lines
        .slice(Math.max(0, lineIndex - 1), Math.min(lines.length, lineIndex + 6))
        .join(" ");
      // Must have both CASE/WHEN and partition_label mention
      if (!/CASE[^;]*WHEN[^;]*partition_label/i.test(window)) return false;
      // Must show string concatenation (||, +, CONCAT) in the THEN clause
      if (!/(THEN[^;]{0,150}(?:\|\||[\+]|CONCAT))/i.test(window)) return false;
      // OK if it uses the shared primitive instead of hand-rolling
      if (/Display\s*\(|render\s*\(|operational_location_display/i.test(window)) return false;
      return true;
    },
    msg: "CASE statement composes a display string from partition_label instead of calling the shared primitive (backend/internal/platform/oploc.Display, PartitionLabel.render, operational_location_display)",
  },
  {
    id: "missing-partition-column",
    // Detect user-facing/read-model tables that carry shed_id but lack partition_label.
    // Tables bearing location-related data (verification, weighing, counts) must carry
    // both when partitions exist, or the surface loses partition context.
    // This check scans CREATE TABLE / migration statements.
    test: (line, file, lines, lineIndex) => {
      // Only check migration files
      if (!/migrations.*\.sql$/.test(file)) return false;
      if (!/CREATE\s+TABLE/i.test(line)) return false;
      if (/^\s*--/.test(line)) return false; // allow comment lines
      // Scan a bounded window (next ~20 lines) for the table definition
      const window = lines.slice(lineIndex, Math.min(lineIndex + 25)).join("\n");
      // Check if it defines shed_id...
      if (!/shed_id/.test(window)) return false;
      // ...but lacks partition_label
      if (/partition_label/.test(window)) return false;
      // Additional signal: it's a user-facing table name (verify, weigh, count, batch, etc.)
      const tableName = window.match(/CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)/i);
      const tblName = (tableName ? tableName[1] : "").toLowerCase();
      return /verif|weigh|count|batch|assignment|items|campaign/.test(tblName);
    },
    msg: "table carries shed_id but lacks partition_label; user-facing/read-model tables must carry partition context when partitions exist",
  },
];

// Tables that legitimately read/group by shed_id alone (no partition_label required).
// Each entry must include an explicit WHY, not a dumping ground.
// Response schemas that legitimately carry a shed WITHOUT a partition. Each entry
// states WHY, because "it was failing" is not a reason.
const RESPONSE_PARTITION_EXEMPT = new Set([
  // ShedCardSummary (2026-08-16) is a keyed AGGREGATE sidecar, not a rendered location row: it
  // rides in a map keyed by card id alongside the execution rows, and DOES carry partition_label —
  // but as card IDENTITY for keying/grain, not for display. The card header's location text is
  // owned by the execution ROW contract, which carries the full partition + composed
  // operational_location_display; the summary is never rendered as a location, so composing a
  // second display string here would duplicate the row's authority (two writers to one label).
  "ShedCardSummary",
  // CommandBoardShedVaccineCell arrived from main on 2026-08-07, after this rule existed. It is
  // a shed x vaccine matrix cell and its Go struct (vaccinationexecution/domain.
  // CommandBoardShedVaccineCell) carries no partition at that grain, so wiring one means changing
  // a feature this branch does not own.
  //
  // STATED WHY, and it is a DEFERRAL not a denial: two partitions of one shed DO collapse into a
  // single cell here, so a matrix cell reading "behind" cannot tell a park head WHICH pen is
  // behind. That is a real gap, tracked in the operational-location ledger as OL-18 rather than
  // hidden by this exemption. Remove this entry when the cell grain gains a partition.
  "CommandBoardShedVaccineCell",
  // Feed targets the whole shed -- there is no per-pen feeding concept, so a partition
  // on these rows would be a fiction (confirmed against the feed contract, 2026-08-07).
  "FeedDirectionRow",
  "FeedDirectionFilterShed",
  "FeedPackingRow",
  "FeedTransportTask",
  "FeedConfigShedFactor",
  "FeedConfigExperiment",
  "FeedDirectionGenerationPreviewRow",
  "FeedDirectionGenerationPreviewTotal",
  "FeedDirectionCountsProjectionException",
  // Weighing derives partition from locations catalog via name-parsing (oploc.SplitShedPartitionName),
  // not from goat_shed_partitions (which it must never read, per isolation rule). This allows weighing
  // to label videos with their physical partition without reading per-goat animal data (maintainer
  // decision 2026-08-07: weighing isolation + partition awareness are compatible). Removed exemption.
  // "WeighingShedVideos",
  // Telemetry/event payloads are not rendered as a location label.
  "VerificationReviewEventPayload",
]);

const SHED_GRAIN_ONLY_TABLES = [
  // Structural / configuration tables; never surface to users as location.
  "locations", // shed directory; has location_type ENUM, not partition granularity
  "shed_partitions", // the PARTITION catalog itself, keyed by shed_id
  "goat_shed_partitions", // per-goat partition mapping; read to find one goat's partition
  "locations_resource_attributes", // shed-level attributes; no partition aspect
  "location_resource_tags", // shed-level tags; no partition aspect
  // Operational plumbing (audit, identity, not user-facing read models)
  "audit_log", // identity audit only; sheds appear as context, not as operational location
  "proof_artifacts", // content store; identity reference only
  "outbox", // transactional outbox; events carry full location in payload
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

const REQUIRED_PATTERNS = [
  {
    id: "weighing-alias-resolution-predicate",
    file: "backend/internal/weighing/adapters/postgres/repository.go",
    all: [/alias_options AS \(/, /AND alias\.location_type='shed'/, /AND alias\.status='inactive'/],
    msg: "weighing runtime partition alias resolver must only treat inactive shed rows as legacy aliases; active/staging/review whole sheds must never be eligible",
  },
  {
    id: "weighing-alias-resolution-predicate",
    file: "backend/migrations/postgres/000145_weighing_partition_forward_safety.sql",
    all: [/JOIN public\.locations alias/, /AND alias\.location_type = 'shed'/, /AND alias\.status = 'inactive'/],
    msg: "weighing forward repair must use the same inactive-shed legacy-alias predicate as runtime; active/staging/review numeric-suffix sheds must remain whole sheds",
  },
  {
    id: "weighing-create-idempotency-before-hydration",
    file: "backend/internal/weighing/adapters/postgres/repository.go",
    ordered: [
      /requestFingerprint := idempotencyFingerprint\(cmd\)/,
      /campaignByIdempotencyMatchOnly\(ctx, tx, cmd\.TenantID, "weighing\.campaign_created", cmd\.IdempotencyKey, requestFingerprint\)/,
      /hydrateCreateCampaignShedPartitions\(ctx, tx, cmd\.TenantID, cmd\.Sheds\)/,
      /canonicalFingerprint := idempotencyFingerprint\(cmd\)/,
      /recordIdempotency\(ctx, tx, cmd\.TenantID, "weighing\.campaign_created", cmd\.IdempotencyKey, requestFingerprint,/,
    ],
    msg: "weighing CreateCampaign must fingerprint and replay-check the original request before partition hydration, then store that raw request fingerprint",
  },
  {
    id: "herd-register-partition-catalog-picker",
    file: "apps/admin-web/features/counts/herd-register.tsx",
    all: [/getHerdRegisterLocations\(\)/],
    none: [/getCountsBreakdown\(/, /data\.facets\.sheds/],
    msg: "herd register write destinations must come from the partition catalog, not census/count facets; empty partitions with zero animals must remain selectable",
  },
  {
    id: "herd-register-partition-catalog-picker",
    file: "apps/admin-web/lib/api/herd-locations.ts",
    all: [/listAllFeedConfigPens\(/, /partition_label/, /operational_location_display/],
    msg: "herd register location helper must include partition catalog rows so empty partitions are valid registration/shifting destinations",
  },
  {
    id: "stg-clouddeploy-task-errexit",
    file: "tools/deploy/stg-clouddeploy-task.sh",
    all: [/trap write_failed_on_exit EXIT/, /\nmain "\$@"\s*$/],
    none: [/if ! main "\$@"/],
    msg: "staging Cloud Deploy task wrapper must let Bash errexit observe migration failures; do not wrap main in `if ! main`",
  },
];

function scannable(file) {
  if (!/\.(go|ts|tsx|mjs|kt|sql|yaml)$/.test(file)) return false;
  if (/_test\.go$|\.test\.(mjs|ts|tsx)$|Test\.kt$/.test(file)) return false;
  if (/\/(node_modules|generated|migrations|\.git)\//.test(file)) return false;
  // Seed programs under backend/cmd/seed-* are NOT a display surface: they read a source
  // spreadsheet and write canonical rows, and their in-memory maps are already park-scoped by
  // construction (parkCode + shedName), so the cross-park name collision this rule guards against
  // cannot occur there. They are also hard-listed contract sources for
  // check-vaccination-hrms-seed-fixture.mjs, which requires six companion fixture/runbook updates
  // for ANY edit -- so annotating them line-by-line would force unrelated seed-contract churn, and
  // an id lookup per source row would add an N+1 in the seed loop. Excluded deliberately; if a seed
  // program ever renders a user-facing location label, that belongs behind oploc like any other.
  if (/^backend\/cmd\/seed-/.test(file)) return false;
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
      // missing-partition-column check: skip allowlisted tables
      if (check.id === "missing-partition-column") {
        const inAllowlist = SHED_GRAIN_ONLY_TABLES.some((tbl) => file.includes(`/${tbl}`) || line.toLowerCase().includes(`table ${tbl}`) || line.toLowerCase().includes(`entity ${tbl}`));
        if (inAllowlist) continue;
      }
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
    // A READ schema with shed_id is not a WRITE path, so location-write-without-partition
    // must not fire on it -- but response-shed-missing-partition MUST. This fixture used to
    // assert "clean"; that premise died with the read-side blind spot it documented
    // (2026-08-07). Reads are where the display happens, so they are now guarded too.
    [
      "contracts/openapi/app-api.yaml",
      `    ShedSummary:\n      properties:\n        shed_id:\n          type: string\n    NextSchema:\n      type: object`,
      "response-shed-missing-partition",
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
    // NEW CHECK FIXTURES: response-shed-missing-partition (the ABSENCE check, 2026-08-07).
    // A response schema carrying a shed identity MUST declare both contract fields.
    [
      "contracts/openapi/app-api.yaml",
      `    DriveShedRow:\n      properties:\n        shed_id:\n          type: string\n        shed_name:\n          type: string\n    NextSchema:\n      type: object`,
      "response-shed-missing-partition",
    ],
    // Correct form: both fields present -> clean.
    [
      "contracts/openapi/app-api.yaml",
      `    DriveShedRow:\n      properties:\n        shed_id:\n          type: string\n        partition_label:\n          type: string\n        operational_location_display:\n          type: string\n    NextSchema:\n      type: object`,
      null,
    ],
    // Allowlisted whole-shed schema -> clean even without the fields.
    [
      "contracts/openapi/app-api.yaml",
      `    FeedPackingRow:\n      properties:\n        shed_id:\n          type: string\n        shed_name:\n          type: string\n    NextSchema:\n      type: object`,
      null,
    ],
    // BLIND SPOT 1 CLOSURE: shed_name-only identification (no shed_id) must still flag missing partition.
    // Real case: several legacy read models identified sheds by name, not by id.
    [
      "contracts/openapi/app-api.yaml",
      `    LegacyShedReport:\n      properties:\n        shed_name:\n          type: string\n        total_count:\n          type: integer\n    NextSchema:\n      type: object`,
      "response-shed-missing-partition",
    ],
    // BLIND SPOT 1 FIX: shed_name-only schema with partition fields -> clean.
    [
      "contracts/openapi/app-api.yaml",
      `    LegacyShedReport:\n      properties:\n        shed_name:\n          type: string\n        partition_label:\n          type: string\n        operational_location_display:\n          type: string\n    NextSchema:\n      type: object`,
      null,
    ],
    // BLIND SPOT 2 CLOSURE: $ref to shed-related type without partition in the parent.
    // The parent schema doesn't directly declare shed identity, but the $ref'd type does (e.g., shed: { $ref: '#/components/schemas/ShedRef' }).
    [
      "contracts/openapi/app-api.yaml",
      `    ShedRef:\n      properties:\n        shed_id:\n          type: string\n    NextSchema:\n      type: object\n    OperationRow:\n      properties:\n        shed:\n          $ref: '#/components/schemas/ShedRef'\n        operation_id:\n          type: string\n    FinalSchema:\n      type: object`,
      "response-shed-missing-partition",
    ],
    // BLIND SPOT 2 FIX: $ref'd schema with partition fields in the referenced type -> clean.
    [
      "contracts/openapi/app-api.yaml",
      `    ShedRef:\n      properties:\n        shed_id:\n          type: string\n        partition_label:\n          type: string\n        operational_location_display:\n          type: string\n    NextSchema:\n      type: object\n    OperationRow:\n      properties:\n        shed:\n          $ref: '#/components/schemas/ShedRef'\n        operation_id:\n          type: string\n    FinalSchema:\n      type: object`,
      null,
    ],
    // NEW CHECK FIXTURES: shed-name-keying (Defect #3 from 2026-08-06 partition sweep)
    // Real defect: admin-web execution-board.tsx grouped by parkId|physicalShedName
    [
      "apps/admin-web/pages/execution-board.tsx",
      `  const grouped = groupBy(items, (item) => \`\${item.parkId}|\${item.physicalShedName}\`);`,
      "shed-name-keying",
    ],
    // Correct form: keying by shed_id
    [
      "apps/admin-web/pages/execution-board-fixed.tsx",
      `  const grouped = groupBy(items, (item) => \`\${item.parkId}|\${item.shedId}\`);`,
      null,
    ],
    // Defect: GROUP BY shed_name without shed_id
    [
      "backend/internal/counts/q_bad.go",
      `q := "SELECT COUNT(*) FROM goats g GROUP BY g.shed_name"`,
      "shed-name-keying",
    ],
    // OK: GROUP BY shed_id and partition_label
    [
      "backend/internal/counts/q_ok.go",
      `q := "SELECT COUNT(*) FROM goats g GROUP BY g.shed_id, gsp.partition_label, g.park_id"`,
      null,
    ],

    // NEW CHECK FIXTURES: sql-display-drift (Defect #2 from 2026-08-06 partition sweep)
    // Real defect: hand-rolled CASE might render "Castro - Part 2" (if partition_label
    // is bare numeric "2", dashed form is wrong) while canonical renders "Castro 2"
    // (space form for numeric; farm's physical naming) or "Castro - Part 3" (if
    // partition_label is "Part 3", dash form is correct for worded partitions)
    [
      "backend/internal/obligation/queries.sql",
      `  CASE WHEN gsp.partition_label IS NOT NULL
         THEN shed.name || ' - Part ' || gsp.partition_label
         ELSE shed.name END AS label`,
      "sql-display-drift",
    ],
    // Correct form: using the shared primitive
    [
      "backend/internal/obligation/queries-fixed.sql",
      `  oploc.Display(shed.name, gsp.partition_label) AS label`,
      null,
    ],
    // Also ok: referencing the primitive via alias
    [
      "backend/internal/vaccination/queries.go",
      `q := "SELECT operational_location_display(s.name, gsp.partition_label) AS loc"`,
      null,
    ],

    // NEW CHECK FIXTURES: missing-partition-column (Defect #1 from 2026-08-06 partition sweep)
    // Real defect: verification_items table has shed_id but no partition_label
    [
      "backend/migrations/postgres/000180_verification_items.sql",
      `CREATE TABLE verification_items (
  id UUID PRIMARY KEY,
  shed_id UUID NOT NULL REFERENCES sheds(id),
  source_module TEXT NOT NULL
);`,
      "missing-partition-column",
    ],
    // Correct form: includes partition_label
    [
      "backend/migrations/postgres/000180_verification_items_fixed.sql",
      `CREATE TABLE verification_items (
  id UUID PRIMARY KEY,
  shed_id UUID NOT NULL REFERENCES sheds(id),
  partition_label TEXT,
  source_module TEXT NOT NULL
);`,
      null,
    ],
    // Legitimate exception: locations table is shed-only (already in allowlist)
    [
      "backend/migrations/postgres/000040_locations.sql",
      `CREATE TABLE locations (
  id UUID PRIMARY KEY,
  shed_id UUID NOT NULL,
  location_type TEXT NOT NULL
);`,
      null,
    ],

    // NEW CHECK FIXTURES: go-display-drift (Defect: hand-rolled Go concatenation)
    // Real defect: six call sites concatenate shedName + partitionLabel after SQL->Go conversion
    [
      "backend/internal/counts/domain.go",
      `  display := shedName + " " + partitionLabel`,
      "go-display-drift",
    ],
    [
      "backend/internal/counts/domain_ok.go",
      `  display := oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display()`,
      null,
    ],
    [
      "backend/internal/obligation/helpers.go",
      `  location := fmt.Sprintf("%s %s", shedLabel, partitionLabel)`,
      "go-display-drift",
    ],

    // NEW CHECK FIXTURES: ts-display-drift (TypeScript concatenation outside lib/operational-location.ts)
    [
      "apps/admin-web/features/counts/shed-detail.tsx",
      `  const label = shedName + " - " + partitionLabel;`,
      "ts-display-drift",
    ],
    [
      "apps/admin-web/features/counts/shed-detail-ok.tsx",
      `  const label = OperationalLocation.format(shedName, partitionLabel);`,
      null,
    ],
    [
      "apps/admin-web/lib/operational-location.ts",
      `  export const format = (shedName, partitionLabel) => shedName + partitionLabel;`,
      null, // approved location for composition
    ],

    // NEW CHECK FIXTURES: kt-display-drift (Kotlin concatenation outside PartitionLabel.kt)
    [
      "apps/goatos-android/feature/feature-counts/ShedScreen.kt",
      `  val label = shedName + " " + partitionLabel`,
      "kt-display-drift",
    ],
    [
      "apps/goatos-android/feature/feature-counts/ShedScreen-ok.kt",
      `  val label = PartitionLabel.render(shedName, partitionLabel)`,
      null,
    ],
    [
      "apps/goatos-android/core/core-ui/PartitionLabel.kt",
      `  fun render(shed: String, partition: String) = shed + " " + partition`,
      null, // approved location for composition
    ],

    // CLOSURE FIXTURES FOR JOB 1: whole-leak fix (React key expressions)
    // Real case: calendar-event-drawer.tsx:499 uses 'whole' as a React KEY disambiguator
    [
      "a/calendar-event-drawer.tsx",
      `  key={\`\${label}-\${partitionLabel || 'whole'}\`}`,
      null, // React keys are matching keys, not rendered labels
    ],

    // CLOSURE FIXTURES FOR JOB 1: shed-name-keying fix (canonical helper calls)
    // Real case: workflows-landing.tsx:246 and :251 call operationalLocationLabel() correctly
    [
      "apps/admin-web/features/workflows/workflows-landing.tsx",
      `  row.operational_location_display || operationalLocationLabel({ shedName: row.shed_name, partitionLabel: row.partition_label })`,
      null, // canonical helper handles park+shed composition correctly
    ],
    [
      "apps/admin-web/features/count/detail.tsx",
      `  const label = Display(shedName, partitionLabel)`,
      null, // Display helper is approved
    ],

    // JOB 2 FIXTURES: normalized-label-to-display (the new rule)
    // VIOLATION: SQL selecting normalized_label into a display alias
    [
      "backend/internal/counts/queries.sql",
      `SELECT g.normalized_label AS display_label FROM goat_shed_partitions g`,
      "normalized-label-to-display",
    ],
    // VIOLATION: SQL passing normalized_label to a display composer
    [
      "backend/internal/vaccination/queries.sql",
      `SELECT operational_location_display(s.name, gsp.normalized_label) AS label`,
      "normalized-label-to-display",
    ],
    // VIOLATION: Go assigning normalized_label into a Display field
    [
      "backend/internal/counts/domain.go",
      `  display := NormalizedLabel`,
      "normalized-label-to-display",
    ],
    // VIOLATION: OperationalLocation{PartitionLabel: normalized_label}
    [
      "backend/internal/counts/queries.go",
      `  loc := oploc.OperationalLocation{ShedName: shedName, PartitionLabel: normalizedLabel}`,
      "normalized-label-to-display",
    ],
    // VIOLATION: TypeScript rendering normalized_label in a label expression
    [
      "apps/admin-web/features/counts/detail.tsx",
      `  const label = \`\${shedName} - \${normalizedLabel}\`;`,
      "normalized-label-to-display",
    ],
    // VIOLATION: Kotlin val label = shedName + normalizedLabel
    [
      "apps/goatos-android/feature/feature-counts/Detail.kt",
      `  val label = shedName + " " + normalizedLabel`,
      "normalized-label-to-display",
    ],

    // CORRECT: normalized_label in a WHERE clause (legitimate use)
    [
      "backend/internal/counts/queries.sql",
      `SELECT g.id FROM goat_shed_partitions g WHERE g.normalized_label = 'Part 3'`,
      null, // WHERE clause is a legitimate use
    ],
    // CORRECT: normalized_label in a JOIN predicate (legitimate use)
    [
      "backend/internal/counts/queries.sql",
      `JOIN shed_partitions sp ON sp.normalized_label = g.normalized_label`,
      null, // JOIN is a legitimate use
    ],
    // CORRECT: normalized_label in GROUP BY (legitimate use)
    [
      "backend/internal/counts/queries.sql",
      `SELECT g.shed_id, g.normalized_label FROM goat_shed_partitions g GROUP BY g.shed_id, g.normalized_label`,
      null, // GROUP BY is a legitimate use
    ],
    // CORRECT: normalized_label as a map key (legitimate use)
    [
      "apps/admin-web/features/counts/detail.tsx",
      `  const partitionsByKey = new Map<string, Partition>();\n  partitions.forEach(p => partitionsByKey.set(p.normalizedLabel, p));`,
      null, // map key is a legitimate use
    ],
    // CORRECT: using partition_label instead of normalized_label for display
    [
      "backend/internal/counts/queries.sql",
      `SELECT operational_location_display(s.name, gsp.partition_label) AS label`,
      null, // partition_label (human form) is correct for display
    ],

    // RECLASSIFICATION FIXTURES (2026-08-07: false-positive exemptions per refined rule)
    // These fixtures establish that the refined rule correctly exempts legitimate uses:

    // EXEMPT: local variable normalization in vaccine label domain (no shed partition context)
    [
      "backend/internal/vaccination/domain/vaccinelabels.go",
      `  if label := matrixDoseDisplayLabel(normalized); label != "" {`,
      null, // local variable "normalized" without partition context is vaccine label code
    ],

    // EXEMPT: sp.normalized_label in comparison operator within WHERE/JOIN context
    [
      "backend/internal/counts/adapters/postgres/shifting_execution.go",
      `AND sp.normalized_label = regexp_replace(lower(btrim($3::text)), '^part[[:space:]]+', '')`,
      null, // comparison operator (=) is legitimate matching, even with shed partition field
    ],

    // EXEMPT: sp.normalized_label in JOIN ON predicate with comparison operator
    [
      "backend/internal/weighing/adapters/postgres/shed_partition_resolve.go",
      ` AND sp.normalized_label = lower(btrim(regexp_replace(`,
      null, // JOIN ON predicate with comparison operator is legitimate matching
    ],

    // EXEMPT: assignment to field designed to hold normalized values (NormalizedSourceLabel)
    [
      "backend/internal/locations/adapters/postgres/repository.go",
      `item.NormalizedSourceLabel = textPtr(normalizedLabel)`,
      null, // NormalizedSourceLabel field is designed to store normalized values, not display
    ],

    // EXEMPT: partitions.normalized_label in comparison within CASE/WHERE
    [
      "backend/internal/counts/adapters/postgres/shifting_destinations.go",
      `regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') = partitions.normalized_label`,
      null, // comparison operator (=) in WHERE filtering is legitimate matching
    ],

    // REAL DEFECT: gsp.normalized_label selected into display alias (still flagged)
    [
      "backend/internal/counts/queries.sql",
      `SELECT gsp.normalized_label AS display_shed_label FROM goat_shed_partitions gsp`,
      "normalized-label-to-display", // selecting into AS display_* alias is the defect
    ],

    // REAL DEFECT: gsp.normalized_label passed to Display composer (still flagged)
    [
      "backend/internal/vaccination/queries.sql",
      `SELECT Display(shed.name, gsp.normalized_label) AS label`,
      "normalized-label-to-display", // passing to Display() composer is the defect
    ],

    // REAL DEFECT: assignment to display variable (still flagged)
    [
      "backend/internal/counts/domain.go",
      `display := shedName + " - " + normalizedLabel`,
      "normalized-label-to-display", // assigning to display variable is the defect (has partition context from var name pattern)
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

  let problems = [];
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

  for (const req of REQUIRED_PATTERNS) {
    let content = "";
    try {
      content = readFileSync(resolve(repo, req.file), "utf8");
    } catch {
      problems.push(`${req.file}: [${req.id}] file missing`);
      continue;
    }
    if (req.all && !req.all.every((pattern) => pattern.test(content))) {
      problems.push(`${req.file}: [${req.id}] ${req.msg}`);
    }
    if (req.none && req.none.some((pattern) => pattern.test(content))) {
      problems.push(`${req.file}: [${req.id}] ${req.msg}`);
    }
    if (req.ordered) {
      let cursor = 0;
      let ok = true;
      for (const pattern of req.ordered) {
        const rest = content.slice(cursor);
        const match = pattern.exec(rest);
        if (!match) {
          ok = false;
          break;
        }
        cursor += match.index + match[0].length;
      }
      if (!ok) {
        problems.push(`${req.file}: [${req.id}] ${req.msg}`);
      }
    }
  }

  // Shrink-only ratchet for sql-display-drift / go-display-drift.
  //
  // These two rules were ADDED on the partition branch (2026-08-07) and immediately surfaced
  // 31 long-standing composition sites across obligation/counts/processintegrity/sqlc. Nothing
  // regressed -- the detector simply did not exist before. Failing the whole guard on
  // pre-existing debt would have exactly one outcome: someone disables the rule, and the class
  // goes dark again.
  //
  // So: every site KNOWN at the moment the rule landed is listed below and reported as debt,
  // while any NEW site fails the build. Entries are keyed by file + rule, never by line number,
  // so unrelated edits above them do not invalidate the baseline.
  //
  // Adding an entry here to make a NEW violation stop failing is not an accepted way to land
  // code -- fix the site, or route it through oploc.ShedScopedLocationSQL / Display().
  // Removing an entry as sites get fixed is encouraged and is the point of "shrink-only".
  const DISPLAY_DRIFT_BASELINE = new Set([
    "backend/internal/obligation/adapters/postgres/repository.go|sql-display-drift",
    "backend/internal/obligation/adapters/postgres/sqlc/query.sql|sql-display-drift",
    "backend/internal/obligation/adapters/postgres/sqlc/query.sql.go|sql-display-drift",
    "backend/internal/counts/adapters/postgres/repository.go|sql-display-drift",
    "backend/internal/counts/adapters/postgres/shifting_destinations.go|sql-display-drift",
    "backend/internal/processintegrity/adapters/postgres/repository.go|sql-display-drift",
    "backend/internal/vaccination/adapters/postgres/repository.go|sql-display-drift",
    "backend/internal/vaccinationexecution/adapters/postgres/repository.go|sql-display-drift",
    "backend/cmd/backfill-verification-subject-labels/main.go|sql-display-drift",
  ]);

  const baselined = [];
  problems = problems.filter((p) => {
    const m = /^([^:]+):\d+: \[(sql-display-drift|go-display-drift)\]/.exec(p);
    if (m && DISPLAY_DRIFT_BASELINE.has(`${m[1]}|${m[2]}`)) {
      baselined.push(p);
      return false;
    }
    return true;
  });
  if (baselined.length > 0) {
    console.error(
      `operational-location guard: ${baselined.length} BASELINED display-drift site(s) ` +
        `(pre-existing debt, not failing the build). Fix and delete the baseline entry when you touch these files.`
    );
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
  console.log("\nRemainingBlind Spots (CAUGHT BY TESTS, NOT THIS GUARD):");
  console.log("  - duplicate-options: dropdowns rendering duplicate text when same partition label");
  console.log("    appears in multiple parks or parks have sheds with identical names (e.g., both");
  console.log("    parks have a shed named 'Godel 1'). Runtime-only; caught by integration tests.");
  console.log("  - location_type laundering: intermediate variable across lines (dataflow required;");
  console.log("    every regex attempt flags correct code). Exact assignment on same line IS caught.");
}

main();
