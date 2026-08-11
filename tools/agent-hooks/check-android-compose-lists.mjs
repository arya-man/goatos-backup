#!/usr/bin/env node

// check-android-compose-lists.mjs — Jetpack Compose lazy-list correctness guard.
// Blocks the LazyColumn/LazyRow/LazyVerticalGrid/LazyHorizontalGrid key crash class
// and the index-key state-loss class. See docs/decisions/mobile-data-fetch-anti-patterns.md
// ("Compose lazy-list key correctness") + docs/mobile/android-ui-quality.md.
//
// Rules (all diff-scoped by default; a genuinely-bounded case appends
// `compose-guard:ignore: <reason>` on the offending line):
//
//   [lazy-list-missing-key]
//     items(<collection>) / itemsIndexed(<collection>) with NO `key =`. Without a
//     stable key Compose uses positional identity, so an insert/remove/reorder reuses
//     an item's remembered state for the WRONG row (checkbox/expand/scroll jumps) and
//     some list mutations crash. The count overload `items(<Int>)` is exempt (no key
//     param exists). Fix: items(list, key = { it.<uniqueRowId> }).
//
//   [lazy-list-entity-id-key]
//     A key whose primary selector is a bare per-ENTITY id (goatId/animalId/...), used
//     on a list that renders one row per OBLIGATION/record. This includes Elvis/fallback
//     keys that try goatId before obligationId. A goat with two due vaccines
//     (ET+TT · PPR) then produces two rows with the SAME key ->
//     "IllegalArgumentException: Key <x> was already used" in the LazyList measure pass,
//     which pops the whole screen (shipped crash, Crashlytics 0.1.6-stg, fixed a9c35a1d).
//     Fix: key on the unique per-row id (obligationId) or a composite that separates the
//     two rows: key = { "${it.goatId}|${it.vaccineLabel}" }.
//
//   [lazy-list-derived-key-drift]
//     A grouped ViewModel identity carries fields that are NOT present in the rendered Compose
//     key. Example shipped in vaccination sheds: grouping by `sopVersionId` / `taskRowVersion`
//     produced two `ShedRow`s, while `ShedRow.id` ignored those fields, so LazyColumn saw two
//     cards with the same key and crashed. Fix: group by the exact card key rendered by Compose,
//     or include every grouping discriminator in the rendered key.
//
//   [lazy-list-parent-location-key]
//     A key whose selector is a bare shed/location id inside partition-aware code. Once shed
//     partitions are operational locations, siblings such as `Godel 2 - Part 1` and
//     `Godel 2 - Part 2` can legitimately share the parent shed id. A list key must use the
//     actual row id or include the partition/operational key.
//
// Phone-scale UI rules (added per the standing "phone-scale UI" maintainer rule — this
// anti-pattern class has shipped 3x: unbounded lazy windowing, chip pickers over unbounded
// dimensions, and full-screen spinners discarding rendered content):
//
//   [nested-scroll-in-lazy-items]
//     A `Column`/`Row` with `verticalScroll(...)`/`horizontalScroll(...)`, OR another
//     `LazyColumn`/`LazyRow`/`LazyVerticalGrid`, placed directly inside an `items(...)` /
//     `itemsIndexed(...)` row lambda. Two scrollables sharing one measure/scroll axis is the
//     "nested scroll" class: infinite-constraint crashes, double-scroll jank, or a scrollable
//     that never gets its own gesture. Fix: hoist the inner list to its own destination/sheet,
//     or replace the outer wrapper with a plain (non-scrolling) layout sized by its content.
//
//   [column-foreach-unbounded]
//     A `Column`/`Row` carrying `verticalScroll`/`horizontalScroll` renders rows via
//     `<state-or-domain-chain>.forEach { ... Composable ... }` instead of `LazyColumn`/`LazyRow`.
//     `forEach` inflates every row up front — no windowing, no recycling — so it is fine for a
//     handful of literal/enum items (a fixed tab strip, `DayOfWeek.entries`) but silently
//     unbounded once the source is state/viewmodel data (sheds, animals, operators, dates) that
//     can legitimately be hundreds of rows on a real park. DELIBERATELY CONSERVATIVE: only flags
//     when the iterated chain contains a state/list/domain-ish keyword (state, list, items, rows,
//     data, records, sheds, animals, operators, dates, goats) — plain small/local receivers
//     (`tabs.forEach`, `DayOfWeek.entries.forEach`, `listOf(...).forEach`) are intentionally
//     never flagged. This trades recall for near-zero noise; a real bug with a receiver name that
//     doesn't match those keywords is a false negative by design.
//
//   [chip-row-unbounded-dimension]
//     A `<state-or-domain-chain>.forEach { ... }` that emits a `FilterChip(`/`AssistChip(` per
//     element, where `<chain>` matches the same narrow state/domain keyword allowlist as
//     column-foreach-unbounded. Chips are one pill per element — correct only for a small FIXED
//     set (2-3 values, e.g. individual/lump-sum toggle) — never for sheds/animals/operators/dates
//     at real park scale. DELIBERATELY CONSERVATIVE, same shape/by-design false negatives as
//     column-foreach-unbounded (a `listOf(...)` literal or `.entries`/`.values()` receiver never
//     matches; indirection through a variable defined elsewhere is invisible to a static grep).
//     Fix: replace the chip row with the `FilterSelectorRow` + `SearchablePickerDialog` pattern in
//     feature-weighing/WeightHistoryChartScreen.kt.
//
//   [spinner-replaces-cached-content]
//     A `when { }` branch guarded by a BARE loading flag (no `&&` compounding it with a
//     cache-emptiness check) whose body is only `CircularProgressIndicator(...)`, in a `when`
//     that also has a sibling branch rendering non-empty cached state (`<x>.isNotEmpty()`, and
//     that branch condition itself is not loading-flagged). This is the "refresh tears down
//     already-rendered content" class — WeighingLeadershipVideosScreen's correct shape compounds
//     the condition (`state.loading && state.sheds.isEmpty() ->`), which this rule treats as
//     exempt (any `&&`/`||` in the condition is skipped). DELIBERATELY NARROW: only fires inside a
//     `when { }` expression with a provable cache-rendering sibling branch — a plain
//     `if (loading) { ... } else { ... }` chain, a pull-to-refresh/`SwipeRefresh` indicator, a
//     button-level/inline spinner, or a first-load-only screen with no cache concept at all are
//     all by-design false negatives (no sibling-branch signal to prove a cache exists). Fix:
//     compound the loading condition with the cache-emptiness check, or overlay/annotate
//     (`Syncing…`) instead of tearing down rendered content — see
//     docs/mobile/android-ui-quality.md "Render cached content during refresh".
//
// NOT implemented as a hard guard (see apps/goatos-android/docs/phone-scale-ui.md +
// .agents/skills/mobile-anti-patterns/SKILL.md for the review-time rule instead):
//   - Missing `contentType =` on `items()`. Whether a list is heterogeneous enough to need
//     `contentType` cannot be decided from the call site alone (needs data-flow into the item
//     composable); a hard rule here would either miss every real case or fire on every
//     single-type list. Left as skill/review guidance only.
//   - Chips over an unbounded dimension, and spinner-over-cached-content, ARE machine-checked
//     above — but only in their narrow forEach{FilterChip}/when{} shapes. A chip row or spinner
//     built a different way (e.g. a custom `Row` loop without `.forEach`, or an `if/else` loading
//     branch instead of `when {}`) is a by-design false negative; the skill/doc above is still the
//     authority for the general rule, this guard only catches the two shipped shapes.
//
// Modes:
//   (default)     diff-scoped: only .kt changed vs $ANDROID_GUARD_BASE (or origin/main).
//   --all         audit the whole apps/goatos-android tree.
//   --self-test   run built-in fixtures and exit.

import { execSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const isAndroidKt = (rel) =>
  rel.startsWith("apps/goatos-android/") &&
  rel.endsWith(".kt") &&
  rel.includes("/src/main/") &&
  !rel.includes("/build/") &&
  !/(?:Test|Fake|Preview)\w*\.kt$/.test(rel) &&
  !rel.endsWith("Entity.kt") &&
  !rel.endsWith("Dto.kt");

// Bare per-entity id selectors that are NOT safe as a lazy key when a list can
// hold >1 row per entity. Row-unique ids (id, obligationId, rowId, uuid, key)
// are intentionally excluded.
const ENTITY_ID = /\b(?:it|row|item|entry|[a-z]\w*)\.(goatId|animalId|goatUuid|animalUuid|herdAnimalId|tagId)\b/;
const ROW_UNIQUE_ID = /\b(?:it|row|item|entry|[a-z]\w*)\.(?:id|rowId|uiKey|stableKey|uuid|key|obligationId|obligationInstanceId|taskId|recordId|eventId|proofId|captureId|completionId|assignmentId)\b/;
const ROW_DISCRIMINATOR = /\b(?:it|row|item|entry|[a-z]\w*)\.(?:vaccineLabel|vaccineId|protocolRuleId|doseLabel|primaryTag|secondaryTag|status|tone|scannedAtLabel|obligationRowVersion)\b/;
const MULTI_ROW_PER_ENTITY_CONTEXT = /obligation|vaccine|vaccination|proof|roster|scan/i;
const DERIVED_KEY_DRIFT_FIELDS = /\b(sopVersionId|taskRowVersion|sopTaskRowVersion|rowVersion|assignmentVersion)\b/;
const PARENT_LOCATION_KEY =
  /^\s*(?:(?:_,\s*)?(?:it|row|item|entry|[a-z]\w*)\s*->\s*)?(?:(?:["']?[\w-]*\$\{)?(?:it|row|item|entry|[a-z]\w*)\.(shedId|parentShedId|sourceShedId|destinationShedId|parentLocationId)(?:\.(?:orEmpty|trim|toString|hashCode)\(\)|!!|\s*\?:[\s\S]*)?(?:\}["']?)?)\s*$/;

const hasPartitionRenderSignal = (body) =>
  /\b(partitionLabel|partitionName|partitionDisplay|partitionKey|operationalLocationDisplay|penName|penLabel|PartitionRow|PartitionCard|PartitionItem|PartitionChip|PartitionLine|OperationalLocationRow)\b/.test(body) ||
  /\b\w*(?:Partition|Pen|OperationalLocation)\w*(?:Row|Card|Item|Tile)\s*\(/.test(body) ||
  /\b\w*(?:Partition|ShedPartition|OperationalLocation|Pen)\w*(?:Location(?:Card|Cell|Chip|Content|Item|Row|Tile|Badge)?|Partition|Badge)\s*\(/.test(body) ||
  /\b\w*(?:PartitionLocation|OperationalLocation|Pen)\w*\s*\(/.test(body) ||
  /\b\w*render(?:ed)?\w*(?:Partition|PartitionLocation|ShedPartition|LocationPartition)\w*\s*\(/.test(body) ||
  /\bitemContent\s*\(/.test(body);

const hasParentLocationRenderSignal = (body) =>
  /\b(parentShedId|sourceShedId|destinationShedId|shedId|parentLocationId)\b/.test(body);

const lineOf = (source, index) => source.slice(0, index).split("\n").length;

// Extract the parenthesised argument text of the call starting at `open`
// (the index of the '(' ). Returns { args, endIndex } with balanced parens,
// ignoring parens inside strings.
function matchCall(source, open) {
  let depth = 0;
  let inStr = null;
  for (let i = open; i < source.length; i++) {
    const c = source[i];
    if (inStr) {
      if (c === "\\") { i++; continue; }
      if (c === inStr) inStr = null;
      continue;
    }
    if (c === '"' || c === "'") { inStr = c; continue; }
    if (c === "(") depth++;
    else if (c === ")") {
      depth--;
      if (depth === 0) return { args: source.slice(open + 1, i), endIndex: i };
    }
  }
  return null;
}

// True when the first positional arg is a count (the `items(count: Int)` overload),
// which has no `key` parameter to require.
function isCountOverload(args) {
  const first = args.split(",")[0].trim();
  return (
    /^\d+$/.test(first) ||
    /^count\b/i.test(first) ||
    /\.(size|count|length)\b/.test(first) ||
    /^[A-Za-z_]\w*Count$/.test(first)
  );
}

// Only Compose lazy scopes define the item DSL. Gate on lazy usage so a stdlib
// `items(` in a repository/util (e.g. a builder or a non-Compose extension) is
// never mistaken for a lazy-list item call.
const usesComposeLazy = (source) =>
  /androidx\.compose\.foundation\.lazy/.test(source) ||
  /\bLazy(Column|Row|VerticalGrid|HorizontalGrid|VerticalStaggeredGrid|HorizontalStaggeredGrid)\b/.test(source);

// Find the trailing lambda `{ ... }` immediately after `afterIndex` (skipping only
// whitespace — a trailing lambda has nothing else between the call's `)` and `{`).
// Balances braces while ignoring braces inside string/char literals. Returns
// { body, bodyStart, bodyEnd } (bodyStart/bodyEnd are indices of the `{`/`}`), or null.
function findTrailingLambda(source, afterIndex) {
  let i = afterIndex + 1;
  while (i < source.length && /\s/.test(source[i])) i++;
  if (source[i] !== "{") return null;
  const open = i;
  let depth = 0;
  let inStr = null;
  for (; i < source.length; i++) {
    const c = source[i];
    if (inStr) {
      if (c === "\\") { i++; continue; }
      if (c === inStr) inStr = null;
      continue;
    }
    if (c === '"' || c === "'") { inStr = c; continue; }
    if (c === "{") depth++;
    else if (c === "}") {
      depth--;
      if (depth === 0) return { body: source.slice(open + 1, i), bodyStart: open, bodyEnd: i };
    }
  }
  return null;
}

// Domain/state-ish keyword allowlist for the column-foreach-unbounded and
// chip-row-unbounded-dimension rules — see the module doc comment above for why this is
// intentionally narrow.
const FOREACH_STATE_KEYWORD =
  /state|viewmodel|rows?|items?|list|data|records?|sheds?|animals?|operators?|dates?|goats?/i;

const CHIP_CALL = /\b(?:FilterChip|AssistChip)\s*\(/;
const CIRCULAR_PROGRESS = /CircularProgressIndicator\s*\(/;

// Balance a `{ ... }` block starting AT a known '{' index (not after it — use
// findTrailingLambda when you need to locate the brace first). Ignores braces inside
// string/char literals, same escaping rules as matchCall/findTrailingLambda.
function blockAt(source, open) {
  let depth = 0;
  let inStr = null;
  for (let i = open; i < source.length; i++) {
    const c = source[i];
    if (inStr) {
      if (c === "\\") { i++; continue; }
      if (c === inStr) inStr = null;
      continue;
    }
    if (c === '"' || c === "'") { inStr = c; continue; }
    if (c === "{") depth++;
    else if (c === "}") {
      depth--;
      if (depth === 0) return { bodyStart: open, bodyEnd: i, body: source.slice(open + 1, i) };
    }
  }
  return null;
}

function extractKeyBody(args) {
  const marker = /\bkey\s*=\s*\{/g.exec(args);
  if (!marker) return null;
  const open = marker.index + marker[0].length - 1;
  const block = blockAt(args, open);
  return block ? block.body.trim() : null;
}

export function findingsForSource(source) {
  const findings = [];
  const seen = new Set();

  // App/ViewModel source rule: a data class named *Identity/*Key that stores version/row metadata
  // but derives a `cardId`/`uiKey` without those fields is a duplicate-key trap once the backend
  // returns multiple rows for the same visible card. This is intentionally narrow and catches the
  // shipped vaccination-sheds shape without trying to do whole-program data-flow.
  const identityClassRe = /\bdata\s+class\s+(\w*(?:Identity|Key))\s*\(([\s\S]*?)\)\s*\{([\s\S]*?)\n\}/g;
  let im;
  while ((im = identityClassRe.exec(source)) !== null) {
    const [, className, ctor, body] = im;
    if (!DERIVED_KEY_DRIFT_FIELDS.test(ctor)) continue;
    const keyLine = body.split("\n").find((line) => /\b(?:cardId|uiKey|stableKey)\b/.test(line));
    if (!keyLine) continue;
    if (DERIVED_KEY_DRIFT_FIELDS.test(keyLine)) continue;
    const line = lineOf(source, im.index);
    const lineText = source.split("\n")[line - 1] || "";
    if (/compose-guard:ignore/.test(lineText)) continue;
    findings.push({
      line,
      rule: "lazy-list-derived-key-drift",
      message:
        `${className} groups on row/version metadata but derives a Compose card key without ` +
        "that metadata. Group by the exact rendered key, or include every grouping discriminator " +
        "in the rendered key, otherwise split backend rows can produce duplicate LazyColumn keys.",
    });
  }

  if (usesComposeLazy(source)) {
    const callRe = /\b(items|itemsIndexed)\s*\(/g;
    let m;
    while ((m = callRe.exec(source)) !== null) {
      const open = m.index + m[0].length - 1;
      const call = matchCall(source, open);
      if (!call) continue;
      const startLine = lineOf(source, m.index);
      const lineText = source.split("\n")[startLine - 1] || "";
      const lambda = findTrailingLambda(source, call.endIndex);
      if (/compose-guard:ignore/.test(lineText)) continue;
      if (!seen.has(`key:${startLine}`)) {
        seen.add(`key:${startLine}`);

        const args = call.args;
        const hasKey = /\bkey\s*=/.test(args);

        if (!hasKey) {
          if (!isCountOverload(args)) {
            findings.push({
              line: startLine,
              rule: "lazy-list-missing-key",
              message:
                "items()/itemsIndexed() over a collection needs key = { it.<uniqueRowId> }; " +
                "positional keys reuse remembered state for the wrong row on insert/reorder.",
            });
          }
        } else {
          // has a key -> check it is not a bare per-entity id on a per-row list.
          const keyBody = extractKeyBody(args);
          if (keyBody) {
            const entityMatch = ENTITY_ID.exec(keyBody);
            const rowUniqueMatch = ROW_UNIQUE_ID.exec(keyBody);
            const hasEntityId = Boolean(entityMatch);
            const hasEntityBeforeRowUnique = Boolean(entityMatch && (!rowUniqueMatch || entityMatch.index < rowUniqueMatch.index));
            const hasCompositeSeparator = /\||joinToString\s*\(| to \b|Pair\s*\(/.test(keyBody);
            const hasRowDiscriminator = ROW_DISCRIMINATOR.test(keyBody) && hasCompositeSeparator;
            const localContext = source.slice(Math.max(0, m.index - 500), Math.min(source.length, call.endIndex + 500));
            const canRenderMultipleRowsPerEntity =
              MULTI_ROW_PER_ENTITY_CONTEXT.test(args) || MULTI_ROW_PER_ENTITY_CONTEXT.test(localContext);
            if (hasEntityId && canRenderMultipleRowsPerEntity && hasEntityBeforeRowUnique && !hasRowDiscriminator) {
              findings.push({
                line: startLine,
                rule: "lazy-list-entity-id-key",
                message:
                  "lazy key selects a per-entity id (goatId/animalId/...): a list with >1 row per " +
                  "entity (e.g. a goat with two due vaccines) yields duplicate keys -> " +
                  "'Key was already used' crash. Key the unique per-row id (obligationId) first, " +
                  "or use a composite only when no row id exists.",
              });
            } else if (
              !hasCompositeSeparator &&
              lambda &&
              hasPartitionRenderSignal(lambda.body) &&
              PARENT_LOCATION_KEY.test(keyBody)
            ) {
              findings.push({
                line: startLine,
                rule: "lazy-list-parent-location-key",
                message:
                  "lazy key selects only a parent shed/location id for a row that renders " +
                  "partition/pen data. Sibling partitions can share the parent id, causing " +
                  "duplicate-key crashes. Key the row id, operational pen id, or a composite " +
                  "including partitionLabel.",
              });
            }
          }
        }
      }

      // Phone-scale rule: nested-scroll-in-lazy-items. Look inside this items() row lambda
      // for another scrollable Column/Row or another Lazy* — both are "two scrollables on
      // one axis" bugs.
      if (lambda) {
        const nestedLazyRe = /\bLazy(Column|Row|VerticalGrid|HorizontalGrid)\b/g;
        let lm;
        while ((lm = nestedLazyRe.exec(lambda.body)) !== null) {
          const absIndex = lambda.bodyStart + 1 + lm.index;
          const line = lineOf(source, absIndex);
          const lineText = source.split("\n")[line - 1] || "";
          if (/compose-guard:ignore/.test(lineText)) continue;
          const key = `nest:${line}`;
          if (seen.has(key)) continue;
          seen.add(key);
          findings.push({
            line,
            rule: "nested-scroll-in-lazy-items",
            message:
              "a Lazy* list is nested directly inside another list's items() row lambda " +
              "(lazy-in-lazy). Hoist the inner list to its own destination/sheet instead of " +
              "nesting two scrollables on the same axis.",
          });
        }
        const nestedScrollRe = /\b(?:verticalScroll|horizontalScroll)\s*\(/g;
        let sm;
        while ((sm = nestedScrollRe.exec(lambda.body)) !== null) {
          const absIndex = lambda.bodyStart + 1 + sm.index;
          const line = lineOf(source, absIndex);
          const lineText = source.split("\n")[line - 1] || "";
          if (/compose-guard:ignore/.test(lineText)) continue;
          const key = `nest:${line}`;
          if (seen.has(key)) continue;
          seen.add(key);
          findings.push({
            line,
            rule: "nested-scroll-in-lazy-items",
            message:
              "a scrollable Column/Row (verticalScroll/horizontalScroll) is nested directly " +
              "inside a list's items() row lambda. Two scrollables sharing an axis is an " +
              "infinite-constraint / double-scroll bug — size the row to its content instead.",
          });
        }
      }
    }
  }

  // Phone-scale rule: column-foreach-unbounded. A scrollable Column/Row rendering rows via
  // `<chain>.forEach { ... }` instead of a Lazy* list. Deliberately narrow — see module doc.
  const containerRe = /\b(Column|Row)\s*\(/g;
  let cm;
  while ((cm = containerRe.exec(source)) !== null) {
    const open = cm.index + cm[0].length - 1;
    const call = matchCall(source, open);
    if (!call) continue;
    if (!/\b(?:verticalScroll|horizontalScroll)\s*\(/.test(call.args)) continue;
    const lambda = findTrailingLambda(source, call.endIndex);
    if (!lambda) continue;
    const forEachRe = /([\w.]+)\.forEach\s*(?:\([^)]*\))?\s*\{/g;
    let fm;
    while ((fm = forEachRe.exec(lambda.body)) !== null) {
      const chain = fm[1];
      const lastSegment = chain.split(".").pop();
      if (lastSegment.toLowerCase() === "entries") continue;
      if (!FOREACH_STATE_KEYWORD.test(chain)) continue;
      const absIndex = lambda.bodyStart + 1 + fm.index;
      const line = lineOf(source, absIndex);
      const lineText = source.split("\n")[line - 1] || "";
      if (/compose-guard:ignore/.test(lineText)) continue;
      const key = `feach:${line}`;
      if (seen.has(key)) continue;
      seen.add(key);
      findings.push({
        line,
        rule: "column-foreach-unbounded",
        message:
          `'${chain}.forEach' renders every row up front inside a scrollable Column/Row with ` +
          "no windowing. If this can be state/viewmodel data at real park scale (sheds/animals/" +
          "operators/dates), use LazyColumn/LazyRow with items(..., key = ...) instead.",
      });
    }
  }

  // Phone-scale rule: chip-row-unbounded-dimension. `<chain>.forEach { ... FilterChip/AssistChip
  // ... }` where the chain looks state/domain-derived. See module doc for the narrow allowlist
  // and the by-design false negatives (listOf(...) literal / .entries / .values() never match the
  // receiver regex at all; indirection through a variable is invisible to a static grep).
  if (CHIP_CALL.test(source)) {
    const chipForEachRe = /([\w.]+)\.forEach\s*(?:\([^)]*\))?\s*\{/g;
    let cfm;
    while ((cfm = chipForEachRe.exec(source)) !== null) {
      const chain = cfm[1];
      const lastSegment = chain.split(".").pop();
      if (lastSegment.toLowerCase() === "entries") continue;
      if (!FOREACH_STATE_KEYWORD.test(chain)) continue;
      const openBrace = cfm.index + cfm[0].length - 1;
      const lambda = findTrailingLambda(source, openBrace - 1);
      if (!lambda) continue;
      if (!CHIP_CALL.test(lambda.body)) continue;
      const line = lineOf(source, cfm.index);
      const lineText = source.split("\n")[line - 1] || "";
      if (/compose-guard:ignore/.test(lineText)) continue;
      const key = `chip:${line}`;
      if (seen.has(key)) continue;
      seen.add(key);
      findings.push({
        line,
        rule: "chip-row-unbounded-dimension",
        message:
          `'${chain}.forEach' emits a FilterChip/AssistChip per element. Chips are correct only ` +
          "for a small FIXED set (2-3 values, e.g. individual/lump-sum) — wrong for sheds, " +
          "animals, operators, dates, or parks at farm scale (~100 sheds/park). Use the " +
          "FilterSelectorRow + SearchablePickerDialog pattern in feature-weighing/" +
          "WeightHistoryChartScreen.kt (one-line selector -> searchable bottom sheet) instead.",
      });
    }
  }

  // Phone-scale rule: spinner-replaces-cached-content. A `when { }` branch guarded by a BARE
  // loading flag (no `&&` compounding it with a cache-emptiness check) whose only content is
  // CircularProgressIndicator, sitting alongside a sibling branch that renders non-empty cached
  // state. See module doc for the deliberately narrow scope and what this does not flag.
  if (CIRCULAR_PROGRESS.test(source)) {
    const whenRe = /\bwhen\s*\{/g;
    let wm;
    while ((wm = whenRe.exec(source)) !== null) {
      const openBrace = wm.index + wm[0].length - 1;
      const block = blockAt(source, openBrace);
      if (!block) continue;
      const body = block.body;
      const lines = body.split("\n");
      const branches = [];
      let offset = 0;
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i];
        const arrowIdx = line.indexOf("->");
        if (arrowIdx !== -1) {
          const cond = line.slice(0, arrowIdx).trim();
          if (cond && !/^[{}]/.test(cond)) branches.push({ cond, lineIdx: i, offset });
        }
        offset += line.length + 1;
      }
      const hasCacheBranch = branches.some(
        (b) => /\.isNotEmpty\(\)/.test(b.cond) && !/loading/i.test(b.cond),
      );
      if (!hasCacheBranch) continue;
      for (const b of branches) {
        if (/&&|\|\|/.test(b.cond)) continue; // compound condition -> already the correct pattern
        if (!/loading/i.test(b.cond)) continue;
        const window = lines.slice(b.lineIdx, b.lineIdx + 8).join("\n");
        if (!CIRCULAR_PROGRESS.test(window)) continue;
        const absIndex = block.bodyStart + 1 + b.offset;
        const line = lineOf(source, absIndex);
        const lineText = source.split("\n")[line - 1] || "";
        if (/compose-guard:ignore/.test(lineText)) continue;
        const key = `spin:${line}`;
        if (seen.has(key)) continue;
        seen.add(key);
        findings.push({
          line,
          rule: "spinner-replaces-cached-content",
          message:
            `'${b.cond}' guards a full-screen CircularProgressIndicator with a BARE loading ` +
            "flag while a sibling when-branch renders non-empty cached state -> refresh tears " +
            "down already-rendered rows for a spinner. Compound with the cache-emptiness check " +
            "(state.loading && state.items.isEmpty() -> ...), the pattern used in " +
            "WeighingLeadershipVideosScreen.kt.",
        });
      }
    }
  }

  return findings;
}

function walkTree(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (["build", "node_modules"].includes(entry.name)) continue;
      out.push(...walkTree(path));
    } else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (isAndroidKt(rel)) out.push(rel);
    }
  }
  return out;
}

function changedFiles() {
  const base = process.env.ANDROID_GUARD_BASE || "origin/main";
  const ranges = [`${base}...HEAD`, "HEAD~1...HEAD"];
  for (const range of ranges) {
    try {
      const refOk = range.split("...")[0];
      execSync(`git rev-parse --verify --quiet ${refOk}^{commit}`, { cwd: repo, stdio: "ignore" });
      const out = execSync(`git diff --name-only --diff-filter=d ${range}`, { cwd: repo, encoding: "utf8" });
      return out.split("\n").map((s) => s.trim()).filter(Boolean).filter(isAndroidKt);
    } catch {
      /* try next range */
    }
  }
  return null;
}

function selfTest() {
  // All fixtures are wrapped in a LazyColumn so the compose-lazy gate applies.
  const wrap = (inner) => `LazyColumn {\n${inner}\n}`;
  const bad = [
    ["items(rows) { r -> Row(r) }", "lazy-list-missing-key"],
    ["itemsIndexed(rows) { i, r -> Row(r) }", "lazy-list-missing-key"],
    ["items(proofRows, key = { row -> row.goatId.takeIf { it.isNotBlank() } ?: row.primaryTag }) { }", "lazy-list-entity-id-key"],
    ['items(proofRows, key = { row -> "proof-${row.goatId}" }) { }', "lazy-list-entity-id-key"],
    ["items(vaccinationRows, key = { it.animalId }) { }", "lazy-list-entity-id-key"],
    [
      `data class ExecutionIdentity(
         val shedId: String,
         val taskId: String?,
         val sopVersionId: String?,
         val taskRowVersion: Int?,
       ) {
         val cardId: String = executionCardId(shedId, taskId)
      }`,
      "lazy-list-derived-key-drift",
    ],
    ["items(proofRows, key = { row -> row.goatId.takeIf { it.isNotBlank() } ?: row.primaryTag }) { }", "lazy-list-entity-id-key"],
    ['items(proofRows, key = { row -> "proof-${row.goatId.takeIf { it.isNotBlank() } ?: row.obligationId.takeIf { it.isNotBlank() } ?: row.primaryTag}" }) { }', "lazy-list-entity-id-key"],
    ["items(vaccinationRows, key = { it.animalId }) { }", "lazy-list-entity-id-key"],
    ["items(rows, key = { it.shedId }) { Text(it.partitionLabel.orEmpty()) }", "lazy-list-parent-location-key"],
    ['items(rows, key = { "shed-${it.shedId}" }) { Text(it.partitionLabel.orEmpty()) }', "lazy-list-parent-location-key"],
    ["items(rows, key = { it.shedId.hashCode() }) { Text(it.partitionLabel.orEmpty()) }", "lazy-list-parent-location-key"],
    ["items(rows, key = { it.parentLocationId.toString() }) { Text(it.partitionLabel.orEmpty()) }", "lazy-list-parent-location-key"],
    ["items(rows, key = { it.shedId.trim() }) { PartitionLocationCard(it) }", "lazy-list-parent-location-key"],
    ["items(rows, key = { it.shedId.orEmpty() }) { OperationalLocationRow(it) }", "lazy-list-parent-location-key"],
    ["items(rows, key = { it.shedId.trim() }) { OperationalLocationRow(it) }", "lazy-list-parent-location-key"],
    ["items(rows, key = { it.parentLocationId }) { PartitionRow(it) }", "lazy-list-parent-location-key"],
    ["items(rows, key = { it.parentLocationId }) { PartitionLocationItem(it) }", "lazy-list-parent-location-key"],
    ["items(rows, key = { it.parentShedId }) { Text(it.operationalLocationDisplay) }", "lazy-list-parent-location-key"],
    ["items(rows, key = { item -> item.parentLocationId }) { item -> itemContent(item) }", "lazy-list-parent-location-key"],
    ["items(rows, key = { row -> row.shedId }) { row -> OperationalLocationRow(row) }", "lazy-list-parent-location-key"],
    ["items(rows, key = { row -> row.shedId }) { row -> CustomPartitionLocationRow(row) }", "lazy-list-parent-location-key"],
  ];
  for (const [inner, rule] of bad) {
    const f = findingsForSource(wrap(inner));
    if (!f.some((x) => x.rule === rule)) throw new Error(`self-test: '${rule}' not flagged for: ${inner.slice(0, 70)}`);
  }
  const good = [
    "items(rows, key = { it.id }) { r -> Row(r) }",
    "items(rows, key = { it.obligationId }) { r -> Row(r) }",
    'items(filtered, key = { row -> row.obligationId.takeIf { it.isNotBlank() } ?: "${row.goatId}|${row.vaccineLabel}" }) { }',
    'items(matches, key = { "match-${it.goatId}|${it.vaccineLabel}" }) { }',
    'items(rows, key = { "${it.shedId}|${it.partitionLabel.orEmpty()}" }) { }',
    "items(rows, key = { it.operationalLocationId }) { Text(it.partitionLabel.orEmpty()) }",
    "itemsIndexed(rows, key = { _, row -> row.locationId }) { _, row -> Text(row.partitionLabel.orEmpty() + row.shedId) }",
    "items(rows, key = { it.locationId }) { Text(it.operationalLocationDisplay + it.shedName) }",
    "items(rows, key = { it.locationId }) { Text(it.operationalLocationDisplay) }",
    "items(3) { Dot() }",
    "items(pageCount) { i -> Page(i) }",
    "items(rows.size) { i -> Row(rows[i]) }",
    "items(rows, key = { it.id }, contentType = { it.goatId }) { }",
    "items(rows) { r -> Row(r) } // compose-guard:ignore: static",
    `data class ExecutionIdentity(
       val shedId: String,
       val taskId: String?,
       val sopVersionId: String?,
       val taskRowVersion: Int?,
     ) {
       val cardId: String = executionCardId(shedId, taskId, sopVersionId, taskRowVersion)
     }`,
  ];
  for (const inner of good) {
    const f = findingsForSource(wrap(inner));
    if (f.length) throw new Error(`self-test: false positive: ${inner.slice(0, 70)} -> ${JSON.stringify(f)}`);
  }
  // Gate: a non-Compose file with a stdlib items( call must never be scanned.
  if (findingsForSource("fun build() { builder.items(rows) }").length) {
    throw new Error("self-test: non-Compose items( should be ignored");
  }

  // Phone-scale: nested-scroll-in-lazy-items.
  const nestedBad = [
    `LazyColumn {
       items(rows, key = { it.id }) { row ->
         Column(modifier = Modifier.verticalScroll(rememberScrollState())) { Detail(row) }
       }
     }`,
    `LazyColumn {
       items(rows, key = { it.id }) { row ->
         LazyRow { items(row.tags, key = { it.tagId }) { TagChip(it) } }
       }
     }`,
  ];
  for (const src of nestedBad) {
    const f = findingsForSource(src);
    if (!f.some((x) => x.rule === "nested-scroll-in-lazy-items")) {
      throw new Error(`self-test: 'nested-scroll-in-lazy-items' not flagged for: ${src.slice(0, 70)}`);
    }
  }
  const nestedGood = [
    `LazyColumn {
       items(rows, key = { it.id }) { row -> Detail(row) }
     }`,
    `LazyColumn {
       items(rows, key = { it.id }) { row ->
         Column(modifier = Modifier.verticalScroll(rememberScrollState())) { Detail(row) } // compose-guard:ignore: card is a fixed 3-field summary, height-capped by design
       }
     }`,
  ];
  for (const src of nestedGood) {
    const f = findingsForSource(src).filter((x) => x.rule === "nested-scroll-in-lazy-items");
    if (f.length) throw new Error(`self-test: false positive nested-scroll: ${src.slice(0, 70)} -> ${JSON.stringify(f)}`);
  }

  // Phone-scale: column-foreach-unbounded.
  const foreachBad = `
    Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
      state.sheds.forEach { shed -> ShedChip(shed) }
    }`;
  {
    const f = findingsForSource(foreachBad);
    if (!f.some((x) => x.rule === "column-foreach-unbounded")) {
      throw new Error("self-test: 'column-foreach-unbounded' not flagged for state.sheds.forEach in scrollable Column");
    }
  }
  const foreachGood = [
    // Fixed enum entries, not state/domain data.
    `Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
       DayOfWeek.entries.forEach { day -> DayChip(day) }
     }`,
    // Literal list, not a variable/parameter.
    `Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
       listOf("A", "B", "C").forEach { label -> Chip(label) }
     }`,
    // No scrollable modifier at all — a bounded fixed-height Column is fine either way.
    `Column {
       state.sheds.forEach { shed -> ShedChip(shed) }
     }`,
    // Escape hatch.
    `Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
       state.sheds.forEach { shed -> ShedChip(shed) } // compose-guard:ignore: capped to <=5 sheds by config
     }`,
  ];
  for (const src of foreachGood) {
    const f = findingsForSource(src).filter((x) => x.rule === "column-foreach-unbounded");
    if (f.length) throw new Error(`self-test: false positive column-foreach: ${src.slice(0, 70)} -> ${JSON.stringify(f)}`);
  }

  // Phone-scale: chip-row-unbounded-dimension.
  const chipBad = `
    Row {
      state.sheds.forEach { shed -> FilterChip(selected = false, onClick = {}, label = { Text(shed.name) }) }
    }`;
  {
    const f = findingsForSource(chipBad);
    if (!f.some((x) => x.rule === "chip-row-unbounded-dimension")) {
      throw new Error("self-test: 'chip-row-unbounded-dimension' not flagged for state.sheds.forEach { FilterChip }");
    }
  }
  const chipGood = [
    // Small fixed literal set (individual/lump-sum style toggle) — listOf(...) never matches
    // the receiver regex, so it's a structural non-match, not a value judgement.
    `Row {
       listOf("Individual", "Lump-sum").forEach { mode -> FilterChip(selected = false, onClick = {}, label = { Text(mode) }) }
     }`,
    // Enum entries.
    `Row {
       WeighingMode.entries.forEach { mode -> AssistChip(onClick = {}, label = { Text(mode.name) }) }
     }`,
    // forEach with no chip inside — not this rule's concern.
    `Column {
       state.sheds.forEach { shed -> Text(shed.name) }
     }`,
    // Escape hatch.
    `Row {
       state.sheds.forEach { shed -> FilterChip(selected = false, onClick = {}, label = { Text(shed.name) }) } // compose-guard:ignore: capped to 3 sheds by config
     }`,
  ];
  for (const src of chipGood) {
    const f = findingsForSource(src).filter((x) => x.rule === "chip-row-unbounded-dimension");
    if (f.length) throw new Error(`self-test: false positive chip-row: ${src.slice(0, 70)} -> ${JSON.stringify(f)}`);
  }

  // Phone-scale: spinner-replaces-cached-content.
  const spinnerBad = `
    @Composable
    fun Screen(state: UiState) {
        when {
            state.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator()
            }
            state.items.isNotEmpty() -> Column { Text("cached") }
            else -> EmptyMessage()
        }
    }`;
  {
    const f = findingsForSource(spinnerBad);
    if (!f.some((x) => x.rule === "spinner-replaces-cached-content")) {
      throw new Error("self-test: 'spinner-replaces-cached-content' not flagged for bare state.loading branch");
    }
  }
  const spinnerGood = [
    // Compound condition — the correct WeighingLeadershipVideosScreen shape.
    `@Composable
     fun Screen(state: UiState) {
         when {
             state.loading && state.items.isEmpty() -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                 CircularProgressIndicator()
             }
             state.items.isNotEmpty() -> Column { Text("cached") }
             else -> EmptyMessage()
         }
     }`,
    // No sibling cache-rendering branch (first-load-only screen) — no evidence a cache exists.
    `@Composable
     fun Screen(state: UiState) {
         when {
             state.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                 CircularProgressIndicator()
             }
             else -> EmptyMessage()
         }
     }`,
    // Plain if/else, not a when{} — outside this rule's deliberately narrow scope.
    `@Composable
     fun Screen(state: UiState) {
         if (state.loading) {
             CircularProgressIndicator()
         } else {
             Column { Text("cached") }
         }
     }`,
    // Escape hatch.
    `@Composable
     fun Screen(state: UiState) {
         when {
             state.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { // compose-guard:ignore: auth gate, no cache concept
                 CircularProgressIndicator()
             }
             state.items.isNotEmpty() -> Column { Text("cached") }
             else -> EmptyMessage()
         }
     }`,
  ];
  for (const src of spinnerGood) {
    const f = findingsForSource(src).filter((x) => x.rule === "spinner-replaces-cached-content");
    if (f.length) throw new Error(`self-test: false positive spinner: ${src.slice(0, 70)} -> ${JSON.stringify(f)}`);
  }

  console.log("check-android-compose-lists self-test: ok");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const all = process.argv.includes("--all");
let files;
if (all) {
  files = walkTree(join(repo, "apps/goatos-android"));
} else {
  files = changedFiles();
  if (files === null) {
    console.log("check-android-compose-lists: skipped (no git diff base; run with --all to audit)");
    process.exit(0);
  }
  if (files.length === 0) files = walkTree(join(repo, "apps/goatos-android"));
}

const findings = [];
for (const rel of files) {
  let source;
  try {
    source = readFileSync(join(repo, rel), "utf8");
  } catch {
    continue;
  }
  for (const f of findingsForSource(source)) findings.push({ ...f, rel });
}

if (findings.length) {
  console.error(`check-android-compose-lists: ${findings.length} violation(s) (see docs/decisions/mobile-data-fetch-anti-patterns.md)`);
  for (const f of findings) console.error(`- ${f.rule} ${f.rel}:${f.line}: ${f.message}`);
  console.error("If a case is genuinely bounded, append `compose-guard:ignore: <reason>` on the line.");
  process.exit(1);
}
console.log(`check-android-compose-lists: ok (${files.length} kotlin file(s) scanned)`);
