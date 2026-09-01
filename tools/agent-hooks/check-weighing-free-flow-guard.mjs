#!/usr/bin/env node

// check-weighing-free-flow-guard.mjs — Weighing is FREE-FLOW: operators scan real ear-tag
// RFIDs, `weighing_observations.animal_id` is nullable per migration
// 000007_weighing_free_flow_scanned_identifier.sql, and there is deliberately NO validation
// against herd roster / vaccination tables / shed ownership before accepting a scan. The same
// scanned_identifier may legitimately appear in different weighing buckets
// (campaign_shed_id). Vaccination stays strict; weighing must never borrow its gating.
// See context/repo-audits/weighing-implementation-do-not-reopen-ledger.md (A-6, B-4, C-3)
// and docs/features/weighing/TRD.md.
//
// Fails on FIFTEEN failure modes across weighing backend code
// (backend/internal/weighing/**, backend/migrations/postgres/*weighing*.sql),
// the Android weighing write-request DTOs (apps/goatos-android/**), and the
// weighing UI surfaces (Android Compose screens + admin-web weighing pages):
//   1. vaccination-or-herd-roster-read-in-write-path — an observation write/submit function
//      joins/reads a vaccination_* table, sop_submissions*, protocol_rules, or a herd-roster
//      membership/ownership assertion before accepting a scan.
//   2. animal-id-not-null-reintroduced — a migration's Up section re-adds
//      `SET NOT NULL` on weighing_observations.animal_id.
//   3. scanned-identifier-unique-across-buckets — a migration's Up section creates a
//      UNIQUE index/constraint on scanned_identifier that does not also key on
//      campaign_shed_id, which would collapse the same scanned RFID across buckets.
//   4. reject-null-animal-id — Go write-path code returns an error because animal_id/AnimalID
//      is nil/empty without routing through the unknown-animal free-flow path.
//   7. write-path-table-not-allowlisted — STRICT: the write path touches a table outside
//      the weighing-owned allowlist (goats, weighing_expected_animals, vaccination_*, ...).
//      This is the catch-all that makes modes 1/5/6 belt-and-braces rather than the only
//      defence.
//   6. roster-state-gate-in-write-path — a write/submit function gates on
//      weighing_expected_animals.status / availability_status. The roster is a label for
//      wrong-shed classification, never a precondition for recording a weight.
//   5. clinical-state-read-in-write-path — a write/submit function reads goats.health_status /
//      goats.lifecycle_status or a clinical-defer vocabulary to decide whether to accept a
//      scan. Weighing must never gate a measurement on clinical state.
//   8. animal-id-required-precondition — a Go struct field tag or validator-library call makes
//      `AnimalID` a required/non-empty precondition anywhere in the weighing module (not just the
//      hand-written `if AnimalID == ""` shape mode 4 already covers). Free-flow must accept a scan
//      with no resolved animal_id at all.
//   9. android-write-request-carries-animal-id — a Kotlin `data class` under
//      `apps/goatos-android/**` named `Weighing*RequestDto` (the observation WRITE request shape,
//      e.g. `WeighingAnimalObservationRequestDto`, `WeighingShedObservationRequestDto`) declares an
//      `animal_id`/`animalId` field. The write request must carry only `scanned_identifier` (plus
//      weight/proof/location); resolving to an animal is a backend-only, best-effort concern.
//  10. expected-denominator-progress-in-ui — a weighing UI surface (Android Compose screens under
//      `apps/goatos-android/**/feature/weighing/**`, or admin-web pages under
//      `apps/admin-web/features/weighing/**` / `apps/admin-web/app/(admin)/weighing/**`) renders or
//      computes a ratio against an expected/roster count (`x/expectedCount`-shaped division, a
//      literal `N/N`, or a literal `/100`). Weighing has no expected-animal denominator; progress
//      is reported only as plain counts.
//  11. observations-animal-id-* — the weighing_observations.animal_id column itself must never
//      exist again. This is stricter than mode 2 (which only forbids re-adding NOT NULL): the
//      column was outright DROPPED by 000078_weighing_observations_drop_animal_id.sql (maintainer
//      decision 2026-08-03 — "not good enough that animal_id is dead but harmless. Delete it."),
//      so a migration after 000078 may never re-add it in any form
//      (observations-animal-id-column-reintroduced), no weighing SQL string may reference
//      weighing_observations together with animal_id (observations-animal-id-column-referenced),
//      and no weighing Go struct outside the weighing_expected_animals roster catalog
//      (ExpectedAnimal, rosterCursor) may declare an AnimalID field tagged `json:"animal_id"`
//      (observations-animal-id-field-reintroduced).
//  12. expected-animals-table-reintroduced — weighing_expected_animals (the SECOND expected-set/
//      herd-roster model — CreateCampaign/UpdateCampaign populating it from goats +
//      herd_register_is_kid, ListScopeRoster joining it to goats/goat_identifiers, a dead
//      RefreshAvailability writing goats.health_status/lifecycle_status into it) was DROPPED
//      OUTRIGHT by 000079_weighing_drop_expected_animals_table.sql (free-flow mandate,
//      2026-08-03 — "delete it entirely"). A migration after 000079 may never CREATE this table
//      again.
//  13. expected-animals-table-referenced — no live (non-comment) weighing Go code anywhere in the
//      module may reference weighing_expected_animals at all: the table does not exist.
//  14. roster-cursor-type-reintroduced — `type rosterCursor struct` (the AnimalID-keyed cursor
//      that used to page the dead expected-animal roster; it and its encode/decode helpers were
//      deleted outright once the dead `cursor`/`includeRoster` params were dropped from
//      ListScopeRoster) must never be declared again anywhere in the weighing module.
//  15. roster-items-populated — a `domain.RosterPage{...}` composite literal assigns Items to
//      anything other than an empty slice. RosterPage.Items/NextCursor stay ON THE WIRE for
//      older-client compatibility (a breaking response-shape change is out of scope), but
//      free-flow has no expected roster to serve, so nothing may ever populate them.
//
// Modes:
//   (default)     scan the real weighing backend tree + weighing migrations + Android/admin-web
//                 weighing surfaces.
//   --self-test   run adversarial good/bad fixtures for all eleven failure modes and exit.
//
// Blind spots (native Grep/Read must still catch these): dynamically built SQL strings
// (string concatenation/fmt.Sprintf assembling table names), reflection-based query builders,
// any new write-path function not listed in WRITE_FN_NAMES below (extend the list when a
// new observation-accepting function is added), and — for mode 9 specifically — a future write
// request DTO that is NOT named `Weighing*RequestDto` (e.g. reusing a shared/generic request
// shape, or embedding the field via a base class/interface rather than a literal property in the
// class body). This guard only parses literal Kotlin `data class` bodies textually; it does not
// resolve Kotlin type inheritance, so a field inherited from a shared base type would not be seen.
// Mode 10 is a textual heuristic over ratio-shaped expressions and literal `/100`/`N/N`; a
// denominator computed indirectly (e.g. via an intermediate variable whose name does not contain
// "expected") would not be caught by name-matching alone.

