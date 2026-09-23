// commit-ledger-lib.mjs — the pure half of the commit ledger.
//
// WHY THIS EXISTS. The coverage ledger was 125 hand-typed descriptions citing 6
// commits, while 2,669 user-visible commits had landed since 2026-08-01. Nobody
// could see the gap because the denominator was written by hand: a ledger that
// lists what somebody remembered can never report what it is missing.
//
// So every row here is DERIVED from `git log`. The generator holds no list of
// commits, no list of behaviours and no hand-assigned status. It reads the
// commits, derives the module/surface facts from the paths each one touched,
// derives the behaviour grouping from those same facts, and reads the status off
// the coverage artefacts that define it — the same artefacts, through the same
// strength grader, that check-coverage-since-aug1.mjs uses. An entry cannot talk
// itself into being covered here either.
//
// Everything in this file is pure so the guard can re-derive a row without git,
// without the filesystem and without the network, and so the self-test can hand
// it adversarial input.

// ---------------------------------------------------------------------------
// 1. What counts as user-visible
// ---------------------------------------------------------------------------

// Conventional-commit type, measured against this repo's own history rather than
// assumed: `feat` and `fix` are what the maintainer means by "every commit, fix
// and feature". chore/docs/test/refactor/ci/build/perf/style ship no new visible
// behaviour on their own, so they are OUT OF SCOPE rather than gaps — a
// distinction the ledger records explicitly instead of quietly dropping them.
const CONVENTIONAL = /^(?<type>[a-z]+)(?:\((?<scope>[^)]*)\))?(?<bang>!)?:\s*(?<rest>.*)$/;
export const USER_VISIBLE_TYPES = new Set(["feat", "fix"]);

export function parseSubject(subject) {
  const m = CONVENTIONAL.exec(String(subject ?? "").trim());
  if (!m) return { type: null, scope: null, breaking: false, summary: String(subject ?? "").trim() };
  return {
    type: m.groups.type,
    scope: m.groups.scope ? m.groups.scope.trim().toLowerCase() : null,
    breaking: Boolean(m.groups.bang),
    summary: m.groups.rest.trim(),
  };
}

export function isUserVisible(subject) {
  const { type } = parseSubject(subject);
  return type !== null && USER_VISIBLE_TYPES.has(type);
}

// ---------------------------------------------------------------------------
// 2. Surfaces, derived from the paths a commit touched
// ---------------------------------------------------------------------------

// The six modules that exist only to connect other modules. Measured, not
// guessed: they carry no route, no screen and no endpoint of their own, so a
// per-screen check can NEVER reach them and calling them an ordinary gap would
// invite somebody to go write a screen check that cannot exist. They get their
// own named reason.
export const BRIDGE_MODULES = new Set([
  "countsbridge",
  "sopbridge",
  "notificationbridge",
  "domainconsumer",
  "eventwiring",
  "kernelstages",
]);

const RE = {
  backend: /^backend\/internal\/([a-z0-9_]+)\//,
  migration: /^backend\/migrations\/postgres\/(\d+_[a-z0-9_]+\.sql)$/,
  openapi: /^contracts\/openapi\//,
  adminRoute: /^apps\/admin-web\/app\/\(admin\)\/(.+)\/page\.tsx$/,
  adminFeature: /^apps\/admin-web\/features\/([a-z0-9-]+)\//,
  adminOther: /^apps\/admin-web\//,
  android: /^apps\/goatos-android\/feature\/feature-([a-z0-9-]+)\//,
  androidOther: /^apps\/goatos-android\//,
  tooling: /^(tools|\.github|\.agents|\.agent|scripts)\//,
  docs: /^(docs|context|wiki|mock)\//,
};

