#!/usr/bin/env node

// check-offline-first-reads.mjs — enforces the offline-first read repository rule
// (docs/decisions/android-offline-first.md): every screen-facing READ repository must
// persist backend responses to Room and expose a Flow-based observe method.
// A network-only read repository — a repo method that calls api.xxx() and returns
// the result directly to the ViewModel with NO Room persistence — is BANNED for
// screen-facing reads (bug class C35-001/C35-019/MOB-007).
//
// Modes:
//   (default)     scan the whole android tree and report offenders.
//                 Compare findings against a committed baseline file; fail only on NEW offenders.
//   --self-test   run the built-in adversarial Kotlin snippets and exit.
//
// Escape hatch: a genuinely-bounded case may append `offline-first-guard:ignore: <reason>`
// on the line where the repo method is defined.

import { execSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const BASELINE_FILE = "tools/agent-hooks/offline-first-reads-baseline.txt";

// Patterns for repositories that are screen-facing (have interface + default implementation)
const isRepositoryKt = (rel) =>
  rel.endsWith("Repository.kt") &&
  rel.includes("core-data") &&
  rel.includes("/src/main/") &&
  !rel.includes("/build/");

// A finding: { line, relPath, methodName, reason }
export function findingsForSource(source, relPath) {
  const findings = [];
  const lines = source.split("\n");

  // Parse the repository structure: interface followed by Default implementation class
  const interfaceMatcher = /^\s*interface\s+(\w+Repository)/m;
  const implMatcher = /^\s*class\s+(Default\w+Repository)/m;

  const interfaceMatch = interfaceMatcher.exec(source);
  const implMatch = implMatcher.exec(source);

  if (!interfaceMatch || !implMatch) {
    // Not a standard repository pattern
    return findings;
  }

  const interfaceName = interfaceMatch[1];

  // Find all public interface method declarations (suspend fun or fun returning Flow/Dto)
  // Pattern: fun xxx(...): SomeType or suspend fun xxx(...): SomeType
  const interfaceMethodsRegex = /^\s*(?:suspend\s+)?fun\s+(\w+)\s*\([^)]*\):\s*([^/\n{]+)/gm;
  let match;
  const methodSignatures = [];

  const interfaceStart = interfaceMatcher.index;
  const implStart = implMatcher.index;
  const interfaceBody = source.slice(interfaceStart, implStart);

  interfaceMethodsRegex.lastIndex = 0;
  while ((match = interfaceMethodsRegex.exec(interfaceBody)) !== null) {
    const methodName = match[1];
    const returnType = match[2].trim();
    const fullLine = match[0];
    const lineNum = source.slice(0, match.index + interfaceStart).split("\n").length;
    methodSignatures.push({ methodName, returnType, lineNum, fullLine });
  }

  // For each interface method, find the corresponding implementation and check for offline-first violations
  for (const { methodName, returnType, lineNum } of methodSignatures) {
    // Skip private/helper methods (those that don't observe from Room or aren't screen-facing)
    // Heuristic: screen-facing methods are:
    //   - fun observe...: Flow<Resource<...>>
    //   - fun refresh...: Result<Unit>
    //   - suspend fun get/fetch/list...: Dto (but these should also have an observe counterpart)

    const isObserveMethod = methodName.includes("observe") || methodName.includes("Observe");
    const isRefreshMethod = methodName.includes("refresh") || methodName.includes("Refresh");

    // For observe methods: check they come from Room (dao.observe or similar)
    if (isObserveMethod) {
      const implMethodRegex = new RegExp(
        `override\\s+fun\\s+${methodName}\\s*\\([^)]*\\)[^{]*\\{`,
        "s"
      );
      const implStart = source.search(implMethodRegex);
      if (implStart < 0) continue;

      // Scan the body of the implementation (up to next method, ~500 chars)
      const bodyStart = source.indexOf("{", implStart);
      const nextMethod = source.indexOf("\n    override fun", bodyStart + 1);
      const nextTopMethod = source.indexOf("\n    private fun", bodyStart + 1);
      const bodyEnd = Math.min(
        nextMethod > 0 ? nextMethod : Infinity,
        nextTopMethod > 0 ? nextTopMethod : Infinity,
        bodyStart + 800
      );
      const methodBody = source.slice(bodyStart, bodyEnd);

      // An observe method must have a Flow that reads from Room (dao.observe)
      // If it only returns api.xxx() or creates a Flow wrapping api.xxx() without dao, it's wrong
      if (
        !methodBody.includes("dao.observe") &&
        !methodBody.includes("dao.get") &&
        !methodBody.includes("database.") &&
        methodBody.includes("api.")
      ) {
        const bodyLineNum = source.slice(0, implStart).split("\n").length;
        findings.push({
          line: bodyLineNum,
          methodName,
          reason: `observe method '${methodName}' calls api.xxx() but doesn't upsert or observe from Room; must be backed by a dao.observe() call`,
        });
      }
    }

    // For refresh methods: check they upsert to Room
    if (isRefreshMethod) {
      const implMethodRegex = new RegExp(
        `override\\s+suspend\\s+fun\\s+${methodName}\\s*\\([^)]*\\)`,
        "s"
      );
      const implStart = source.search(implMethodRegex);
      if (implStart < 0) continue;

      const bodyStart = source.indexOf("{", implStart);
      const nextMethod = source.indexOf("\n    override", bodyStart + 1);
      const nextTopMethod = source.indexOf("\n    private fun", bodyStart + 1);
      const bodyEnd = Math.min(
        nextMethod > 0 ? nextMethod : Infinity,
        nextTopMethod > 0 ? nextTopMethod : Infinity,
        bodyStart + 800
      );
      const methodBody = source.slice(bodyStart, bodyEnd);

      // A refresh method must call api.xxx() and then upsert to a DAO
      if (
        methodBody.includes("api.") &&
        !methodBody.includes("dao.upsert")
      ) {
        const bodyLineNum = source.slice(0, implStart).split("\n").length;
        findings.push({
          line: bodyLineNum,
          methodName,
          reason: `refresh method '${methodName}' fetches from api but doesn't upsert to Room; must call dao.upsert() after successful fetch`,
        });
      }
    }

    // For one-shot get/fetch methods: check if they're exposed in the interface
    // (suspend fun without observe counterpart means it's likely screen-facing and needs offline-first)
    if (!isObserveMethod && !isRefreshMethod && returnType && !returnType.includes("Flow")) {
      const implMethodRegex = new RegExp(
        `override\\s+suspend\\s+fun\\s+${methodName}\\s*\\([^)]*\\)[^{]*\\{`,
        "s"
      );
      const implStart = source.search(implMethodRegex);
      if (implStart < 0) continue;

      const bodyStart = source.indexOf("{", implStart);
      const nextMethod = source.indexOf("\n    override", bodyStart + 1);
      const bodyEnd = nextMethod > 0 ? nextMethod : bodyStart + 500;
      const methodBody = source.slice(bodyStart, bodyEnd);

      // Check if this method just returns api.xxx() without any Room operations
      // Simple heuristic: if it calls api.xxx() and doesn't call dao.upsert, flag it
      // BUT skip if the method is clearly a one-shot helper (name suggests it, or very short)
      if (
        methodBody.includes("api.") &&
        !methodBody.includes("dao.")
      ) {
        // Don't flag one-shot helpers like `suspend fun events()` if there's an `observe` counterpart
        // This is handled by checking if there's an observeXxx method in the interface
        const hasObserveCounterpart = methodSignatures.some(
          (m) => m.methodName === `observe${methodName.charAt(0).toUpperCase()}${methodName.slice(1)}`
        );
        if (!hasObserveCounterpart) {
          const bodyLineNum = source.slice(0, implStart).split("\n").length;
          findings.push({
            line: bodyLineNum,
            methodName,
            reason: `screen-facing method '${methodName}' returns api.xxx() directly; needs an observe() Flow-based counterpart backed by Room for offline-first`,
          });
        }
      }
    }
  }

  // Filter out lines with ignore comments (check the line itself and surrounding context)
  return findings.filter((f) => {
    const lineText = lines[f.line - 1] || "";
    const prevLineText = lines[f.line - 2] || "";
    const nextLineText = lines[f.line] || "";
    return !(
      lineText.includes("offline-first-guard:ignore") ||
      prevLineText.includes("offline-first-guard:ignore") ||
      nextLineText.includes("offline-first-guard:ignore")
    );
  });
}

function walkAndroid(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (["build", "node_modules", ".gradle"].includes(entry.name)) continue;
      out.push(...walkAndroid(path));
    } else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (isRepositoryKt(rel)) out.push(rel);
    }
  }
  return out;
}