import { readFileSync, readdirSync, existsSync, mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

// WEIGHING_GUARD_TEST_REPO lets the exit-code self-test point this script at a throwaway
// fixture tree instead of the real repo, so it can prove process.exit(1)/exit(0) behavior by
// spawning this same script as a child process against planted good/bad fixtures.
const repo = process.env.WEIGHING_GUARD_TEST_REPO
  ? resolve(process.env.WEIGHING_GUARD_TEST_REPO)
  : resolve(import.meta.dirname, "../..");
const SELF_PATH = fileURLToPath(import.meta.url);

const WEIGHING_DIR = "backend/internal/weighing";
const MIGRATIONS_DIR = "backend/migrations/postgres";

// Functions that accept/write a weighing observation (submit path). Extend this list when a
// new observation-writing function is added to the weighing module.
const WRITE_FN_NAMES = [
  "RecordAnimalObservation",
  "recordUnknownAnimalObservationTx",
  "RecordShedObservation",
  "classifyAnimalObservationRejection",
  "classifyShedObservationRejection",
  "SubmitIndividualScope",
];

const FORBIDDEN_TABLE_RE = /\b(?:FROM|JOIN)\s+(vaccination_\w+|sop_submissions\w*|sop_submission_items\w*|protocol_rules\w*|vaccination_completions\w*)\b/i;
const FORBIDDEN_ROSTER_FN_RE = /\b(AssertHerdRosterMembership|ValidateHerdRoster|ValidateAgainstHerdRegister|RequireShedOwnership|AssertShedOwnership)\s*\(/;

// Failure mode 5: reading a goat's CLINICAL STATE on the write path.
//
// This rule exists because the guard as originally written did NOT catch it. A
// "critical-animal-action" gate was added to RecordAnimalObservation that joined
// `goats` and refused the write when health_status was sick/under_treatment/
// recovering/quarantine/icu, or lifecycle_status was an exit state. It passed
// every other free-flow check here and still broke free-flow, because it made the
// write depend on resolved herd identity.
//
// Maintainer decision 2026-07-31: weighing records what the scale and scanner saw.
// Putting an animal on a scale administers nothing, so a clinical state must never
// block the measurement — and refusing it destroys exactly the weight trend a vet
// needs for an animal under treatment. Vaccination stays strict; weighing must not
// borrow its gating.
//
// Matches a health/lifecycle column predicate or the shared clinical-state
// vocabularies, inside a weighing write function. Deliberately does NOT ban the
// `goats` join outright: the write path still legitimately reads
// g.current_location_id to label where the animal actually was.
const FORBIDDEN_CLINICAL_RE = /(health_status|lifecycle_status|MandatoryClinicalDeferStates|ExitLifecycleStates|EffectiveClinicalDeferStates)/;

// Failure mode 6: gating the write on the EXPECTED-ANIMAL ROSTER.
//
// The clinical gate's twin, one table over. The known-animal write CTE used to
// INNER JOIN weighing_expected_animals carrying
//   AND ea.status <> 'unavailable'
//   AND ea.availability_status NOT IN ('icu','quarantine','dead',...)
// so a resolved animal that was off-roster, or whose roster snapshot said the
// animal was away, produced an empty CTE and surfaced to the operator as a 404.
// availability_status is a periodically-refreshed SNAPSHOT, so it was stale-gating
// too. Roster membership is a LABEL for wrong-shed classification, never a
// precondition for recording a weight (ledger B-0 / B-4).
//
// Matches a roster STATUS/AVAILABILITY predicate inside a weighing write function.
// Reading ea.expected_location_id / expected_location_label for labelling stays
// allowed, so this deliberately keys on the status columns, not on the table name.
// Matches only COMPARISON forms (NOT IN / IN / <> / !=). A bare `=` is excluded on
// purpose: `UPDATE weighing_expected_animals SET status='weighed',
// availability_status=CASE ...` is weighing WRITING BACK progress to the roster,
// which is allowed — the ban is on the roster DECIDING whether the write happens.
const FORBIDDEN_ROSTER_STATE_RE = /(availability_status\s*(?:NOT\s+)?IN\s*\(|availability_status\s*(?:<>|!=)|\bea\.status\s*(?:<>|!=)|\bea\.status\s+(?:NOT\s+)?IN\s*\()/i;

function extractFunctionBody(source, fnName) {
  // Matches `func (recv) Name(` or `func Name(` and returns the body up to the next
  // column-zero `func ` (heuristic used elsewhere in this repo's guards).
  const re = new RegExp(`\\nfunc\\s+(?:\\([^)]*\\)\\s+)?${fnName}\\s*\\(`);
  const m = re.exec("\n" + source);
  if (!m) return null;
  const start = m.index + m[0].length;
  const rest = source.slice(start);
  const nextFn = rest.search(/\nfunc\s/);
  const body = rest.slice(0, nextFn < 0 ? rest.length : nextFn);
  return body;
}

// RULES ARE ABOUT CODE, NOT PROSE.
//
// Every pattern below is tested against the function body with comments removed.
// Without this, a comment EXPLAINING a banned predicate — including the comment
// that records why it was deleted — trips the guard, so documenting the ban makes
// the build fail and deleting the explanation makes it pass. That is the same
// prose-vs-code defect that check-vaccination-hrms-seed-fixture.mjs had.
//
// Blind spot: a banned predicate hidden inside a Go string literal that itself
// contains `//` or `--` before the predicate would be stripped. No weighing query
// does that; the self-test below pins the comment case in both directions.
function stripComments(text) {
  return text
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .split("\n")
    // `(?<!:)` keeps `http://` inside a Go string literal from being treated as a
    // comment start, which previously truncated the rest of the line and silently
    // deleted a real banned predicate sitting after a URL (demonstrated bypass).
    .map((line) => line.replace(/(?<!:)\/\/.*$/, "").replace(/--.*$/, ""))
    .join("\n");
}

// Failure mode 7: TABLE ALLOWLIST on the weighing write path (the strict rule).
//
// Modes 1/5/6 each blocked one back door AFTER it shipped: vaccination tables, then
// goats.health_status, then weighing_expected_animals.availability_status. Chasing
// patterns one incident at a time is why three separate gates reached main. This rule
// inverts it: the weighing write path may touch ONLY weighing-owned tables plus proof
// and the generic infrastructure tables. Anything else is a finding by default, so a
// FOURTH back door cannot be invented.
//
// Maintainer decision 2026-07-31 (strict form): weighing write may use campaign,
// campaign_sheds, proof_artifacts, weighing_observations, and idempotency/audit/outbox.
// No goats. No weighing_expected_animals. No vaccination tables. No clinical state.
// THE ONE RECORDED EXCEPTION TO WEIGHING ISOLATION (maintainer decision 2026-08-07).
//
// The Kids — Weights screen reports average weight by BREED, SEX and MANAGEMENT
// STAGE. Weighing stores a scanned tag and a weight, so those three facts can only
// come from resolving the tag to its animal. The maintainer directed this
// explicitly: "for id's we have details right what type they are", and "for
// lumpsum use what that shed is assigned to".
//
// The exception is FILE-SCOPED on purpose. Adding goats/goat_identifiers to the
// global allowlist would silently unlock every weighing file, including the write
// path, which is the exact 2026-08-04 defect this guard exists to prevent. Only
// the named reporting file may resolve a tag, and only to read breed/sex/stage.
// Maintainer decision 2026-08-19: the same reporting file may also read
// goat_shed_partitions only to label lump-sum composition at exact shed/partition
// grain. This covers historical display rows such as Castro 1/2/3, Gandhi 1/2/3,
// legacy Gandi 1/2/3, and Godel 2 - Part 1 without letting the write path use
// per-goat location data.
//
// Boundaries that still hold inside the exempt file, and are the reason this is
// safe: it is READ-ONLY, it is a reporting path with no capture, submit or close
// behaviour, no scan is gated on identity, and a tag that resolves to nothing is
// counted and reported rather than rejected — free-flow capture is untouched.
//
// THE SECOND RECORDED EXCEPTION (maintainer decision 2026-08-24): the LUMP-SUM
// CENSUS SNAPSHOT, lump_sum_census.go. Operators kept typing wrong head counts,
// so the maintainer ruled that the lump-sum submit no longer accepts a typed
// count: RecordShedObservation snapshots the bucket's live resident count from
// goats + goat_shed_partitions inside the submit transaction, freezes it on the
// row forever, and derives the average from it. This is knowingly a WRITE-PATH
// read — the guard's cross-file scanning would not see the call from
// RecordShedObservation into this helper, so the exemption is recorded here
// EXPLICITLY rather than left to that blind spot. Its boundaries: one COUNT of
// the bucket's own (shed, pen) residents, no per-animal identity leaves the
// query, individual free-flow capture is untouched (scans still accepted
// verbatim, unknown tags still counted), and the only gate it adds is the
// maintainer-ruled zero-census refusal (ports.ErrShedCountUnavailable).
//
// THE THIRD RECORDED EXCEPTION (maintainer decision 2026-08-26): the WEIGHTS
// SEX FILTER, sex_scope.go. The maintainer asked for the Weights page's Sex
// filter to govern the WHOLE page — the shed table, the KPI row, the growth
// leaderboard and the Growth Director widgets — and a weighing row knows only a
// scanned string, so something must say which strings belong to a male kid.
//
// The alternative was to let shed_weights.go, growth.go and the Growth Director
// reads each join goat_identifiers, which is exactly the leak the 2026-08-04
// defect was about. Instead ONE file answers the question and hands the other
// reads an OPAQUE list — tag strings and (location, partition) buckets — so
// those files still name no herd table and still know nothing about animals.
//
// Its boundaries: READ-ONLY and REPORTING-ONLY, no capture/submit/close/verdict
// path calls it; NO scan is gated on identity; an empty sex resolves to an empty
// scope that every caller reads as "no filter", so the unfiltered page runs the
// query it ran before this file existed; and a whole-shed weigh is claimed only
// when its cohort is provably one sex, never split across a mix.
//
// Adding a file here is a MAINTAINER decision, never a developer convenience.
// The herd tables the three reporting/census exemptions share. Vaccination, clinical, protocol
// and obligation tables stay banned everywhere.
const HERD_JOIN_BASE_TABLES = ["goats", "goat_identifiers", "goat_shed_partitions"];

// EXEMPTION IS PER FILE, AND SO IS THE TABLE LIST. Each entry names the tables THAT file may
// resolve and nothing more, so widening one exemption cannot silently widen the others: when
// origin_scope.go earned `procurement_load_goats` on 2026-09-01, a single shared table set would
// have handed a procurement table to the census and demographics files too, neither of which has
// any business asking where an animal was bought.
const HERD_JOIN_EXEMPT_FILES = new Map([
  [
    "backend/internal/weighing/adapters/postgres/weight_demographics.go",
    {
      reason:
        "maintainer decisions 2026-08-07/2026-08-19: average weight by breed/sex/stage and lump-sum shed/partition composition on the Weights screen",
      tables: HERD_JOIN_BASE_TABLES,
    },
  ],
  [
    "backend/internal/weighing/adapters/postgres/sex_scope.go",
    {
      reason:
        "maintainer decision 2026-08-26: resolves the Weights page's Sex filter to a tag list and a lump-sum bucket list, so the other weighing reads filter without naming a herd table",
      tables: HERD_JOIN_BASE_TABLES,
    },
  ],
  [
    "backend/internal/weighing/adapters/postgres/lump_sum_census.go",
    {
      reason:
        "maintainer decision 2026-08-24: lump-sum submit snapshots the bucket's resident head count from the herd register (frozen on the row; operator no longer types it)",
      tables: HERD_JOIN_BASE_TABLES,
    },
  ],
  [
    "backend/internal/weighing/adapters/postgres/origin_scope.go",
    {
      // The Farm born / Purchased filter. It began pen-level and needed NO exception, reading only
      // the weighing-owned load-tag mapping; that is correct for a pen whose every resident came
      // off a load and wrong for a MIXED one (Mandela 1 - Part 1 is 4 bought of 13), which filed
      // nine farm-born kids as purchased. A scanned weigh carries a tag and can be answered per
      // ANIMAL, and procurement_load_goats is the only table that says which animal came off which
      // load -- so the exception buys accuracy the pen-level rule cannot reach at any price.
      // READ-ONLY and REPORTING-ONLY; no capture, submit, close or verdict path calls it.
      reason:
        "maintainer decision 2026-09-01: resolves the Weights page's Farm born / Purchased filter per ANIMAL for scanned weighs, so a pen holding both cohorts is not claimed whole",
      tables: [...HERD_JOIN_BASE_TABLES, "procurement_load_goats"],
    },
  ],
]);

const WRITE_PATH_ALLOWED_TABLES = new Set([
  "weighing_campaigns",
  "weighing_campaign_sheds",
  "weighing_observations",
  "weighing_shed_observations",
  // Per-proof child rows of a lump-sum shed observation (1-5 videos). Weighing-owned.
  "weighing_shed_observation_proofs",
  "proof_artifacts",
  // Generic infrastructure the write transaction legitimately owns.
  "outbox_messages",
  "audit_log",
  // Weighing's own idempotency ledger (module-scoped table, not a herd table).
  "weighing_idempotency_records",
  // Shed -> procurement-load mapping for load-wise growth on the Weights screen
  // (000131, maintainer decision 2026-08-08). WEIGHING-OWNED, and that is the whole
  // point of it: the farm's mapping is SHED-level ("Castro 1 + Castro 2 came from
  // load 131"), so weighing can answer "which supplier's animals grow best" from a
  // table of its own instead of reading procurement_loads / procurement_load_goats.
  // Adding a PROCUREMENT table to this set would be the isolation breach; adding
  // this one is not. It carries a load reference, a supplier name and a shed id --
  // no animal identity, no clinical state, no other module's rules -- and it is read
  // ONLY, on a reporting path, by load_weights.go. It is deliberately NOT a foreign
  // key to procurement_loads, which would reintroduce the dependency by the back
  // door. Widening this to per-animal load membership means procurement_load_goats
  // and a recorded maintainer exception, exactly like the goats/goat_identifiers one.
  "weighing_shed_load_tags",
  // Weighing-owned kernel execution rows; submit marks the exact bucket completed so
  // mobile lists stop reopening finished sheds without reading any herd/vaccine roster.
  "weighing_work_items",
]);

// A CTE may never be NAMED after a banned table. Otherwise it shadows it:
//   WITH weighing_expected_animals AS (SELECT ... FROM weighing_expected_animals ...)
//   SELECT * FROM weighing_expected_animals
// collects the CTE name, and every reference to the REAL banned table inside the CTE
// body is then treated as a reference to the local CTE — silently exempting the exact
// thing mode 7 exists to catch. An adversarial review demonstrated this against goats
// and weighing_expected_animals both.
const BANNED_SHADOW_RE = /^(goats|goat_\w+|weighing_expected_animals|vaccination_\w*|herd_\w+|protocol_\w+|sop_\w+)$/i;

// CTE names are legal FROM/JOIN targets; collect the ones this body defines. Returns
// both the usable names and any that illegally shadow a banned table.
function cteNames(body) {
  const names = new Set();
  const shadows = new Set();
  const re = /(?:\bWITH\s+|,\s*)([a-z_][a-z0-9_]*)\s+AS\s*\(/gi;
  let m;
  while ((m = re.exec(body)) !== null) {
    const name = m[1].toLowerCase();
    if (BANNED_SHADOW_RE.test(name)) {
      shadows.add(name);
      continue; // deliberately NOT exempted
    }
    names.add(name);
  }
  return { names, shadows };
}

export function writePathTableFindings(rel, fn, body) {
  const findings = [];
  const { names: ctes, shadows } = cteNames(body);
  for (const shadow of shadows) {
    findings.push({
      rule: "cte-shadows-banned-table",
      message: `${rel}: ${fn}() defines a CTE named \`${shadow}\`, which collides with a banned table name — a CTE may not shadow goats/weighing_expected_animals/vaccination/herd tables, because that would make every reference to the real table look like a local CTE reference and silently defeat the allowlist. Rename the CTE.`,
    });
  }
  // `IS [NOT] DISTINCT FROM <expr>` is a COMPARISON OPERATOR whose right-hand side is a value,
  // not a table. Without the negative lookbehind the guard reads the operator's FROM as a table
  // reference and reports whatever alias follows -- e.g. `prior.proof_artifact_id IS DISTINCT
  // FROM p.proof_id` was reported as a phantom table `p`. That fired on the fan-out fix, i.e. it
  // punished the correct change-detection this guard exists to encourage, exactly as the `FOR
  // UPDATE OF` case below did for correct row locking.
  const re = /(?<!\bDISTINCT\s)\b(?:FROM|JOIN|INTO|UPDATE)\s+(?:public\.)?([a-z_][a-z0-9_]*)/gi;
  let m;
  const seen = new Set();
  while ((m = re.exec(body)) !== null) {
    const table = m[1].toLowerCase();
    if (seen.has(table)) continue;
    seen.add(table);
    if (WRITE_PATH_ALLOWED_TABLES.has(table) || ctes.has(table)) continue;
    // SQL keywords that can follow UPDATE/FROM in the shapes we scan.
    // "of" is the row-lock form `FOR [NO KEY] UPDATE OF <alias>`: the token after UPDATE is the
    // keyword OF, not a table. Without this the guard reports a phantom table named `of` on any
    // write path that takes an explicit row lock -- i.e. it fired on the fix that closed the
    // late-capture-after-close race, punishing the correct locking it exists to encourage.
    if (["set", "select", "only", "unnest", "lateral", "values", "of"].includes(table)) continue;
    findings.push({
      rule: "write-path-table-not-allowlisted",
      message: `${rel}: ${fn}() reads/writes \`${table}\` — the weighing write path is restricted to weighing-owned tables plus proof/idempotency/audit/outbox. goats, weighing_expected_animals and vaccination tables are banned outright (maintainer decision 2026-07-31, strict form). If this table is genuinely weighing-owned, add it to WRITE_PATH_ALLOWED_TABLES with a reason.`,
    });
  }
  return findings;
}

// The write path is not only the functions named above. Extracting a gate into a
// private helper (`r.checkClinicalEligibility(...)`) previously defeated every rule
// here, because only the named function's own text was scanned — an innocent-looking
// refactor could silently turn the guard green. So the effective write path is the
// transitive closure of same-file methods called from the named entry points.
//
// Blind spot that remains: a helper defined in a DIFFERENT file of the weighing
// package. Those files are still scanned for their own named entry points, and the
// allowlist rule (mode 7) applies to every function reached here, so a cross-file
// helper would have to be both unlisted AND unreferenced to hide.
function writePathBodies(source) {
  const bodies = new Map();
  const queue = [...WRITE_FN_NAMES];
  const seen = new Set();
  while (queue.length) {
    const fn = queue.shift();
    if (seen.has(fn)) continue;
    seen.add(fn);
    const raw = extractFunctionBody(source, fn);
    if (raw == null) continue;
    bodies.set(fn, raw);
    // r.someHelper( / someHelper( — same-file callees.
    const callRe = /\b(?:r|repo)\.([a-zA-Z_][A-Za-z0-9_]*)\s*\(/g;
    let m;
    while ((m = callRe.exec(raw)) !== null) {
      if (!seen.has(m[1])) queue.push(m[1]);
    }
  }
  return bodies;
}

// Failure mode 16: WHOLE-FILE TABLE ALLOWLIST — every SQL statement in the weighing package,
// READ PATHS INCLUDED.
//
// Mode 7 only inspects functions reachable from the known write/submit entry points. A READ
// model is reachable from none of them, so a leadership/report query could join any herd table
// and this guard would pass. That is not hypothetical: a weight-history/ADG read model shipped a
// `LEFT JOIN goat_identifiers` to resolve a scanned tag to a goat_id, and every one of the 15
// existing modes stayed green because the join lived on a read path.
//
// Weighing is ISOLATED. Not "isolated on writes" — isolated. It owns its tables and reads
// NOTHING from the herd, vaccination, protocol, or any other module's schema, in any direction,
// on any path. If weighing needs a fact, weighing must have captured it.
// Tables weighing may read on ANY path besides its own. Deliberately SHORT and explicit.
//
// These are ORG tables — where a park is, who an operator is, what they may see. They are NOT
// animal data. Weighing on main already reads them, because a weighing task belongs to a park
// and is assigned to a person; removing them would break existing behaviour, not isolate it.
//
// What is banned outright, on every path: goats, goat_identifiers, herd_*, vaccination_*, sop_*,
// protocol_*, obligation_* — anything describing an ANIMAL or another module's rules. Weighing
// knows a scanned string and a weight. It does not know what animal that is, and must not ask.
//
// CRITICAL: goat_shed_partitions is BANNED except in the file-scoped reporting
// exception above (PER-GOAT table, reveals which animal sits where). shed_partitions
// (ORG-scoped catalog of existing partitions) is ALLOWED. See BANNED_SHADOW_RE and
// guard self-test below for enforcement.
//
// Adding to this set is a maintainer decision, not a developer convenience.
const NON_WEIGHING_TABLES_ALLOWED_ON_READ = new Set([
  "locations",
  "workforce_members",
  "user_scope_grants",
  // shed_partitions: catalog of partitions that physically exist, keyed by location ID.
  // ORG-scoped (tenant_id, shed_id, normalized_label), not per-animal. Maintained by migration
  // 000112. Resolves partition_label in weighing_campaign_sheds without reading goat_shed_partitions.
  // Maintainer decision 2026-08-06: exact catalog wins over name-parsing inference.
  "shed_partitions",
  // Weighing's OWN lifecycle notifications (assigned/submitted/reopened/rework/closed). Shared
  // delivery plumbing, not another module's animal data.
  "notification_requests",
  // Weighing-owned kernel table; belongs with the weighing_* family.
  "weighing_work_items",
]);

export function anyPathTableFindings(rel, source) {
  const findings = [];
  // Tests are excluded: a fake repository's PROSE ("reads from an oversight view") is not SQL,
  // and scanning it produced phantom tables named `the`, `an` and `oversight`.
  if (rel.endsWith("_test.go")) return findings;
  // Only real SQL is scanned: Go backtick raw-string literals. Scanning the whole file matched
  // English sentences in comments, which is how the first cut of this rule cried wolf.
  const sql = [...stripComments(source).matchAll(/`([^`]*)`/g)]
    .map((m) => m[1])
    .filter((lit) => /\b(?:SELECT|INSERT|UPDATE|DELETE|WITH)\b/i.test(lit))
    .join("\n");
  if (!sql.trim()) return findings;
  const body = sql;
  const { names: ctes } = cteNames(body);
  // Supplementary CTE sweep: cteNames() parses a single statement's WITH clause, but this rule
  // concatenates every SQL literal in the file, and a query can be assembled from several
  // literals (a shared base CTE in one const, the SELECT that uses it in another). Any
  // `name AS (` in the scanned text is a local CTE, not a table.
  for (const m of body.matchAll(/\b([a-z_][a-z0-9_]*)\s+AS\s*\(/gi)) {
    ctes.add(m[1].toLowerCase());
  }
  const re = /(?<!\bDISTINCT\s)\b(?:FROM|JOIN|INTO|UPDATE)\s+(?:public\.)?([a-z_][a-z0-9_]*)/gi;
  let m;
  const seen = new Set();
  while ((m = re.exec(body)) !== null) {
    const table = m[1].toLowerCase();
    if (seen.has(table)) continue;
    seen.add(table);
    if (WRITE_PATH_ALLOWED_TABLES.has(table) || ctes.has(table)) continue;
    if (NON_WEIGHING_TABLES_ALLOWED_ON_READ.has(table)) continue;
    // SQL keywords that legitimately follow FROM/JOIN/INTO/UPDATE in the shapes we scan.
    // Beyond mode 7's list: "with" (FROM ... UPDATE ... WITH), "update" (FOR UPDATE / DO UPDATE),
    // "skip" (FOR UPDATE SKIP LOCKED), "nothing" (ON CONFLICT DO NOTHING). Each of these was a
    // real phantom-table report on correct code before being listed.
    if ([
      "set", "select", "only", "unnest", "lateral", "values", "of",
      "with", "update", "skip", "nothing", "conflict", "returning", "where",
    ].includes(table)) continue;
    const exemption = HERD_JOIN_EXEMPT_FILES.get(rel);
    if (exemption && exemption.tables.includes(table)) continue;
    findings.push({
      rule: "weighing-reads-non-weighing-table",
      message: `${rel}: reads \`${table}\` — weighing is ISOLATED and may touch ONLY weighing-owned tables (plus proof/idempotency/audit/outbox), on READ paths as well as writes. Joining goats/goat_identifiers/vaccination/herd tables is banned outright, including from a report or read model (maintainer decision 2026-08-04). If weighing needs this fact, weighing must capture it itself.`,
    });
  }
  return findings;
}

export function findingsForGoSource(rel, source) {
  const findings = [];
  findings.push(...anyPathTableFindings(rel, source));
  for (const [fn, rawBody] of writePathBodies(source)) {
    const body = stripComments(rawBody);
    if (FORBIDDEN_TABLE_RE.test(body)) {
      findings.push({
        rule: "vaccination-or-herd-roster-read-in-write-path",
        message: `${rel}: ${fn}() joins/reads a vaccination/SOP/protocol table — weighing free-flow must never gate a scan on vaccination or herd-roster state`,
      });
    }
    if (FORBIDDEN_ROSTER_FN_RE.test(body)) {
      findings.push({
        rule: "vaccination-or-herd-roster-read-in-write-path",
        message: `${rel}: ${fn}() calls a herd-roster/shed-ownership assertion — weighing free-flow must accept any scanned RFID without roster/ownership validation`,
      });
    }
    findings.push(...writePathTableFindings(rel, fn, body));
    if (FORBIDDEN_ROSTER_STATE_RE.test(body)) {
      findings.push({
        rule: "roster-state-gate-in-write-path",
        message: `${rel}: ${fn}() gates on the expected-animal roster status/availability_status — weighing free-flow treats the roster as a LABEL for wrong-shed classification, never a precondition for recording a weight (maintainer decision 2026-07-31)`,
      });
    }
    if (FORBIDDEN_CLINICAL_RE.test(body)) {
      findings.push({
        rule: "clinical-state-read-in-write-path",
        message: `${rel}: ${fn}() reads a goat clinical/lifecycle state (health_status, lifecycle_status, or a clinical-defer vocabulary) — weighing is free-flow and must record the scale reading without gating on herd clinical state (maintainer decision 2026-07-31; vaccination stays strict)`,
      });
    }
    // Failure mode 4: rejecting because animal_id is null/empty without routing through the
    // unknown-animal free-flow path.
    const rejectRe = /(?:cmd\.)?AnimalID\s*==\s*(?:""|nil)[\s\S]{0,220}?return[\s\S]{0,120}?(?:Err\w*|error)/;
    const rm = rejectRe.exec(body);
    if (rm) {
      const windowStart = Math.max(0, rm.index - 200);
      const window = body.slice(windowStart, rm.index + rm[0].length + 200);
      if (!/recordUnknownAnimalObservationTx|ScannedIdentifier|unknown[A-Za-z]*Animal/i.test(window)) {
        findings.push({
          rule: "reject-null-animal-id",
          message: `${rel}: ${fn}() appears to reject an observation solely because animal_id is null/empty — free-flow must route null-animal_id scans through the unknown-animal path (scanned_identifier), never reject them`,
        });
      }
    }
  }
  return findings;
}

function upSection(sql) {
  // goose migrations mark Up/Down with `-- +goose Up` / `-- +goose Down`. Only the Up section
  // represents the forward (currently-applied) schema; the Down section legitimately restores
  // the old NOT NULL constraint as a rollback and must not be flagged.
  const downIdx = sql.search(/--\s*\+goose\s+Down/i);
  return downIdx < 0 ? sql : sql.slice(0, downIdx);
}

export function findingsForMigrationSource(rel, sql) {
  const findings = [];
  const up = upSection(sql);

  if (/weighing_observations[\s\S]{0,400}?ALTER COLUMN\s+animal_id\s+SET\s+NOT\s+NULL/i.test(up)) {
    findings.push({
      rule: "animal-id-not-null-reintroduced",
      message: `${rel}: Up section re-adds NOT NULL on weighing_observations.animal_id — this column must stay nullable per migration 000007 (free-flow scanned_identifier contract)`,
    });
  }

  const uniqueRe = /CREATE\s+UNIQUE\s+INDEX[^;]*?ON\s+(?:public\.)?weighing_observations\s*\(([^)]*)\)|ADD\s+CONSTRAINT\s+\w+\s+UNIQUE\s*\(([^)]*)\)/gi;
  let um;
  while ((um = uniqueRe.exec(up)) !== null) {
    const cols = (um[1] || um[2] || "").toLowerCase();
    if (cols.includes("scanned_identifier") && !cols.includes("campaign_shed_id")) {
      findings.push({
        rule: "scanned-identifier-unique-across-buckets",
        message: `${rel}: UNIQUE index/constraint on scanned_identifier without campaign_shed_id would collapse the same scanned RFID across different weighing buckets — include campaign_shed_id in the key or drop the uniqueness`,
      });
    }
  }

  // Failure mode 11: the weighing_observations.animal_id column must never come
  // back, in any migration AFTER the one that dropped it. It was removed
  // outright by 000078_weighing_observations_drop_animal_id.sql (maintainer
  // decision 2026-08-03, following the 2026-07-31 decision that first made it
  // a permanently-NULL no-op): "not good enough that animal_id is dead but
  // harmless. Delete it." A later migration re-adding the column — via a fresh
  // CREATE TABLE, an ADD COLUMN, or any other DDL that leaves
  // weighing_observations.animal_id existing at the end of this file's Up
  // section — must fail the build.
  //
  // Gated to version > 78: migrations 000006/000007 (and any other pre-000078
  // file) legitimately CREATED and then nullable-ified this column before it
  // existed to drop, and must not be flagged for their own history. DROP
  // COLUMN is explicitly exempt within 000078 itself (that is the state this
  // rule protects), and 000078's own Down section (a documented, reviewed
  // rollback) is exempt because upSection() already strips Down.
  const versionMatch = /^(\d+)_/.exec(rel.split("/").pop() || "");
  const version = versionMatch ? parseInt(versionMatch[1], 10) : 0;
  if (version > 78) {
    const withoutDrops = up.replace(/DROP\s+COLUMN\s+(?:IF\s+EXISTS\s+)?animal_id\b/gi, "");
    const reintroducesColumn =
      /\bweighing_observations\b[\s\S]{0,2000}?\b(?:ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?animal_id\b|animal_id\s+uuid\b)/i.test(
        withoutDrops,
      );
    if (reintroducesColumn) {
      findings.push({
        rule: "observations-animal-id-column-reintroduced",
        message: `${rel}: Up section adds an animal_id column back onto weighing_observations — this column was deliberately dropped (000078_weighing_observations_drop_animal_id.sql) and must never exist on this table again`,
      });
    }
  }

  // Failure mode 12: weighing_expected_animals -- the second expected-set model
  // (the herd/goats-linked roster catalog) -- was DROPPED OUTRIGHT by
  // 000079_weighing_drop_expected_animals_table.sql (free-flow mandate,
  // 2026-08-03: "delete it entirely" so the second model can never be selected
  // again). Any LATER migration that CREATEs this table back must fail the
  // build. 000079's OWN Down section legitimately recreates the empty table
  // shape for rollback safety (schema only, no data — see that file's Down
  // header) and is exempt, exactly like mode 11's exemption for 000078's own
  // Down section.
  // Gated to version > 79 for the same reason mode 11 gates on > 78: migration
  // 000006 legitimately CREATED this table long before 000079 dropped it, and
  // must not be flagged for its own history.
  if (
    version > 79 &&
    /CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:public\.)?weighing_expected_animals\b/i.test(up)
  ) {
    findings.push({
      rule: "expected-animals-table-reintroduced",
      message: `${rel}: Up section CREATEs weighing_expected_animals — this table was DROPPED OUTRIGHT (000079_weighing_drop_expected_animals_table.sql, free-flow mandate) because it was the second expected-set/herd-roster model that kept reintroducing herd coupling. It must never exist again; weighing has no expected set by definition.`,
    });
  }
  return findings;
}

// Failure mode 13: weighing_expected_animals must not be referenced by any live
// (non-comment) Go code anywhere in the weighing tree — not a query, not a Go
// type, not a struct field, nothing. The table itself is gone
// (000079_weighing_drop_expected_animals_table.sql); a reference to it in code
// is either a compile-time-dead pointer at a nonexistent table or, worse, the
// first line of code trying to bring the second expected-set model back.
// Comments are explicitly allowed (and expected — the deletion is documented
// inline throughout the write path) via stripComments().
export function findingsForGoSourceExpectedAnimalsTableGone(rel, source) {
  const findings = [];
  const stripped = stripComments(source);
  const lines = stripped.split("\n");
  for (let i = 0; i < lines.length; i++) {
    if (/\bweighing_expected_animals\b/i.test(lines[i])) {
      findings.push({
        rule: "expected-animals-table-referenced",
        message: `${rel}:${i + 1}: references \`weighing_expected_animals\` in live code — this table was DROPPED OUTRIGHT (000079_weighing_drop_expected_animals_table.sql). Weighing has no expected set; this table must never be read, written, or joined again.`,
      });
    }
  }
  return findings;
}

// Failure mode 11b: no weighing Go source may select, filter, insert, or scan
// weighing_observations.animal_id, and no weighing struct may declare an
// AnimalID field carrying an `animal_id` JSON/db tag. The column does not
// exist (dropped by 000078_weighing_observations_drop_animal_id.sql); a
// caller/struct that still names it is either dead code pointing at a
// nonexistent column (Go compile failure) or, worse, a reintroduction of the
// exact coupling this guard exists to prevent.
//
// Scoped to weighing_observations specifically: weighing_expected_animals.
// animal_id is a DIFFERENT table (the planner/roster catalog's own join to
// goats for population counts and roster display, out of this guard's scope
// per 000078's own migration header) and must not be flagged here.
const OBSERVATIONS_ANIMAL_ID_STRUCT_FIELD_RE = /\bAnimalID\s+\*?string\s+`[^`]*json:"animal_id/;