/** Every fact the ledger states about a commit's surfaces comes from here. */
export function deriveSurfaces(files) {
  const backendModules = new Set();
  const adminWebRoutes = new Set();
  const adminWebFeatures = new Set();
  const androidModules = new Set();
  const migrations = new Set();
  let touchesOpenApi = false;
  let touchesAdminWebOther = false;
  let touchesAndroidOther = false;
  let touchesTooling = false;
  let touchesDocsOnly = true;

  for (const file of files ?? []) {
    const path = String(file);
    if (!path) continue;
    if (!RE.docs.test(path)) touchesDocsOnly = false;

    let m;
    if ((m = RE.backend.exec(path))) backendModules.add(m[1]);
    if ((m = RE.migration.exec(path))) migrations.add(m[1]);
    if (RE.openapi.test(path)) touchesOpenApi = true;
    // A route directory can carry Next.js group segments `(x)` and dynamic
    // segments `[id]`; both are part of the real URL shape a person visits, so
    // only the group parens are dropped.
    if ((m = RE.adminRoute.exec(path))) {
      const route = m[1]
        .split("/")
        .filter((seg) => !/^\(.*\)$/.test(seg))
        .join("/");
      if (route) adminWebRoutes.add(`/${route}`);
    }
    if ((m = RE.adminFeature.exec(path))) adminWebFeatures.add(m[1]);
    else if (RE.adminOther.test(path)) touchesAdminWebOther = true;
    if ((m = RE.android.exec(path))) androidModules.add(m[1]);
    else if (RE.androidOther.test(path)) touchesAndroidOther = true;
    if (RE.tooling.test(path)) touchesTooling = true;
  }

  return {
    backendModules: [...backendModules].sort(),
    adminWebRoutes: [...adminWebRoutes].sort(),
    adminWebFeatures: [...adminWebFeatures].sort(),
    androidModules: [...androidModules].sort(),
    migrations: [...migrations].sort(),
    touchesOpenApi,
    touchesAdminWebOther,
    touchesAndroidOther,
    touchesTooling,
    touchesDocsOnly: touchesDocsOnly && (files ?? []).length > 0,
  };
}

// ---------------------------------------------------------------------------
// 3. The grouping rule
// ---------------------------------------------------------------------------

// THE RULE, stated so anybody can re-derive it and get the same answer:
//
//   behaviour = "<area>/<subject>"
//
//   area     the layer the commit's files land in, in a fixed precedence:
//            admin-web > android > backend > migration > contract > tooling > docs.
//            Precedence, not "the most files", because a commit that adds one
//            screen and twelve backend files is still that screen's behaviour to
//            the person who sees it — and a rule keyed on file COUNT moves a row
//            between groups when somebody reformats.
//
//   subject  the single most specific thing that area names: the admin-web
//            feature directory, the Android feature module, the backend module,
//            or the conventional-commit scope when the area names nothing
//            (a lone migration, a contract-only change).
//
// Forty commits iterating on vaccination drive batching all touch
// backend/internal/vaccination, so they land in ONE behaviour with forty cases —
// which is the point. The grouping is a property of the commits, not an opinion
// about them, so two people re-deriving it get the same groups.
//
// Where a commit's area names more than one subject (two backend modules), the
// alphabetically first is the group key and the rest are recorded on the row as
// `alsoTouches`. Picking the first is arbitrary but STABLE; picking "the most
// important" would be a hand-written ledger wearing a rule's clothes.
export const AREA_PRECEDENCE = ["admin-web", "android", "backend", "migration", "contract", "tooling", "docs"];

export function deriveBehaviour(surfaces, parsed) {
  const pick = (area, list) => ({ area, subject: list[0], also: list.slice(1) });
  let chosen = null;
  if (surfaces.adminWebFeatures.length) chosen = pick("admin-web", surfaces.adminWebFeatures);
  else if (surfaces.adminWebRoutes.length) chosen = pick("admin-web", surfaces.adminWebRoutes);
  else if (surfaces.androidModules.length) chosen = pick("android", surfaces.androidModules);
  else if (surfaces.backendModules.length) chosen = pick("backend", surfaces.backendModules);
  else if (surfaces.migrations.length) chosen = pick("migration", [parsed.scope || "schema"]);
  else if (surfaces.touchesOpenApi) chosen = pick("contract", [parsed.scope || "openapi"]);
  else if (surfaces.touchesAdminWebOther) chosen = pick("admin-web", [parsed.scope || "shell"]);
  else if (surfaces.touchesAndroidOther) chosen = pick("android", [parsed.scope || "app"]);
  else if (surfaces.touchesTooling) chosen = pick("tooling", [parsed.scope || "tooling"]);
  else if (surfaces.touchesDocsOnly) chosen = pick("docs", [parsed.scope || "docs"]);
  else chosen = pick("other", [parsed.scope || "unclassified"]);
  return { behaviour: `${chosen.area}/${chosen.subject}`, area: chosen.area, alsoTouches: chosen.also };
}


// ---------------------------------------------------------------------------
// 4. Status — read off the tests and coverage artefacts, never declared
// ---------------------------------------------------------------------------

