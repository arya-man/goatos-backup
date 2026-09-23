#!/usr/bin/env node

// build-commit-ledger.mjs — writes the commit ledger by READING the commits.
//
//   node tools/dashboard-automation/build-commit-ledger.mjs            regenerate
//   node tools/dashboard-automation/build-commit-ledger.mjs --dry-run  print, write nothing
//
// Output: tools/dashboard-automation/commit-ledger.jsonl  (one row per commit)
//         tools/dashboard-automation/commit-ledger.json   (the summary)
//
// COUNT WHAT YOU READ. Every total this prints is counted while walking the rows
// it actually parsed. Nothing here prints the length of a list it was handed.
// The one place that could lie — the test index and the coverage artefacts — is
// asserted non-empty before a single status is derived, because an empty index
// would silently mark 2,669 correct commits as gaps.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import {
  assertArtefactsPresent,
  deriveBehaviour,
  deriveStatus,
  deriveSurfaces,
  dirOf,
  isTestFile,
  isUserVisible,
  parseSubject,
  summarize,
} from "./commit-ledger-lib.mjs";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
export const SINCE = "2026-08-01";
const ROWS_PATH = "tools/dashboard-automation/commit-ledger.jsonl";
const SUMMARY_PATH = "tools/dashboard-automation/commit-ledger.json";
const RECEIPTS_PATH = "tools/dashboard-automation/commit-ledger-receipts.json";
const FEATURE_ASSERTIONS_PATH = "tools/dashboard-automation/feature-assertions.json";

const git = (args) =>
  execFileSync("git", args, { cwd: repoRoot, encoding: "utf8", maxBuffer: 256 * 1024 * 1024 });

/** Parse `git log --name-only` with record/field separators that cannot occur in a path. */
export function parseGitLog(raw) {
  const commits = [];
  for (const block of String(raw).split("\u0001")) {
    if (!block.trim()) continue;
    const nl = block.indexOf("\n");
    const header = nl === -1 ? block : block.slice(0, nl);
    const [sha, short, date, ...subjectParts] = header.split("\u0002");
    if (!sha) continue;
    const files = (nl === -1 ? "" : block.slice(nl + 1))
      .split("\n")
      .map((l) => l.trim())
      .filter(Boolean);
    commits.push({ sha, short, date, subject: subjectParts.join("\u0002"), files });
  }
  return commits;
}

function readCommits(base) {
  const raw = git([
    "log",
    "--no-merges",
    `--since=${SINCE}`,
    "--format=%x01%H%x02%h%x02%ad%x02%s",
    "--date=short",
    "--name-only",
    base,
  ]);
  return parseGitLog(raw);
}