// Structs that legitimately own weighing_expected_animals.animal_id -- the
// planner/roster catalog's own join to goats, a DIFFERENT table from
// weighing_observations and out of this rule's scope (see the migration
// header on 000078_weighing_observations_drop_animal_id.sql). Every other
// weighing struct is fair game: if a future struct needs a roster-identity
// field too, add it here deliberately rather than have this guard silently
// stop checking everything.
//
// ExpectedAnimal is the ONLY entry, kept for a narrower reason than the one
// above: it is retained purely for WIRE COMPATIBILITY on domain.RosterPage
// (RosterPage.Items []ExpectedAnimal is still serialized as `items: []` so an
// older client reading that key does not break), and nothing in the weighing
// module ever constructs a populated ExpectedAnimal or appends one to
// RosterPage.Items -- see mode 15 (roster-items-populated) below, which is
// the guard that actually enforces "always absent from responses." Do NOT
// add rosterCursor back here: it was the AnimalID-keyed roster cursor and was
// deleted outright, not retained (see mode 14, roster-cursor-type-reintroduced).
const OBSERVATIONS_ANIMAL_ID_FIELD_ALLOWED_STRUCTS = new Set(["ExpectedAnimal"]);

// Struct bodies, keyed by type name, via brace counting (Go struct field types
// can themselves contain braces -- map[string]struct{} -- so a non-greedy
// regex up to the first `}` is not safe here).
function structBodies(source) {
  const out = [];
  const re = /\btype\s+(\w+)\s+struct\s*\{/g;
  let m;
  while ((m = re.exec(source)) !== null) {
    let depth = 1;
    let i = m.index + m[0].length;
    while (i < source.length && depth > 0) {
      if (source[i] === "{") depth++;
      else if (source[i] === "}") depth--;
      i++;
    }
    out.push({ name: m[1], body: source.slice(m.index + m[0].length, i - 1) });
  }
  return out;
}

export function findingsForGoSourceObservationsAnimalId(rel, source) {
  const findings = [];
  const body = stripComments(source);
  // Scan each string-literal SQL block individually so the proximity window
  // stays tight to one query rather than spanning unrelated code between two
  // functions in the same file.
  const stringLiteralRe = /`([^`]*)`/g;
  let sm;
  while ((sm = stringLiteralRe.exec(body)) !== null) {
    const literal = sm[1];
    if (/\bweighing_observations\b/i.test(literal) && /\banimal_id\b/i.test(literal)) {
      findings.push({
        rule: "observations-animal-id-column-referenced",
        message: `${rel}: a SQL string references weighing_observations together with animal_id — that column was dropped (000078_weighing_observations_drop_animal_id.sql) and must not be selected, inserted, or filtered on`,
      });
      break;
    }
  }
  for (const { name, body: structBody } of structBodies(body)) {
    if (OBSERVATIONS_ANIMAL_ID_FIELD_ALLOWED_STRUCTS.has(name)) continue;
    if (OBSERVATIONS_ANIMAL_ID_STRUCT_FIELD_RE.test(structBody)) {
      findings.push({
        rule: "observations-animal-id-field-reintroduced",
        message: `${rel}: struct ${name} declares an AnimalID field with an "animal_id" tag — weighing_observations has no animal_id column and no weighing struct outside the roster catalog (${[...OBSERVATIONS_ANIMAL_ID_FIELD_ALLOWED_STRUCTS].join(", ")}) may carry one`,
      });
    }
  }
  return findings;
}

// Failure mode 14: rosterCursor (the herd-cursor carrier keyed on `animal_id`
// that used to page the dead expected-animal roster) was deleted outright
// (repository.go's rosterCursor type + encodeRosterCursor/decodeRosterCursor)
// once the `cursor`/`includeRoster` params it served were dropped from
// ListScopeRoster/ListScopeRosterForOperator. It must never come back — not
// as that type name, not as any struct field tagged `json:"animal_id"`
// outside the retained-for-wire-compatibility ExpectedAnimal type.
//
// Failure mode 15: nothing may populate domain.RosterPage.Items with anything
// other than an empty slice literal. RosterPage.Items and RosterPage.NextCursor
// stay ON THE WIRE for older-client compatibility (a breaking response-shape
// change is out of scope), but free-flow has no expected roster to serve, so
// the field must always come back empty. A composite literal that assigns
// Items: <anything but []domain.ExpectedAnimal{}/nil/make(...,0,...)/the
// repository's own always-empty `out` accumulator> is this guard's signal
// that someone is trying to resurrect the roster read. `out` is allowlisted
// by name because adapters/postgres/repository.go declares it as
// `out := make([]domain.ExpectedAnimal, 0)` and never appends to it -- if a
// future edit starts appending to `out`, that is caught by the postgres
// integration tests asserting Items stays empty, not by this textual guard.
const ROSTER_CURSOR_TYPE_RE = /\btype\s+rosterCursor\s+struct\b/;
const ROSTER_ITEMS_POPULATED_RE =
  /RosterPage\{[^}]*\bItems:(?!\s*(?:\[\]domain\.ExpectedAnimal\{\}|nil\b|out\b|make\(\s*\[\]domain\.ExpectedAnimal\s*,\s*0\)))/;

export function findingsForGoSourceRosterCursorAndItemsGone(rel, source) {
  const findings = [];
  const body = stripComments(source);
  if (ROSTER_CURSOR_TYPE_RE.test(body)) {
    findings.push({
      rule: "roster-cursor-type-reintroduced",
      message: `${rel}: declares \`type rosterCursor struct\` — this AnimalID-keyed cursor was deleted outright once the dead \`cursor\`/\`includeRoster\` roster params were dropped from ListScopeRoster. It must not come back.`,
    });
  }
  const rosterLiteralRe = /domain\.RosterPage\{[^}]*\}/g;
  let m;
  while ((m = rosterLiteralRe.exec(body)) !== null) {
    if (ROSTER_ITEMS_POPULATED_RE.test(m[0])) {
      findings.push({
        rule: "roster-items-populated",
        message: `${rel}: a domain.RosterPage{...} composite literal assigns Items to something other than an empty slice — free-flow has no expected roster to serve; RosterPage.Items must always stay empty (kept on the wire only for older-client compatibility).`,
      });
    }
  }
  return findings;
}