function readBaseline() {
  try {
    const content = readFileSync(resolve(repo, BASELINE_FILE), "utf8");
    const lines = content.split("\n");
    const entries = [];
    for (const line of lines) {
      if (line.startsWith("#") || !line.trim()) continue;
      // Format: path:line: reason
      const match = line.match(/^(.+?):(\d+):\s*(.+)$/);
      if (match) {
        entries.push({
          path: match[1],
          line: parseInt(match[2]),
          reason: match[3],
        });
      }
    }
    return entries;
  } catch {
    return [];
  }
}

function selfTest() {
  const testCases = [
    {
      name: "network_only_observe",
      pass: false,
      code: `
interface TestRepository {
    fun observeItems(): Flow<Resource<ItemList>>
}

class DefaultTestRepository(private val api: AppApi) : TestRepository {
    override fun observeItems(): Flow<Resource<ItemList>> {
        return flow { emit(Resource(data = api.listItems())) }
    }
}`,
    },
    {
      name: "correct_offline_first_observe",
      pass: true,
      code: `
interface TestRepository {
    fun observeItems(): Flow<Resource<ItemList>>
}

class DefaultTestRepository(
    private val api: AppApi,
    private val dao: ItemDao,
) : TestRepository {
    override fun observeItems(): Flow<Resource<ItemList>> {
        return dao.observe().map { Resource(data = it) }
    }
}`,
    },
    {
      name: "refresh_without_upsert",
      pass: false,
      code: `
interface TestRepository {
    suspend fun refreshItems(): Result<Unit>
}

class DefaultTestRepository(private val api: AppApi) : TestRepository {
    override suspend fun refreshItems(): Result<Unit> = runCatching {
        api.listItems()
    }
}`,
    },
    {
      name: "correct_refresh_with_upsert",
      pass: true,
      code: `
interface TestRepository {
    suspend fun refreshItems(): Result<Unit>
}

class DefaultTestRepository(
    private val api: AppApi,
    private val dao: ItemDao,
) : TestRepository {
    override suspend fun refreshItems(): Result<Unit> = runCatching {
        val dto = api.listItems()
        dao.upsert(dto.toEntity())
    }
}`,
    },
    {
      name: "ignore_comment_blocks_detection",
      pass: true,
      code: `
interface TestRepository {
    fun observeItems(): Flow<Resource<ItemList>>
}

class DefaultTestRepository(private val api: AppApi) : TestRepository {
    override fun observeItems(): Flow<Resource<ItemList>> {
        // offline-first-guard:ignore: legacy shim pending migration
        return flow { emit(Resource(data = api.listItems())) }
    }
}`,
    },
  ];

  let failures = 0;
  for (const tc of testCases) {
    const findings = findingsForSource(tc.code, "test.kt");
    const hasFinding = findings.length > 0;
    const pass = hasFinding === !tc.pass; // pass if finding matches expectation
    if (!pass) {
      failures++;
      console.error(
        `offline-first-guard self-test ${tc.name}: got ${hasFinding ? "fail" : "pass"}, want ${tc.pass ? "pass" : "fail"} (findings: ${findings.length})`
      );
    }
  }

  if (failures > 0) {
    console.error(`offline-first-guard: ${failures} self-test(s) failed`);
    process.exit(1);
  }
  console.log(`offline-first-guard: ${testCases.length} self-tests passed`);
}