/** The repo's real test suite, indexed from the tracked file list that was read. */
function buildTestIndex() {
  const tracked = git(["ls-files"]).split("\n").filter(Boolean);
  const testDirs = new Set();
  const testedModules = new Set();
  let testFilesRead = 0;
  for (const f of tracked) {
    if (!isTestFile(f)) continue;
    testFilesRead += 1;
    testDirs.add(dirOf(f));
    let m;
    if ((m = /^backend\/internal\/([a-z0-9_]+)\//.exec(f))) testedModules.add(m[1]);
    if ((m = /^apps\/goatos-android\/feature\/feature-([a-z0-9-]+)\//.exec(f))) testedModules.add(m[1]);
    if ((m = /^apps\/admin-web\/features\/([a-z0-9-]+)\//.exec(f))) testedModules.add(m[1]);
  }
  const modulesOf = (files) => {
    const out = new Set();
    for (const f of files ?? []) {
      let m;
      if ((m = /^backend\/internal\/([a-z0-9_]+)\//.exec(f))) out.add(m[1]);
      if ((m = /^apps\/goatos-android\/feature\/feature-([a-z0-9-]+)\//.exec(f))) out.add(m[1]);
      if ((m = /^apps\/admin-web\/features\/([a-z0-9-]+)\//.exec(f))) out.add(m[1]);
    }
    return out;
  };
  return { testDirs, testedModules, modulesOf, trackedRead: tracked.length, testFilesRead };
}

async function loadArtefacts() {
  // Feature assertions carry a sha and an `expect` list. Whether an expectation
  // pins a VALUE or merely presence is decided by the sweep's own
  // `isValueExpect`, imported rather than reimplemented, so this ledger and the
  // thing that runs the assertions cannot drift into disagreeing.
  const faPath = join(repoRoot, FEATURE_ASSERTIONS_PATH);
  if (!existsSync(faPath)) throw new Error(`${FEATURE_ASSERTIONS_PATH} is missing — statuses cannot be derived without it`);
  const featureAssertions = JSON.parse(readFileSync(faPath, "utf8"));

  let isValueExpect;
  try {
    ({ isValueExpect } = await import(pathToFileURL(join(repoRoot, "apps/admin-web/scripts/lib/feature-assertions.mjs")).href));
  } catch (err) {
    throw new Error(`could not load the sweep's isValueExpect (${err.message}) — refusing to grade assertions with a second opinion`);
  }

  const valueAssertionShas = new Set();
  const presenceAssertionShas = new Set();
  let assertionsRead = 0;
  for (const entry of featureAssertions) {
    if (!entry?.sha) continue;
    assertionsRead += 1;
    const short = String(entry.sha).slice(0, 9);
    if ((entry.expect ?? []).some((e) => isValueExpect(e))) valueAssertionShas.add(short);
    else presenceAssertionShas.add(short);
  }

  // Routes the browser sweep actually visits. A route catalogue that cannot be
  // read is a FAILURE, not an empty set: silently reading zero routes moves
  // every admin-web row from smoke-only to gap, which accuses correct work — the
  // direction §2 calls the worse of the two. The first dry run of this generator
  // did exactly that (`sweptRoutesRead: 0`, because the export was called by the
  // wrong name), so the catch below reports instead of shrugging.
  const sweptRoutes = new Set();
  let routesRead = 0;
  {
    let smokeRoutes;
    try {
      ({ smokeRoutes } = await import(pathToFileURL(join(repoRoot, "apps/admin-web/scripts/lib/smoke-route-catalogue.mjs")).href));
    } catch (err) {
      throw new Error(`could not load the sweep's route catalogue (${err.message}) — every admin-web row would read as a gap`);
    }
    for (const route of smokeRoutes(repoRoot)) {
      routesRead += 1;
      // The catalogue carries query strings (`/feed/analytics?scope_mode=company`);
      // a commit touches a page directory, so compare pathnames.
      if (route.path) sweptRoutes.add(String(route.path).split("?")[0]);
    }
    if (routesRead === 0) throw new Error("the sweep route catalogue returned no routes — refusing to grade admin-web rows against nothing");
  }

  // Revert receipts: the ONLY source of `covered`.
  const revertReceipts = new Map();
  let receiptsRead = 0;
  const rPath = join(repoRoot, RECEIPTS_PATH);
  if (existsSync(rPath)) {
    const parsed = JSON.parse(readFileSync(rPath, "utf8"));
    for (const r of parsed.receipts ?? []) {
      if (!r?.sha || !r?.check) continue;
      receiptsRead += 1;
      revertReceipts.set(String(r.sha).slice(0, 9), r);
    }
  }

  return {
    artefacts: { valueAssertionShas, presenceAssertionShas, sweptRoutes, revertReceipts },
    read: { assertionsRead, routesRead, receiptsRead, featureAssertionEntries: featureAssertions.length },
  };
}

export function buildRows(commits, artefacts, index) {
  const rows = [];
  let scanned = 0;
  let outOfScope = 0;
  for (const c of commits) {
    scanned += 1;
    const parsed = parseSubject(c.subject);
    if (!isUserVisible(c.subject)) {
      outOfScope += 1;
      continue;
    }
    const surfaces = deriveSurfaces(c.files);
    const { behaviour, area, alsoTouches } = deriveBehaviour(surfaces, parsed);
    const { status, claimTier, evidence, reason } = deriveStatus({ sha: c.sha, files: c.files, surfaces, artefacts, index });
    rows.push({
      sha: c.short,
      date: c.date,
      type: parsed.type,
      subject: c.subject,
      behaviour,
      area,
      alsoTouches,
      modules: surfaces.backendModules,
      adminWebRoutes: surfaces.adminWebRoutes,
      adminWebFeatures: surfaces.adminWebFeatures,
      androidModules: surfaces.androidModules,
      migrations: surfaces.migrations,
      touchesContract: surfaces.touchesOpenApi,
      fileCount: c.files.length,
      status,
      claimTier,
      evidence,
      reason,
    });
  }
  return { rows, scanned, outOfScope };
}

function main() {
  const args = process.argv.slice(2);
  const dryRun = args.includes("--dry-run");
  const baseIdx = args.indexOf("--base");
  const base = baseIdx !== -1 ? args[baseIdx + 1] : "origin/main";

  const index = buildTestIndex();
  return loadArtefacts().then(({ artefacts, read }) => {
    const problems = assertArtefactsPresent(artefacts, index);
    if (problems.length) {
      for (const p of problems) console.error(`commit-ledger: ${p}`);
      process.exit(1);
    }
    const commits = readCommits(base);
    const { rows, scanned, outOfScope } = buildRows(commits, artefacts, index);
    const summary = summarize(rows);

    // Everything below is counted from what was READ, and says so.
    const out = {
      schemaVersion: 1,
      generatedFrom: { base, since: SINCE, generator: "tools/dashboard-automation/build-commit-ledger.mjs" },
      description:
        "One row per user-visible (feat/fix) commit on main since 2026-08-01, DERIVED from git log — never hand-typed. " +
        "STATUS: covered = a named check was demonstrated to fail when this commit is reverted (stored receipt, nothing else qualifies). " +
        "claimed = a named test plausibly covers it and the row says which and on what evidence, but nobody has shown it goes red. " +
        "smoke-only = the browser sweep reaches a surface it touched and nothing there can fail. " +
        "gap = no test in the repo touches anything it changed and no sweep reaches it, with a named reason. " +
        "Rows live in commit-ledger.jsonl.",
      inputsRead: {
        commitsScanned: scanned,
        outOfScopeNonFeatFix: outOfScope,
        trackedFilesRead: index.trackedRead,
        testFilesRead: index.testFilesRead,
        testDirsIndexed: index.testDirs.size,
        featureAssertionEntriesRead: read.featureAssertionEntries,
        featureAssertionsWithSha: read.assertionsRead,
        sweptRoutesRead: read.routesRead,
        revertReceiptsRead: read.receiptsRead,
      },
      summary,
    };

    const jsonl = `${rows.map((r) => JSON.stringify(r)).join("\n")}\n`;
    if (dryRun) {
      console.log(JSON.stringify(out, null, 2));
      return;
    }
    writeFileSync(join(repoRoot, ROWS_PATH), jsonl);
    writeFileSync(join(repoRoot, SUMMARY_PATH), `${JSON.stringify(out, null, 2)}\n`);
    console.log(`commit-ledger: read ${scanned} commits, wrote ${summary.commitsRead} rows in ${summary.behaviours} behaviours`);
    console.log(`  ${JSON.stringify(summary.byStatus)}`);
  });
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))) {
  main().catch((err) => {
    console.error(`commit-ledger: ${err.message}`);
    process.exit(1);
  });
}