// Failure mode 8: a struct tag / validator-library call making AnimalID a
// required precondition, distinct from mode 4's hand-written `if AnimalID ==
// ""` shape. Scanned across the WHOLE weighing Go source (not just the
// extracted write-path function bodies), because command/DTO struct
// definitions usually live in a separate `domain` file from the repository
// method that uses them.
const FORBIDDEN_REQUIRE_ANIMAL_ID_RE =
  /AnimalID\s+\*?string\s+`[^`]*\bvalidate:"[^"]*\brequired\b[^"]*"[^`]*`|validator\.(?:Var|Struct)\([^)]*AnimalID[^)]*"[^"]*\brequired\b|require(?:d)?\.(?:NotEmpty|NotZero)\([^)]*\.?AnimalID\b/;

export function findingsForGoSourceStructTags(rel, source) {
  const findings = [];
  const body = stripComments(source);
  if (FORBIDDEN_REQUIRE_ANIMAL_ID_RE.test(body)) {
    findings.push({
      rule: "animal-id-required-precondition",
      message: `${rel}: a struct tag or validator call makes AnimalID a required/non-empty precondition — weighing free-flow must accept an observation with no resolved animal_id at all (maintainer decision 2026-07-31)`,
    });
  }
  return findings;
}

// Failure mode 9: the Android weighing WRITE REQUEST DTO must not carry an
// animal_id field. Scoped deliberately narrow: only a Kotlin `data class`
// whose name matches `Weighing...RequestDto` (the write-request naming
// convention already used by `WeighingAnimalObservationRequestDto` /
// `WeighingShedObservationRequestDto` / `WeighingScopeSubmitRequestDto`), so
// this does NOT fire on `WeighingObservationDto` (the read/response shape,
// which legitimately carries a nullable `animal_id` resolved server-side),
// on `WeighingRosterRowDto` (a read DTO), on a Room `@Entity` (a different
// annotation/file shape entirely), or on any Vaccination DTO (different name
// prefix). See the header blind-spot note for what this narrow scope misses.
const WEIGHING_REQUEST_DTO_CLASS_RE = /data class (Weighing\w*RequestDto)\b\s*\(/g;
const ANIMAL_ID_FIELD_RE = /@SerialName\("animal_id"\)|\bval\s+animalId\b/;

function extractKotlinClassCtorBody(source, openParenIndex) {
  // openParenIndex points at the `(` immediately after the class name. Walk
  // forward counting paren depth (ignoring parens inside string literals is
  // unnecessary here: DTO constructors do not embed string literals containing
  // parens) to find the matching close paren.
  let depth = 0;
  for (let i = openParenIndex; i < source.length; i++) {
    const ch = source[i];
    if (ch === "(") depth++;
    else if (ch === ")") {
      depth--;
      if (depth === 0) return source.slice(openParenIndex + 1, i);
    }
  }
  return source.slice(openParenIndex + 1);
}

export function findingsForKotlinWeighingRequestDto(rel, source) {
  const findings = [];
  let m;
  WEIGHING_REQUEST_DTO_CLASS_RE.lastIndex = 0;
  while ((m = WEIGHING_REQUEST_DTO_CLASS_RE.exec(source)) !== null) {
    const className = m[1];
    const ctorBody = extractKotlinClassCtorBody(source, m.index + m[0].length - 1);
    if (ANIMAL_ID_FIELD_RE.test(ctorBody)) {
      findings.push({
        rule: "android-write-request-carries-animal-id",
        message: `${rel}: ${className} declares an animal_id/animalId field — the Android weighing WRITE REQUEST must send only scanned_identifier (plus weight/proof/location); animal_id resolution is backend-only`,
      });
    }
  }
  return findings;
}

// Failure mode 10: weighing UI surfaces must not render/compute a ratio
// against an expected/roster denominator. Deliberately scoped to weighing UI
// paths only (Android Compose weighing screens, admin-web weighing pages).
const FORBIDDEN_NN_LITERAL_RE = /\bN\s*\/\s*N\b/;
const FORBIDDEN_SLASH_100_RE = /\/\s*100\b(?!\s*[%.\w])|["'`]\s*\/\s*100\b/;
// The precise, low-false-positive shape: a division where the RIGHT-HAND side
// token contains "expected" (covers `x / totalExpected`, `x/expectedCount`,
// `{completedCount} / {row.expectedCount}` in JSX/TSX, and the Kotlin
// `individualResolved.toFloat() / totalExpected.toFloat()` shape).
const FORBIDDEN_EXPECTED_DENOMINATOR_RE = /\/\s*[{$]*\s*\w*\.?\w*[Ee]xpected\w*/;

export function findingsForWeighingUiSource(rel, source) {
  const findings = [];
  const body = stripComments(source);
  if (FORBIDDEN_NN_LITERAL_RE.test(body)) {
    findings.push({
      rule: "expected-denominator-progress-in-ui",
      message: `${rel}: a literal N/N progress token — weighing has no expected-animal denominator; render a plain count instead`,
    });
  }
  if (FORBIDDEN_SLASH_100_RE.test(body)) {
    findings.push({
      rule: "expected-denominator-progress-in-ui",
      message: `${rel}: a literal /100-style progress token — weighing has no fixed expected total; render a plain count instead`,
    });
  }
  if (FORBIDDEN_EXPECTED_DENOMINATOR_RE.test(body)) {
    findings.push({
      rule: "expected-denominator-progress-in-ui",
      message: `${rel}: a ratio computed/rendered against an expected/roster-named denominator — weighing progress must be reported as counts only, never as a fraction of an expected/roster count (maintainer decision 2026-07-31)`,
    });
  }
  return findings;
}

function isWeighingAndroidKotlin(rel) {
  return rel.startsWith("apps/goatos-android/") && rel.endsWith(".kt") && !rel.includes("/build/");
}

function isWeighingUiSurface(rel) {
  if (rel.includes("/build/") || rel.includes("/.next/")) return false;
  const isAndroidWeighingScreen =
    rel.startsWith("apps/goatos-android/") && /\/feature\/weighing\//.test(rel) && rel.endsWith(".kt");
  const isAdminWebWeighingPage =
    (rel.startsWith("apps/admin-web/features/weighing/") || rel.startsWith("apps/admin-web/app/(admin)/weighing/")) &&
    (rel.endsWith(".tsx") || rel.endsWith(".ts"));
  return isAndroidWeighingScreen || isAdminWebWeighingPage;
}

function isWeighingGo(rel) {
  return rel.startsWith(`${WEIGHING_DIR}/`) && rel.endsWith(".go");
}

function isWeighingMigration(rel) {
  return rel.startsWith(`${MIGRATIONS_DIR}/`) && /weighing/i.test(rel) && rel.endsWith(".sql");
}

// Generated/vendored trees are never source. Skipping them is not just a speed win: `.next` is
// WRITTEN by the admin-web build job, which ci-local runs CONCURRENTLY with the guards, so walking
// it raced a live build and crashed the guard with
// ENOENT scandir apps/admin-web/.next/standalone/... — a directory that existed when readdir listed
// the parent and was gone microseconds later. A guard that dies on someone else's build output
// reports RED for a reason that has nothing to do with the diff.
const SKIP_DIRS = new Set([
  ".next",
  "node_modules",
  ".git",
  ".gradle",
  "build",
  "dist",
  "out",
  ".code-review-graph",
  "graphify-out",
]);

function walk(dir, matcher, out) {
  if (!existsSync(dir)) return out;
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch (error) {
    // Belt and braces for the same race on any tree we do still walk: a directory removed between
    // the parent listing and this readdir is not a guard failure.
    if (error && (error.code === "ENOENT" || error.code === "ENOTDIR")) return out;
    throw error;
  }
  for (const entry of entries) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (SKIP_DIRS.has(entry.name)) continue;
      walk(path, matcher, out);
    } else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (matcher(rel)) out.push(rel);
    }
  }
  return out;
}