function runScan() {
  const allRepositories = walkAndroid(resolve(repo, "apps/goatos-android"));
  const findings = [];

  for (const rel of allRepositories) {
    let src;
    try {
      src = readFileSync(resolve(repo, rel), "utf8");
    } catch {
      continue;
    }
    for (const f of findingsForSource(src, rel)) {
      findings.push({
        path: rel,
        line: f.line,
        methodName: f.methodName,
        reason: f.reason,
      });
    }
  }

  const baseline = readBaseline();
  const baselineSet = new Set(baseline.map((f) => `${f.path}:${f.line}`));

  // Report only NEW findings (not in baseline)
  const newFindings = findings.filter((f) => !baselineSet.has(`${f.path}:${f.line}`));

  if (newFindings.length > 0) {
    console.error(
      `offline-first-guard: ${newFindings.length} new offline-first violations found (see docs/decisions/android-offline-first.md)`
    );
    for (const f of newFindings) {
      console.error(`  ${f.path}:${f.line}: ${f.methodName}: ${f.reason}`);
    }
    console.error(
      "\nTo suppress a legitimate case, append `offline-first-guard:ignore: <reason>` on the method line."
    );
    process.exit(1);
  }

  console.log(
    `offline-first-guard: ok (${allRepositories.length} repositories scanned; ${findings.length} known grandfathered offender(s); 0 new violations)`
  );
}

const mode = process.argv[2];
if (mode === "--self-test") {
  selfTest();
} else {
  runScan();
}