// FOUR statuses, and the split between the first two is the whole point.
//
//   covered    a named check was DEMONSTRATED to fail when this commit's
//              non-test changes are reverted. Sourced only from a stored
//              receipt. Nothing else may claim it.
//   claimed    a named test plausibly covers this, and the row says which test
//              and on what evidence — but nobody has shown it goes red. This is
//              the honest home of most rows at this scale, and collapsing it
//              into `covered` would be the 87% → 5.6% mistake again.
//   smoke-only the browser sweep reaches a surface this commit touched and
//              nothing there can fail.
//   gap        no test anywhere in the repo appears to touch this, and no sweep
//              reaches it. Each carries its own named reason.
export const STATUSES = new Set(["covered", "claimed", "smoke-only", "gap"]);

// How a `claimed` row knows a test exists, strongest first. The tier is recorded
// on the row so the ledger is orderable by how much its own claim is worth.
export const CLAIM_TIERS = {
  SHIPS_OWN_TEST: "ships-own-test",   // the commit itself changed a test file
  SIBLING_TEST: "sibling-test",       // a file it changed sits in a directory that holds tests
  MODULE_TEST: "module-test",         // the module it changed has tests elsewhere
};

export const GAP_REASONS = {
  BRIDGE: "bridge module — wires other modules together, carries no route, screen or endpoint a per-screen check can reach, and no test sits with the files it changed",
  BACKEND_NO_TEST: "backend package it changed holds no test, and no sweep reaches it",
  MIGRATION_ONLY: "schema-only — a migration with no test and no surface in the same commit",
  ANDROID_NO_TEST: "Android module it changed holds no test and no journey reaches it",
  ADMIN_WEB_NO_TEST: "admin-web code it changed holds no test and the sweep does not visit the route",
  TOOLING: "automation/CI tooling — no product surface",
  DOCS: "documentation only — no product surface",
  NO_REFERENCE: "no test and no coverage artefact touches anything this commit changed",
};

const TEST_FILE_RE = /(_test\.go|Test\.kt|Tests\.kt|\.test\.(?:mjs|js|ts|tsx)|_test\.py)$/;
const TEST_DIR_RE = /\/(?:src\/test|src\/androidTest|__tests__)\//;

export function isTestFile(path) {
  const p = String(path);
  return TEST_FILE_RE.test(p) || TEST_DIR_RE.test(p);
}

export function dirOf(path) {
  return String(path).split("/").slice(0, -1).join("/");
}

/**
 * Which tests does the repo have that bear on this commit?
 *
 * `testDirs` and `testedModules` are counted from files that were READ off the
 * index, never from a list of directories somebody expected to find. A commit
 * whose files all live in directories holding tests gets the sibling tier; the
 * module tier is the weaker fallback for a package that is tested somewhere but
 * not beside the file that changed.
 */
export function findTestEvidence(files, index) {
  const own = (files ?? []).filter(isTestFile);
  if (own.length) {
    return { tier: CLAIM_TIERS.SHIPS_OWN_TEST, tests: own.slice(0, 4), note: "this commit changed its own test" };
  }
  const siblings = [];
  for (const f of files ?? []) {
    const d = dirOf(f);
    if (index.testDirs.has(d)) siblings.push(d);
  }
  if (siblings.length) {
    return { tier: CLAIM_TIERS.SIBLING_TEST, tests: [...new Set(siblings)].slice(0, 4), note: "tests sit in the same directory as a file this commit changed" };
  }
  const modules = [];
  for (const m of new Set([...(index.modulesOf?.(files) ?? [])])) {
    if (index.testedModules.has(m)) modules.push(m);
  }
  if (modules.length) {
    return { tier: CLAIM_TIERS.MODULE_TEST, tests: modules.slice(0, 4), note: "the module has tests, but not beside the files this commit changed" };
  }
  return null;
}

/**
 * Derive one row's status.
 *
 * `covered` comes from a stored revert receipt and from nothing else. Reverting
 * a commit and watching a test go red is the only thing that proves the test
 * bites, and at 2,669 commits that is affordable for a sample, not for all of
 * them — so the sample is `covered` and the rest is `claimed`, with the
 * difference visible rather than averaged away.
 */