const ANDROID_DIR = "apps/goatos-android";
const ADMIN_WEB_DIR = "apps/admin-web";

function run() {
  const goFiles = walk(resolve(repo, WEIGHING_DIR), isWeighingGo, []);
  const migrationFiles = walk(resolve(repo, MIGRATIONS_DIR), isWeighingMigration, []);
  const androidKotlinFiles = walk(resolve(repo, ANDROID_DIR), isWeighingAndroidKotlin, []);
  const uiFiles = [
    ...walk(resolve(repo, ANDROID_DIR), isWeighingUiSurface, []),
    ...walk(resolve(repo, ADMIN_WEB_DIR), isWeighingUiSurface, []),
  ];
  const problems = [];
  for (const rel of goFiles) {
    const source = readFileSync(resolve(repo, rel), "utf8");
    for (const f of findingsForGoSource(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
    for (const f of findingsForGoSourceStructTags(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
    for (const f of findingsForGoSourceObservationsAnimalId(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
    for (const f of findingsForGoSourceExpectedAnimalsTableGone(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
    for (const f of findingsForGoSourceRosterCursorAndItemsGone(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
  }
  for (const rel of migrationFiles) {
    const source = readFileSync(resolve(repo, rel), "utf8");
    for (const f of findingsForMigrationSource(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
  }
  for (const rel of androidKotlinFiles) {
    const source = readFileSync(resolve(repo, rel), "utf8");
    for (const f of findingsForKotlinWeighingRequestDto(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
  }
  for (const rel of uiFiles) {
    const source = readFileSync(resolve(repo, rel), "utf8");
    for (const f of findingsForWeighingUiSource(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
  }
  if (problems.length > 0) {
    console.error("weighing-free-flow guard failed:");
    for (const p of problems) console.error(`- ${p}`);
    process.exit(1);
  }
  console.log(
    `weighing-free-flow guard: ok (${goFiles.length} weighing Go files, ${migrationFiles.length} weighing migrations, ${androidKotlinFiles.length} Android Kotlin files scanned for write-request DTOs, ${uiFiles.length} weighing UI files)`,
  );
}

function selfTest() {
  // Mode 1: vaccination/herd-roster read in write path.
  const badFn1 = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  rows, err := tx.Query(ctx, "SELECT 1 FROM vaccination_completions WHERE goat_id=$1", cmd.AnimalID)
  return domain.Observation{}, nil
}
`;
  const goodFn1 = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  rows, err := tx.Query(ctx, "SELECT 1 FROM goats g WHERE g.goat_id=$1", cmd.AnimalID)
  return domain.Observation{}, nil
}
`;
  const badFn1b = `
func (r *Repository) classifyAnimalObservationRejection(ctx context.Context, tx pgx.Tx, cmd domain.RecordAnimalObservation) error {
  if err := RequireShedOwnership(ctx, tx, cmd.AnimalID); err != nil { return err }
  return nil
}
`;

  // Mode 4: reject on null animal_id without routing through the unknown-animal path.
  const badFn4 = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  if cmd.AnimalID == "" {
    return domain.Observation{}, ports.ErrInvalidArgument
  }
  return domain.Observation{}, nil
}
`;
  const goodFn4 = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  if !uuidutil.IsUUIDString(cmd.AnimalID) {
    return r.recordUnknownAnimalObservationTx(ctx, tx, cmd)
  }
  return domain.Observation{}, nil
}
`;

  // Mode 5: clinical-state read on the write path. badFn5 is the REAL gate that
  // shipped and that this guard previously failed to catch, reproduced verbatim in
  // shape: a `goats` join plus health/lifecycle predicates bound from the shared
  // clinical-defer vocabularies.
  const badFn5 = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  err = tx.QueryRow(ctx, \`
  WITH expected AS (
    SELECT ea.campaign_shed_id
    FROM goats g
    JOIN weighing_expected_animals ea ON ea.animal_id=g.goat_id
    WHERE COALESCE(g.health_status, 'healthy') <> ALL($11::text[])
      AND COALESCE(g.lifecycle_status, '') <> ALL($12::text[])
  )
  SELECT campaign_shed_id FROM expected\`,
    protocoldomain.MandatoryClinicalDeferStates, protocoldomain.ExitLifecycleStates).Scan(&out)
  return domain.Observation{}, err
}
`;
  // The Go-side variant: no SQL, just the classifier branch that returned a
  // refusal sentinel for a clinically held animal.
  const badFn5b = `
func (r *Repository) classifyAnimalObservationRejection(ctx context.Context, tx pgx.Tx, cmd domain.RecordAnimalObservation) error {
  var blocked bool
  tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM goats WHERE health_status = ANY($1::text[]))", protocoldomain.MandatoryClinicalDeferStates).Scan(&blocked)
  if blocked {
    return ports.ErrAnimalUnavailable
  }
  return ports.ErrNotFound
}
`;
  // GOOD: the write path may still join goats for LOCATION labelling. Only
  // clinical/lifecycle gating is banned, so this must NOT be flagged — otherwise
  // the guard would be unusable against the real code.
  const goodFn5 = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  err = tx.QueryRow(ctx, \`
  SELECT COALESCE(NULLIF($8, '')::uuid, g.current_location_id)
  FROM goats g WHERE g.tenant_id=$1::uuid AND g.goat_id=$3::uuid\`).Scan(&out)
  return domain.Observation{}, err
}
`;

  // Mode 6: roster status/availability gate — the exact join that shipped.
  const badFn6 = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  err = tx.QueryRow(ctx, \`
  SELECT ea.campaign_shed_id FROM goats g
  JOIN weighing_expected_animals ea ON ea.animal_id=g.goat_id
   AND ea.status <> 'unavailable'
   AND ea.availability_status NOT IN ('icu','quarantine','dead')\`).Scan(&out)
  return domain.Observation{}, err
}
`;
  // GOOD: a COMMENT that quotes the banned predicates (e.g. explaining why they were
  // removed) must not trip the guard — otherwise documenting the ban breaks the build.
  const goodFn6c = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  // This join used to carry:
  //   AND ea.status <> 'unavailable'
  //   AND ea.availability_status NOT IN ('icu','quarantine','dead')
  // and also read COALESCE(g.health_status,'healthy') <> ALL(...). All removed.
  return domain.Observation{}, nil
}
`;
  // GOOD: writing progress BACK to the roster is allowed; only gating is banned.
  const goodFn6b = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  tx.Exec(ctx, \`UPDATE weighing_expected_animals SET status='weighed', availability_status=CASE WHEN $4::uuid = expected_location_id THEN 'expected_shed' ELSE 'moved_other_shed' END\`)
  return domain.Observation{}, nil
}
`;
  // GOOD: roster LEFT JOINed for labels only, no status predicate.
  const goodFn6 = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  err = tx.QueryRow(ctx, \`
  SELECT ea.expected_location_id, ea.expected_location_label FROM goats g
  LEFT JOIN weighing_expected_animals ea ON ea.animal_id=g.goat_id\`).Scan(&out)
  return domain.Observation{}, err
}
`;

  // Mode 7: STRICT allowlist. Any non-weighing table on the write path fails, even
  // one nobody has thought of yet — that is the point of an allowlist.
  const badFn7goats = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  tx.QueryRow(ctx, "SELECT g.current_location_id FROM goats g WHERE g.goat_id=$1").Scan(&out)
  return domain.Observation{}, nil
}
`;
  const badFn7roster = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  tx.QueryRow(ctx, "SELECT expected_location_id FROM weighing_expected_animals WHERE animal_id=$1").Scan(&out)
  return domain.Observation{}, nil
}
`;
  // A table nobody has banned by name yet must STILL fail under the allowlist.
  const badFn7novel = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  tx.QueryRow(ctx, "SELECT 1 FROM herd_register_snapshots WHERE goat_id=$1").Scan(&out)
  return domain.Observation{}, nil
}
`;
  // GOOD: only weighing-owned tables + proof, with CTE names used as FROM targets.
  const goodFn7 = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  tx.QueryRow(ctx, \`
  WITH campaign AS (SELECT campaign_id FROM weighing_campaigns WHERE tenant_id=$1),
  assigned_shed AS (SELECT campaign_shed_id FROM weighing_campaign_sheds WHERE tenant_id=$1),
  proof_ok AS (SELECT proof_id FROM proof_artifacts WHERE proof_id=$5)
  INSERT INTO weighing_observations (tenant_id) SELECT $1 FROM campaign c JOIN assigned_shed s ON true JOIN proof_ok p ON true\`).Scan(&out)
  return domain.Observation{}, nil
}
`;

  // REGRESSION: two bypasses an adversarial review actually demonstrated against an
  // earlier version of this guard. Both must stay caught.
  //   (a) the gate extracted into a helper that is not in WRITE_FN_NAMES;
  //   (b) `//` inside a Go string literal (a URL) truncating the line and deleting
  //       the banned predicate before the regex saw it.
  const bypassHelper = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  if err := r.checkClinicalEligibility(ctx, tx, cmd); err != nil { return domain.Observation{}, err }
  return domain.Observation{}, nil
}

func (r *Repository) checkClinicalEligibility(ctx context.Context, tx pgx.Tx, cmd domain.RecordAnimalObservation) error {
  tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM goats WHERE health_status = ANY($1::text[]))").Scan(&blocked)
  return nil
}
`;
  if (!findingsForGoSource("fake.go", bypassHelper).length) {
    throw new Error("self-test failed: gate hidden in an unlisted helper was not caught");
  }
  const bypassUrlComment = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  marker := "note http://example.com" + " AND COALESCE(g.health_status,'healthy') <> ALL($11::text[])"
  return domain.Observation{}, nil
}
`;
  if (!findingsForGoSource("fake.go", bypassUrlComment).some((f) => f.rule === "clinical-state-read-in-write-path")) {
    throw new Error("self-test failed: banned predicate after a URL in a string literal was not caught");
  }

  // REGRESSION: CTE self-shadowing, demonstrated by an adversarial review.
  const bypassCteShadow = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  tx.QueryRow(ctx, "WITH weighing_expected_animals AS (SELECT animal_id FROM weighing_expected_animals WHERE status <> 'unavailable') SELECT * FROM weighing_expected_animals").Scan(&out)
  return domain.Observation{}, nil
}
`;
  if (!findingsForGoSource("fake.go", bypassCteShadow).some((f) => f.rule === "cte-shadows-banned-table")) {
    throw new Error("self-test failed: a CTE shadowing a banned table was not caught");
  }
  const bypassCteShadowGoats = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  tx.QueryRow(ctx, "WITH goats AS (SELECT goat_id FROM goats) SELECT * FROM goats").Scan(&out)
  return domain.Observation{}, nil
}
`;
  if (!findingsForGoSource("fake.go", bypassCteShadowGoats).some((f) => f.rule === "cte-shadows-banned-table")) {
    throw new Error("self-test failed: a CTE shadowing goats was not caught");
  }

  const findings1 = findingsForGoSource("fake.go", badFn1);
  if (!findings1.some((f) => f.rule === "vaccination-or-herd-roster-read-in-write-path")) {
    throw new Error(`self-test failed: mode 1 (table) not flagged. got: ${JSON.stringify(findings1)}`);
  }
  for (const [label, src] of [["goats", badFn7goats], ["roster", badFn7roster], ["novel table", badFn7novel]]) {
    const f = findingsForGoSource("fake.go", src);
    if (!f.some((x) => x.rule === "write-path-table-not-allowlisted")) {
      throw new Error(`self-test failed: mode 7 did not flag ${label}. got: ${JSON.stringify(f)}`);
    }
  }
  const findings7good = findingsForGoSource("fake.go", goodFn7);
  if (findings7good.length) {
    throw new Error(`self-test failed: mode 7 false positive on an allowlisted weighing-only write. got: ${JSON.stringify(findings7good)}`);
  }
  const findings6 = findingsForGoSource("fake.go", badFn6);
  if (!findings6.some((f) => f.rule === "roster-state-gate-in-write-path")) {
    throw new Error(`self-test failed: mode 6 (roster state gate) not flagged. got: ${JSON.stringify(findings6)}`);
  }
  const findings6goodC = findingsForGoSource("fake.go", goodFn6c);
  if (findings6goodC.length) {
    throw new Error(`self-test failed: comment-only mention of the banned predicates was flagged. got: ${JSON.stringify(findings6goodC)}`);
  }
  const findings6goodB = findingsForGoSource("fake.go", goodFn6b);
  if (findings6goodB.some((f) => f.rule === "roster-state-gate-in-write-path")) {
    throw new Error(`self-test failed: mode 6 false positive on an allowed roster write-back. got: ${JSON.stringify(findings6goodB)}`);
  }
  const findings6good = findingsForGoSource("fake.go", goodFn6);
  if (findings6good.some((f) => f.rule === "roster-state-gate-in-write-path")) {
    throw new Error(`self-test failed: mode 6 false positive on a label-only roster LEFT JOIN. got: ${JSON.stringify(findings6good)}`);
  }
  const findings5 = findingsForGoSource("fake.go", badFn5);
  if (!findings5.some((f) => f.rule === "clinical-state-read-in-write-path")) {
    throw new Error(`self-test failed: mode 5 (SQL clinical gate) not flagged. got: ${JSON.stringify(findings5)}`);
  }
  const findings5b = findingsForGoSource("fake.go", badFn5b);
  if (!findings5b.some((f) => f.rule === "clinical-state-read-in-write-path")) {
    throw new Error(`self-test failed: mode 5b (Go clinical branch) not flagged. got: ${JSON.stringify(findings5b)}`);
  }
  const findings5good = findingsForGoSource("fake.go", goodFn5);
  if (findings5good.some((f) => f.rule === "clinical-state-read-in-write-path")) {
    throw new Error(`self-test failed: mode 5 false positive on a location-only goats join. got: ${JSON.stringify(findings5good)}`);
  }
  const findings1b = findingsForGoSource("fake.go", badFn1b);
  if (!findings1b.some((f) => f.rule === "vaccination-or-herd-roster-read-in-write-path")) {
    throw new Error(`self-test failed: mode 1 (roster fn) not flagged. got: ${JSON.stringify(findings1b)}`);
  }
  // goodFn1 is a bare `goats` join with no vaccination/roster/clinical predicate. It
  // must not trip modes 1/5/6 — but under the STRICT allowlist (mode 7) touching
  // `goats` at all on the write path is now itself a finding, which is the intended
  // behaviour, so this assertion is scoped to the pattern rules.
  if (findingsForGoSource("fake.go", goodFn1).some((f) => f.rule !== "write-path-table-not-allowlisted")) {
    throw new Error("self-test failed: mode 1 false positive on clean goats-only join");
  }

  const findings4 = findingsForGoSource("fake.go", badFn4);
  if (!findings4.some((f) => f.rule === "reject-null-animal-id")) {
    throw new Error(`self-test failed: mode 4 not flagged. got: ${JSON.stringify(findings4)}`);
  }
  if (findingsForGoSource("fake.go", goodFn4).length !== 0) {
    throw new Error("self-test failed: mode 4 false positive on free-flow unknown-animal routing");
  }

  // Mode 2: NOT NULL re-add.
  const badMig2 = `
-- +goose Up
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id SET NOT NULL;
-- +goose Down
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id DROP NOT NULL;
`;
  const goodMig2 = `
-- +goose Up
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id DROP NOT NULL;
-- +goose Down
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id SET NOT NULL;
`;
  const findings2 = findingsForMigrationSource("fake.sql", badMig2);
  if (!findings2.some((f) => f.rule === "animal-id-not-null-reintroduced")) {
    throw new Error(`self-test failed: mode 2 not flagged. got: ${JSON.stringify(findings2)}`);
  }
  if (findingsForMigrationSource("fake.sql", goodMig2).length !== 0) {
    throw new Error("self-test failed: mode 2 false positive on legitimate Down-section rollback");
  }

  // Mode 3: unique constraint collapsing buckets.
  const badMig3 = `
-- +goose Up
CREATE UNIQUE INDEX weighing_observations_scanned_identifier_idx ON public.weighing_observations (tenant_id, scanned_identifier);
`;
  const goodMig3 = `
-- +goose Up
CREATE UNIQUE INDEX weighing_observations_scanned_identifier_idx ON public.weighing_observations (tenant_id, campaign_shed_id, scanned_identifier);
`;
  const findings3 = findingsForMigrationSource("fake.sql", badMig3);
  if (!findings3.some((f) => f.rule === "scanned-identifier-unique-across-buckets")) {
    throw new Error(`self-test failed: mode 3 not flagged. got: ${JSON.stringify(findings3)}`);
  }
  if (findingsForMigrationSource("fake.sql", goodMig3).length !== 0) {
    throw new Error("self-test failed: mode 3 false positive on bucket-scoped unique index");
  }

  // Mode 8: AnimalID required via a struct tag / validator call.
  const badStruct8 = `
package domain

type RecordAnimalObservation struct {
  AnimalID string ` + "`json:\"animal_id\" validate:\"required\"`" + `
  ScannedIdentifier string
}
`;
  const goodStruct8 = `
package domain

type RecordAnimalObservation struct {
  AnimalID string ` + "`json:\"animal_id\"`" + `
  ScannedIdentifier string ` + "`json:\"scanned_identifier\" validate:\"required\"`" + `
}
`;
  const findings8 = findingsForGoSourceStructTags("fake.go", badStruct8);
  if (!findings8.some((f) => f.rule === "animal-id-required-precondition")) {
    throw new Error(`self-test failed: mode 8 not flagged. got: ${JSON.stringify(findings8)}`);
  }
  if (findingsForGoSourceStructTags("fake.go", goodStruct8).length) {
    throw new Error("self-test failed: mode 8 false positive on a plain AnimalID tag with required only on ScannedIdentifier");
  }
  if (findingsForGoSourceStructTags("fake.go", goodStruct8).some((f) => f.rule === "animal-id-required-precondition")) {
    throw new Error("self-test failed: mode 8 false positive on good struct");
  }

  // Mode 9: Android weighing WRITE REQUEST DTO carrying animal_id.
  const badDto9 = `
package sg.mesha.goatos.core.network.dto

@Serializable
data class WeighingAnimalObservationRequestDto(
    @SerialName("campaign_shed_id") val campaignShedId: String,
    @SerialName("animal_id") val animalId: String,
    @SerialName("scanned_identifier") val scannedIdentifier: String,
    @SerialName("weight_kg") val weightKg: Double,
)
`;
  const findings9 = findingsForKotlinWeighingRequestDto("fake.kt", badDto9);
  if (!findings9.some((f) => f.rule === "android-write-request-carries-animal-id")) {
    throw new Error(`self-test failed: mode 9 not flagged. got: ${JSON.stringify(findings9)}`);
  }
  // GOOD: the real write-request shape (no animal_id).
  const goodDto9 = `
package sg.mesha.goatos.core.network.dto

@Serializable
data class WeighingAnimalObservationRequestDto(
    @SerialName("campaign_shed_id") val campaignShedId: String,
    @SerialName("scanned_identifier") val scannedIdentifier: String,
    @SerialName("weight_kg") val weightKg: Double,
    @SerialName("proof_artifact_id") val proofArtifactId: String,
)
`;
  if (findingsForKotlinWeighingRequestDto("fake.kt", goodDto9).length) {
    throw new Error("self-test failed: mode 9 false positive on the real free-flow write request (no animal_id)");
  }
  // GOOD: the READ/response DTO legitimately carries a resolved animal_id — must NOT fire.
  const goodResponseDto9 = `
@Serializable
data class WeighingObservationDto(
    @SerialName("observation_id") val observationId: String = "",
    @SerialName("animal_id") val animalId: String? = null,
)
`;
  if (findingsForKotlinWeighingRequestDto("fake.kt", goodResponseDto9).length) {
    throw new Error("self-test failed: mode 9 false positive on the read/response DTO (class name does not end in RequestDto)");
  }
  // GOOD: a Vaccination request DTO with animal_id must NOT fire (different prefix entirely, but
  // prove the class-name anchor actually requires the Weighing prefix).
  const goodVaccinationDto9 = `
@Serializable
data class VaccinationCompletionRequestDto(
    @SerialName("animal_id") val animalId: String,
)
`;
  if (findingsForKotlinWeighingRequestDto("fake.kt", goodVaccinationDto9).length) {
    throw new Error("self-test failed: mode 9 false positive on a non-Weighing (Vaccination) request DTO");
  }

  // Mode 10: expected-denominator progress in weighing UI.
  const badKotlinUi10 = `
val progress: Float get() = when {
  isShedPartition -> if (shedCompleted > 0) 1f else 0f
  visibleRows.isNotEmpty() -> individualCompleted.toFloat() / visibleRows.size.toFloat()
  totalExpected <= 0 -> 0f
  else -> individualResolved.toFloat() / totalExpected.toFloat()
}
`;
  const findings10a = findingsForWeighingUiSource("apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/feature/weighing/WeighingScreen.kt", badKotlinUi10);
  if (!findings10a.some((f) => f.rule === "expected-denominator-progress-in-ui")) {
    throw new Error(`self-test failed: mode 10 (Kotlin expected ratio) not flagged. got: ${JSON.stringify(findings10a)}`);
  }
  const badTsxUi10 = `
<div className="v">{campaign.individualCompleted}<span>/{campaign.individualExpected}</span></div>
`;
  const findings10b = findingsForWeighingUiSource("apps/admin-web/features/weighing/page.tsx", badTsxUi10);
  if (!findings10b.some((f) => f.rule === "expected-denominator-progress-in-ui")) {
    throw new Error(`self-test failed: mode 10 (TSX expected ratio) not flagged. got: ${JSON.stringify(findings10b)}`);
  }
  const badLiteral10 = `const label = complete ? "100/100" : "N/N";`;
  const findings10c = findingsForWeighingUiSource("apps/admin-web/features/weighing/page.tsx", badLiteral10);
  if (!findings10c.some((f) => f.rule === "expected-denominator-progress-in-ui")) {
    throw new Error(`self-test failed: mode 10 (literal N/N and /100) not flagged. got: ${JSON.stringify(findings10c)}`);
  }
  // GOOD: a plain count-only subtitle, ignoring an unused expected parameter — must NOT fire.
  const goodKotlinUi10 = `
private fun rosterSheetSubtitle(visibleCount: Int, totalExpected: Int): String =
    "$visibleCount captured rows"
`;
  if (findingsForWeighingUiSource("apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/feature/weighing/WeighingScreen.kt", goodKotlinUi10).length) {
    throw new Error("self-test failed: mode 10 false positive on a plain count-only subtitle");
  }
  // GOOD: an ordinary division that has nothing to do with an expected/roster count.
  const goodDivisionUi10 = `val perAnimalKg = totalWeightKg / animalCount`;
  if (findingsForWeighingUiSource("apps/admin-web/features/weighing/page.tsx", goodDivisionUi10).length) {
    throw new Error("self-test failed: mode 10 false positive on an unrelated division");
  }

  // Mode 11: weighing_observations.animal_id must never come back.
  //
  // 11a: a migration AFTER 000078 re-adding the column.
  const badMigration11a = `-- +goose Up
ALTER TABLE public.weighing_observations
  ADD COLUMN IF NOT EXISTS animal_id uuid REFERENCES public.goats(goat_id);
-- +goose Down
ALTER TABLE public.weighing_observations DROP COLUMN IF EXISTS animal_id;
`;
  const findings11a = findingsForMigrationSource(
    "000079_reintroduce_animal_id.sql",
    badMigration11a,
  );
  if (!findings11a.some((f) => f.rule === "observations-animal-id-column-reintroduced")) {
    throw new Error(`self-test failed: mode 11a (migration re-adds column) not flagged. got: ${JSON.stringify(findings11a)}`);
  }
  // GOOD: 000078 itself dropping the column must NOT fire (it is the migration this
  // rule protects, and its Down section legitimately re-adds the column as a
  // documented rollback).
  const goodMigration11a = `-- +goose Up
ALTER TABLE public.weighing_observations DROP COLUMN IF EXISTS animal_id;
-- +goose Down
ALTER TABLE public.weighing_observations
  ADD COLUMN IF NOT EXISTS animal_id uuid REFERENCES public.goats(goat_id);
`;
  if (
    findingsForMigrationSource("000078_weighing_observations_drop_animal_id.sql", goodMigration11a).some(
      (f) => f.rule === "observations-animal-id-column-reintroduced",
    )
  ) {
    throw new Error("self-test failed: mode 11a false positive on 000078's own drop-then-rollback-in-Down shape");
  }
  // GOOD: 000006/000007 (pre-000078 history) creating/nullable-ifying the column
  // must NOT fire -- that history is not a reintroduction.
  const goodMigration11aHistory = `-- +goose Up
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id DROP NOT NULL;
-- +goose Down
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id SET NOT NULL;
`;
  if (
    findingsForMigrationSource("000007_weighing_free_flow_scanned_identifier.sql", goodMigration11aHistory).some(
      (f) => f.rule === "observations-animal-id-column-reintroduced",
    )
  ) {
    throw new Error("self-test failed: mode 11a false positive on pre-000078 history");
  }

  // 11b: a weighing SQL string still referencing weighing_observations.animal_id.
  const badGoSource11b = `
func (r *Repository) getObservation(ctx context.Context, tx pgx.Tx, id string) (domain.Observation, error) {
  return tx.QueryRow(ctx, ` +
    "`SELECT observation_id, animal_id FROM weighing_observations WHERE observation_id=$1`" +
    `, id)
}
`;
  const findings11b = findingsForGoSourceObservationsAnimalId("fake.go", badGoSource11b);
  if (!findings11b.some((f) => f.rule === "observations-animal-id-column-referenced")) {
    throw new Error(`self-test failed: mode 11b (SQL string) not flagged. got: ${JSON.stringify(findings11b)}`);
  }
  // GOOD: weighing_expected_animals.animal_id (the roster catalog's own table) must NOT fire.
  const goodGoSource11b = `
func (r *Repository) rosterRow(ctx context.Context, tx pgx.Tx) error {
  _, err := tx.Query(ctx, ` +
    "`SELECT animal_id FROM weighing_expected_animals WHERE campaign_id=$1`" +
    `)
  return err
}
`;
  if (findingsForGoSourceObservationsAnimalId("fake.go", goodGoSource11b).length) {
    throw new Error("self-test failed: mode 11b false positive on weighing_expected_animals.animal_id");
  }

  // 11c: a weighing struct declaring an AnimalID field tagged animal_id.
  const badStruct11c = `
type Observation struct {
	ObservationID string ` + "`json:\"observation_id\"`" + `
	AnimalID      string ` + "`json:\"animal_id,omitempty\"`" + `
}
`;
  const findings11c = findingsForGoSourceObservationsAnimalId("fake.go", badStruct11c);
  if (!findings11c.some((f) => f.rule === "observations-animal-id-field-reintroduced")) {
    throw new Error(`self-test failed: mode 11c (struct field) not flagged. got: ${JSON.stringify(findings11c)}`);
  }
  // GOOD: ExpectedAnimal (the roster catalog struct) keeping AnimalID must NOT fire.
  const goodStruct11c = `
type ExpectedAnimal struct {
	AnimalID string ` + "`json:\"animal_id\"`" + `
}
`;
  if (findingsForGoSourceObservationsAnimalId("fake.go", goodStruct11c).length) {
    throw new Error("self-test failed: mode 11c false positive on the allowed ExpectedAnimal roster struct");
  }

  // 12: a migration (other than 000079's own Down-exempt Up section) CREATEs
  // weighing_expected_animals back.
  const badMigration12 = `-- +goose Up
CREATE TABLE IF NOT EXISTS public.weighing_expected_animals (
  campaign_id uuid NOT NULL,
  animal_id uuid
);
-- +goose Down
DROP TABLE IF EXISTS public.weighing_expected_animals;
`;
  const findings12 = findingsForMigrationSource("000080_reintroduce.sql", badMigration12);
  if (!findings12.some((f) => f.rule === "expected-animals-table-reintroduced")) {
    throw new Error(`self-test failed: mode 12 not flagged. got: ${JSON.stringify(findings12)}`);
  }
  // GOOD: 000079's own file recreates the table ONLY in its Down section (schema-only
  // rollback), and upSection() strips Down before this rule ever sees it, so it must not fire.
  const goodMigration12 = `-- +goose Up
DROP TABLE IF EXISTS public.weighing_expected_animals;
-- +goose Down
CREATE TABLE IF NOT EXISTS public.weighing_expected_animals (
  campaign_id uuid NOT NULL,
  animal_id uuid
);
`;
  if (
    findingsForMigrationSource("000079_weighing_drop_expected_animals_table.sql", goodMigration12).some(
      (f) => f.rule === "expected-animals-table-reintroduced",
    )
  ) {
    throw new Error("self-test failed: mode 12 false positive on 000079's own Up/Down shape");
  }

  // 13: any live (non-comment) Go reference to weighing_expected_animals anywhere in
  // the weighing tree, not just the write path — the table does not exist at all.
  const badGo13 = `
func (r *Repository) leftoverRosterRead(ctx context.Context, tenantID string) error {
	_, err := r.pool.Query(ctx, "SELECT animal_id FROM weighing_expected_animals WHERE tenant_id=$1", tenantID)
	return err
}
`;
  const findings13 = findingsForGoSourceExpectedAnimalsTableGone("fake.go", badGo13);
  if (!findings13.some((f) => f.rule === "expected-animals-table-referenced")) {
    throw new Error(`self-test failed: mode 13 not flagged. got: ${JSON.stringify(findings13)}`);
  }
  // GOOD: a comment documenting the deletion must not fire.
  const goodGo13 = `
// weighing_expected_animals was DROPPED (migration 000079); there is no
// roster table left for this write to touch.
func (r *Repository) noop(ctx context.Context) error { return nil }
`;
  if (findingsForGoSourceExpectedAnimalsTableGone("fake.go", goodGo13).length) {
    throw new Error("self-test failed: mode 13 false positive on a comment-only mention");
  }

  // 14: rosterCursor must never be declared again anywhere in the weighing module.
  const badGo14 = `
type rosterCursor struct {
	CreatedAt time.Time ` + "`json:\"created_at\"`" + `
	AnimalID  string    ` + "`json:\"animal_id\"`" + `
}
`;
  const findings14 = findingsForGoSourceRosterCursorAndItemsGone("fake.go", badGo14);
  if (!findings14.some((f) => f.rule === "roster-cursor-type-reintroduced")) {
    throw new Error(`self-test failed: mode 14 not flagged. got: ${JSON.stringify(findings14)}`);
  }
  // GOOD: a comment documenting the deletion must not fire.
  const goodGo14 = `
// rosterCursor was deleted outright; there is no roster to page any more.
`;
  if (findingsForGoSourceRosterCursorAndItemsGone("fake.go", goodGo14).length) {
    throw new Error("self-test failed: mode 14 false positive on a comment-only mention");
  }

  // 15: a domain.RosterPage{...} literal that populates Items with anything but an
  // empty slice must be flagged.
  const badGo15 = `
func (r *Repository) leftoverRosterBuild(ctx context.Context) (domain.RosterPage, error) {
	animals := []domain.ExpectedAnimal{{AnimalID: "goat-1"}}
	return domain.RosterPage{Items: animals, Observations: nil}, nil
}
`;
  const findings15 = findingsForGoSourceRosterCursorAndItemsGone("fake.go", badGo15);
  if (!findings15.some((f) => f.rule === "roster-items-populated")) {
    throw new Error(`self-test failed: mode 15 not flagged. got: ${JSON.stringify(findings15)}`);
  }
  // GOOD: the real repository shape (an always-empty `out` accumulator) must not fire.
  const goodGo15a = `
func (r *Repository) listScopeRoster(ctx context.Context) (domain.RosterPage, error) {
	out := make([]domain.ExpectedAnimal, 0)
	return domain.RosterPage{Items: out, Observations: observations, NextCursor: nextCursor, NextObservationsCursor: nextObservationsCursor}, nil
}
`;
  if (findingsForGoSourceRosterCursorAndItemsGone("fake.go", goodGo15a).some((f) => f.rule === "roster-items-populated")) {
    throw new Error("self-test failed: mode 15 false positive on the real repository's always-empty out accumulator");
  }
  // GOOD: an empty-slice literal directly in the composite literal must not fire either.
  const goodGo15b = `
func (f fakeRepo) ListScopeRoster(context.Context, string, string, string, string, int) (domain.RosterPage, error) {
	return domain.RosterPage{Items: []domain.ExpectedAnimal{}}, nil
}
`;
  if (findingsForGoSourceRosterCursorAndItemsGone("fake.go", goodGo15b).length) {
    throw new Error("self-test failed: mode 15 false positive on an empty-slice Items literal");
  }

  // GOOD: SQL operators that merely CONTAIN a scan keyword must not be read as table references.
  // `IS [NOT] DISTINCT FROM <expr>` compares two values; `FOR [NO KEY] UPDATE OF <alias>` locks a
  // row. Both were reported as phantom tables (`p`, `of`) on write paths doing exactly the
  // change-detection and row-locking this guard exists to encourage. A guard that punishes the
  // correct fix teaches people to disable it.
  const goodOperatorSql = `
func (r *Repository) recordUnknownAnimalObservationTx(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, ` + "`" + `
WITH proof_ok AS (SELECT proof_id FROM proof_artifacts WHERE tenant_id=$1),
prior AS (
  SELECT observation_id, proof_artifact_id FROM weighing_observations
   WHERE tenant_id=$1 FOR NO KEY UPDATE OF weighing_observations
)
UPDATE weighing_observations observation
   SET proof_artifact_id=p.proof_id
  FROM proof_ok p JOIN prior ON true
 WHERE prior.proof_artifact_id IS DISTINCT FROM p.proof_id
   AND observation.weight_kg IS NOT DISTINCT FROM prior.weight_kg
` + "`" + `, "t")
	return err
}
`;
  const operatorFindings = writePathTableFindings("fake.go", "recordUnknownAnimalObservationTx", goodOperatorSql);
  if (operatorFindings.length) {
    throw new Error(
      `self-test failed: SQL operator false positive -- IS DISTINCT FROM / FOR UPDATE OF must not be read as tables. got: ${JSON.stringify(operatorFindings)}`,
    );
  }

  // Mode 16: shed_partitions ALLOWED, goat_shed_partitions BANNED except for the
  // single Weights reporting file.
  // Maintainer decision 2026-08-06: shed_partitions is an ORG-scoped catalog (tenant_id, shed_id,
  // normalized_label) with no per-animal data. goat_shed_partitions is per-goat (tenant_id, goat_id)
  // and reveals which animal sits where. Maintainer decision 2026-08-19 permits that table only in
  // weight_demographics.go to label lump-sum composition at shed/partition grain.
  const goodMode16ShedPartitions = `
func (r *Repository) ResolveCampaignPartition(ctx context.Context) error {
  _, err := r.pool.Exec(ctx, ` + "`" + `
    SELECT sp.partition_label
    FROM shed_partitions sp
    WHERE sp.tenant_id=$1 AND sp.shed_id=$2
  ` + "`" + `)
  return err
}
`;
  const badMode16GoatShedPartitions = `
func (r *Repository) ResolveCampaignPartition(ctx context.Context) error {
  _, err := r.pool.Exec(ctx, ` + "`" + `
    SELECT gsp.partition_label, gsp.goat_id
    FROM goat_shed_partitions gsp
    WHERE gsp.tenant_id=$1 AND gsp.shed_id=$2
  ` + "`" + `)
  return err
}
`;
  const goodMode16 = anyPathTableFindings("fake.go", goodMode16ShedPartitions);
  if (goodMode16.length) {
    throw new Error(
      `self-test failed: mode 16 false positive on reading shed_partitions (ORG-scoped catalog). got: ${JSON.stringify(goodMode16)}`,
    );
  }
  const badMode16 = anyPathTableFindings("fake.go", badMode16GoatShedPartitions);
  if (!badMode16.some((f) => f.rule === "weighing-reads-non-weighing-table")) {
    throw new Error(
      `self-test failed: mode 16 did not flag goat_shed_partitions (per-goat, reveals animal location). got: ${JSON.stringify(badMode16)}`,
    );
  }
  const exemptMode16 = anyPathTableFindings(
    "backend/internal/weighing/adapters/postgres/weight_demographics.go",
    badMode16GoatShedPartitions,
  );
  if (exemptMode16.length) {
    throw new Error(
      `self-test failed: mode 16 false positive on goat_shed_partitions inside the Weights reporting exception. got: ${JSON.stringify(exemptMode16)}`,
    );
  }
  // Maintainer decision 2026-08-24: the lump-sum census snapshot file is the
  // SECOND file-scoped exemption (goats + goat_shed_partitions, one COUNT of a
  // bucket's residents). The same herd read in ANY OTHER weighing file must
  // still be a finding — the bad fixture above already proves that half.
  const censusRead = `
func (r *Repository) lumpSumCensusCountTx(ctx context.Context) error {
  _, err := r.pool.Exec(ctx, ` + "`" + `
    SELECT COUNT(*)
    FROM goats g
    LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
    WHERE g.tenant_id=$1 AND g.shed_id=$2
  ` + "`" + `)
  return err
}
`;
  const exemptCensus = anyPathTableFindings(
    "backend/internal/weighing/adapters/postgres/lump_sum_census.go",
    censusRead,
  );
  if (exemptCensus.length) {
    throw new Error(
      `self-test failed: mode 16 false positive on the lump-sum census snapshot exemption (maintainer decision 2026-08-24). got: ${JSON.stringify(exemptCensus)}`,
    );
  }
  // Maintainer decision 2026-09-01: origin_scope.go is the FOURTH file-scoped exemption and the
  // only one permitted `procurement_load_goats`. The two cases below are the whole point of
  // keying the table list PER FILE: the same read is legal in that file and a finding in another
  // exempt one. A single shared table set would pass BOTH, handing a procurement table to the
  // census and demographics files by accident.
  const boughtRead = `
func (r *Repository) resolveOriginScope(ctx context.Context) error {
  _, err := r.pool.Exec(ctx, ` + "`" + `
    SELECT DISTINCT goat_id FROM procurement_load_goats WHERE tenant_id=$1
  ` + "`" + `)
  return err
}
`;
  const exemptOrigin = anyPathTableFindings(
    "backend/internal/weighing/adapters/postgres/origin_scope.go",
    boughtRead,
  );
  if (exemptOrigin.length) {
    throw new Error(
      `self-test failed: mode 16 false positive on the origin-scope procurement exemption (maintainer decision 2026-09-01). got: ${JSON.stringify(exemptOrigin)}`,
    );
  }
  // THE ADVERSARIAL HALF: another EXEMPT file reading the same table must still fail. This is the
  // case a shared table set gets wrong, and it looks correct by name — sex_scope.go really is an
  // allowlisted file.
  const siblingExemptOrigin = anyPathTableFindings(
    "backend/internal/weighing/adapters/postgres/sex_scope.go",
    boughtRead,
  );
  if (!siblingExemptOrigin.some((f) => f.rule === "weighing-reads-non-weighing-table")) {
    throw new Error(
      "self-test failed: procurement_load_goats must be exempt for origin_scope.go ONLY — another exempt weighing file reading it is still a finding",
    );
  }

  const nonExemptCensus = anyPathTableFindings("fake.go", censusRead);
  if (!nonExemptCensus.some((f) => f.rule === "weighing-reads-non-weighing-table")) {
    throw new Error(
      `self-test failed: mode 16 must still flag the census read outside its exempt file. got: ${JSON.stringify(nonExemptCensus)}`,
    );
  }

  console.log("weighing-free-flow guard: self-test passed (16/16 failure modes + 4 demonstrated bypasses + shed_partitions distinction)");
}

// Builds a throwaway fixture repo under os.tmpdir(), writes ONE Go file and ONE migration file
// (either clean or containing exactly one planted violation), spawns THIS script as a child
// process with WEIGHING_GUARD_TEST_REPO pointed at it, and asserts the real process exit code —
// not just the in-process finding text. This is the required proof that `process.exit(1)` (not
// just console.error) actually fires for each failure mode, and that a clean tree exits 0.
function runOneExitCodeCase(
  label,
  { goSource, migrationSource, androidDtoSource, androidUiSource, adminWebUiSource },
  expectFailure,
) {
  const dir = mkdtempSync(join(tmpdir(), "weighing-free-flow-guard-exitcode-"));
  try {
    const goDir = join(dir, "backend/internal/weighing/adapters/postgres");
    const migDir = join(dir, "backend/migrations/postgres");
    mkdirSync(goDir, { recursive: true });
    mkdirSync(migDir, { recursive: true });
    writeFileSync(join(goDir, "fixture_repo.go"), goSource ?? "package postgres\n");
    writeFileSync(join(migDir, "000900_fixture_weighing.sql"), migrationSource ?? "-- +goose Up\n-- +goose Down\n");
    if (androidDtoSource) {
      const dtoDir = join(dir, "apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/dto");
      mkdirSync(dtoDir, { recursive: true });
      writeFileSync(join(dtoDir, "WeighingDto.kt"), androidDtoSource);
    }
    if (androidUiSource) {
      const uiDir = join(dir, "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/feature/weighing");
      mkdirSync(uiDir, { recursive: true });
      writeFileSync(join(uiDir, "WeighingScreen.kt"), androidUiSource);
    }
    if (adminWebUiSource) {
      const webDir = join(dir, "apps/admin-web/features/weighing");
      mkdirSync(webDir, { recursive: true });
      writeFileSync(join(webDir, "page.tsx"), adminWebUiSource);
    }
    const result = spawnSync(process.execPath, [SELF_PATH], {
      env: { ...process.env, WEIGHING_GUARD_TEST_REPO: dir },
      encoding: "utf8",
    });
    const status = result.status;
    const failed = status !== 0;
    if (failed !== expectFailure) {
      throw new Error(
        `exit-code self-test failed [${label}]: expected exit ${expectFailure ? "non-zero" : "0"}, got ${status}\nstdout: ${result.stdout}\nstderr: ${result.stderr}`,
      );
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

function selfTestExitCodes() {
  // A clean write path under the STRICT rule: weighing-owned tables + proof only,
  // no goats, no roster. (This fixture used to query `goats`; the allowlist rule now
  // correctly forbids that, so the "clean" baseline had to become genuinely clean.)
  const cleanGo = `package postgres

func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  rows, err := tx.Query(ctx, "SELECT campaign_shed_id FROM weighing_campaign_sheds WHERE tenant_id=$1", cmd.TenantID)
  return r.recordUnknownAnimalObservationTx(ctx, tx, cmd)
}
`;
  const cleanMig = `-- +goose Up
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id DROP NOT NULL;
CREATE UNIQUE INDEX weighing_observations_scanned_identifier_idx ON public.weighing_observations (tenant_id, campaign_shed_id, scanned_identifier);
-- +goose Down
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id SET NOT NULL;
`;

  runOneExitCodeCase("clean tree", { goSource: cleanGo, migrationSource: cleanMig }, false);

  runOneExitCodeCase(
    "mode 1: vaccination table read in write path",
    {
      goSource: `package postgres

func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  rows, err := tx.Query(ctx, "SELECT 1 FROM vaccination_completions WHERE goat_id=$1", cmd.AnimalID)
  return domain.Observation{}, nil
}
`,
      migrationSource: cleanMig,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 4: reject-null-animal-id",
    {
      goSource: `package postgres

func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  if cmd.AnimalID == "" {
    return domain.Observation{}, ports.ErrInvalidArgument
  }
  return domain.Observation{}, nil
}
`,
      migrationSource: cleanMig,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 2: animal-id NOT NULL reintroduced",
    {
      goSource: cleanGo,
      migrationSource: `-- +goose Up
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id SET NOT NULL;
-- +goose Down
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id DROP NOT NULL;
`,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 3: scanned_identifier unique across buckets",
    {
      goSource: cleanGo,
      migrationSource: `-- +goose Up
CREATE UNIQUE INDEX weighing_observations_scanned_identifier_idx ON public.weighing_observations (tenant_id, scanned_identifier);
`,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 5: clinical-state read in write path",
    {
      goSource: `package postgres

func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  rows, err := tx.Query(ctx, "SELECT 1 FROM goats g WHERE COALESCE(g.health_status,'healthy') <> ALL($1::text[])", protocoldomain.MandatoryClinicalDeferStates)
  return domain.Observation{}, nil
}
`,
      migrationSource: cleanMig,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 8: animal-id-required-precondition",
    {
      goSource: `package domain

type RecordAnimalObservation struct {
  AnimalID string ` + "`json:\"animal_id\" validate:\"required\"`" + `
}
`,
      migrationSource: cleanMig,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 9: android write request carries animal_id",
    {
      goSource: cleanGo,
      migrationSource: cleanMig,
      androidDtoSource: `package sg.mesha.goatos.core.network.dto

@Serializable
data class WeighingAnimalObservationRequestDto(
    @SerialName("campaign_shed_id") val campaignShedId: String,
    @SerialName("animal_id") val animalId: String,
    @SerialName("scanned_identifier") val scannedIdentifier: String,
)
`,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 10: expected-denominator progress in admin-web weighing UI",
    {
      goSource: cleanGo,
      migrationSource: cleanMig,
      adminWebUiSource: `export const label = \`\${completed}/\${expectedCount}\`;\n`,
    },
    true,
  );

  runOneExitCodeCase(
    "clean tree with a real Android write-request DTO and weighing UI (no animal_id, no denominator)",
    {
      goSource: cleanGo,
      migrationSource: cleanMig,
      androidDtoSource: `package sg.mesha.goatos.core.network.dto

@Serializable
data class WeighingAnimalObservationRequestDto(
    @SerialName("campaign_shed_id") val campaignShedId: String,
    @SerialName("scanned_identifier") val scannedIdentifier: String,
    @SerialName("weight_kg") val weightKg: Double,
)

@Serializable
data class WeighingObservationDto(
    @SerialName("observation_id") val observationId: String = "",
    @SerialName("animal_id") val animalId: String? = null,
)
`,
      androidUiSource: `private fun rosterSheetSubtitle(visibleCount: Int, totalExpected: Int): String =
    "$visibleCount captured rows"
`,
      adminWebUiSource: `export const label = \`\${completedCount} captured\`;\n`,
    },
    false,
  );

  console.log("weighing-free-flow guard: exit-code self-test passed (clean=0, 8/8 violations=non-zero)");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  selfTestExitCodes();
} else {
  run();
}