export function deriveStatus({ sha, files, surfaces, artefacts, index }) {
  const short = String(sha).slice(0, 9);

  const receipt = artefacts.revertReceipts.get(short);
  if (receipt) {
    return { status: "covered", claimTier: null, evidence: [receipt.check], reason: `reverting this commit makes ${receipt.check} fail — receipt ${receipt.recordedAt}` };
  }

  const test = findTestEvidence(files, index);
  if (test) {
    return { status: "claimed", claimTier: test.tier, evidence: test.tests, reason: `${test.note} — not shown to fail on revert` };
  }

  if (artefacts.valueAssertionShas.has(short)) {
    return { status: "claimed", claimTier: "feature-assertion-value", evidence: [`feature-assertions:${short}`], reason: "a browser feature assertion on this commit pins a value — not shown to fail on revert" };
  }
  if (artefacts.presenceAssertionShas.has(short)) {
    return { status: "smoke-only", claimTier: null, evidence: [`feature-assertions:${short}`], reason: "a feature assertion names this commit but only checks whether things are on the page" };
  }
  const swept = surfaces.adminWebRoutes.filter((r) => artefacts.sweptRoutes.has(r));
  if (swept.length) {
    return { status: "smoke-only", claimTier: null, evidence: swept, reason: `the sweep visits ${swept.join(", ")} but nothing there can fail` };
  }

  // Gaps, most specific reason first.
  if (surfaces.backendModules.some((m) => BRIDGE_MODULES.has(m))) {
    return { status: "gap", claimTier: null, evidence: [], reason: GAP_REASONS.BRIDGE };
  }
  if (surfaces.androidModules.length || surfaces.touchesAndroidOther) {
    return { status: "gap", claimTier: null, evidence: [], reason: GAP_REASONS.ANDROID_NO_TEST };
  }
  if (surfaces.adminWebRoutes.length || surfaces.adminWebFeatures.length || surfaces.touchesAdminWebOther) {
    return { status: "gap", claimTier: null, evidence: [], reason: GAP_REASONS.ADMIN_WEB_NO_TEST };
  }
  if (surfaces.backendModules.length) {
    return { status: "gap", claimTier: null, evidence: [], reason: GAP_REASONS.BACKEND_NO_TEST };
  }
  if (surfaces.migrations.length) return { status: "gap", claimTier: null, evidence: [], reason: GAP_REASONS.MIGRATION_ONLY };
  if (surfaces.touchesTooling) return { status: "gap", claimTier: null, evidence: [], reason: GAP_REASONS.TOOLING };
  if (surfaces.touchesDocsOnly) return { status: "gap", claimTier: null, evidence: [], reason: GAP_REASONS.DOCS };
  return { status: "gap", claimTier: null, evidence: [], reason: GAP_REASONS.NO_REFERENCE };
}

/**
 * Fail closed on a missing artefact instead of reporting everything uncovered.
 *
 * Trap 6 from the handover in its live form: a listed-but-unread input
 * contributes nothing silently. Here the silence would push the answer toward
 * ACCUSING correct work — every row would read `gap` and the ledger would demand
 * thousands of tests that already exist, which §2 calls the worse of the two
 * failures. So an artefact that did not load is an error, never an empty set.
 */
export function assertArtefactsPresent(artefacts, index) {
  const problems = [];
  for (const name of ["valueAssertionShas", "presenceAssertionShas", "sweptRoutes"]) {
    if (!(artefacts?.[name] instanceof Set)) problems.push(`${name} did not load — a missing artefact would read as "nothing is covered", which accuses correct work`);
  }
  if (!(artefacts?.revertReceipts instanceof Map)) problems.push("revertReceipts did not load — without it every proven row silently downgrades to claimed");
  if (!(index?.testDirs instanceof Set) || index.testDirs.size === 0) {
    problems.push("the test-file index is empty — with 2,147 test files in the tree that means the index was not read, and every row would read as a gap");
  }
  return problems;
}

/** Counted from the rows actually read — never from a length recorded earlier. */
export function summarize(rows) {
  const byStatus = {};
  const byArea = {};
  const byClaimTier = {};
  const gapReasons = {};
  const behaviours = new Set();
  let read = 0;
  for (const row of rows) {
    read += 1;
    byStatus[row.status] = (byStatus[row.status] ?? 0) + 1;
    byArea[row.area] = (byArea[row.area] ?? 0) + 1;
    if (row.claimTier) byClaimTier[row.claimTier] = (byClaimTier[row.claimTier] ?? 0) + 1;
    if (row.status === "gap") gapReasons[row.reason] = (gapReasons[row.reason] ?? 0) + 1;
    behaviours.add(row.behaviour);
  }
  return { commitsRead: read, behaviours: behaviours.size, byStatus, byClaimTier, byArea, gapReasons };
}
