#!/usr/bin/env node

// check-offline-first-reads.mjs — enforces the offline-first read repository rule
// (docs/decisions/android-offline-first.md): every screen-facing READ repository must
// persist backend responses to Room and expose a Flow-based observe method.
// A network-only read repository — a repo method that calls api.xxx() and returns
// the result directly to the ViewModel with NO Room persistence — is BANNED for
// screen-facing reads (bug class C35-001/C35-019/MOB-007).
//
// Detection is DEPENDENCY-AWARE, not substring-based: it discovers the actual Room
// persistence fields (any name/case) from the Default…Repository constructor and checks
// whether a method reads/writes one of them — so a DAO field named `taskDetailDao` or
// `coverageDao` is recognised exactly like `dao`, and an expression-body method (`= dao
// .observe(…).map { }`) is scanned from its signature, not from the first `{` (which the
// old literal check mistook for the `.map { }` lambda). The prior substring heuristic
// false-flagged both cases.
//
// Modes:
//   (default)     scan the whole android tree; compare to a committed baseline, fail on NEW.
//   --list        print EVERY finding (ignores baseline) — used to (re)generate the baseline.
//   --self-test   run the built-in adversarial Kotlin snippets and exit.
//
// Escape hatch: a genuinely-bounded case may append `offline-first-guard:ignore: <reason>`
// on (or adjacent to) the line where the repo method is defined.

import { readFileSync, readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const BASELINE_FILE = "tools/agent-hooks/offline-first-reads-baseline.txt";

const isRepositoryKt = (rel) =>
  rel.endsWith("Repository.kt") &&
  rel.includes("core-data") &&
  rel.includes("/src/main/") &&
  !rel.includes("/build/");

// ---- pure analysis (unit-tested by --self-test) --------------------------------------------

const READ_VERBS = "observe|get|read|flow|stream|find|count|load|query";
const WRITE_VERBS = "upsert|insert|save|update|delete|put|clear|replace";

/**
 * Names of the Room persistence dependencies injected into the Default…Repository constructor,
 * by DECLARED FIELD NAME regardless of naming/case (`dao`, `taskDetailDao`, `coverageDao`,
 * `screenCacheStore`, `database`). A field counts if its type ends in Dao / Database / Store.
 */
export function discoverPersistenceFields(source, implStart) {
  const open = source.indexOf("(", implStart);
  if (open < 0) return [];
  let depth = 0;
  let end = -1;
  for (let i = open; i < source.length; i++) {
    if (source[i] === "(") depth++;
    else if (source[i] === ")") {
      depth--;
      if (depth === 0) {
        end = i;
        break;
      }
    }
  }
  if (end < 0) return [];
  const params = source.slice(open + 1, end);
  const fields = [];
  const re = /(?:private\s+|internal\s+|public\s+)?val\s+(\w+)\s*:\s*([\w.]+)/g;
  let m;
  while ((m = re.exec(params)) !== null) {
    if (/(Dao|Database|Store)$/.test(m[2])) fields.push(m[1]);
  }
  return fields;
}

/**
 * The implementation body of `methodName` (block OR expression body) plus its 1-indexed line.
 * The window runs from the `override … fun name(` signature to the next member declaration (or a
 * bounded fallback), so it captures `= expr…` and `{ … }` uniformly — unlike anchoring on the
 * first `{`, which an expression body's `.map { }` lambda hijacked.
 */
export function implBody(source, methodName) {
  const sig = new RegExp(`override\\s+(?:suspend\\s+)?fun\\s+${methodName}\\s*\\(`);
  const start = source.search(sig);
  if (start < 0) return null;
  const rest = source.slice(start + 1);
  const nextIdx = rest.search(/\n\s{2,}(?:@\w+\s+)?(?:override|private|internal|public)\s+(?:suspend\s+)?fun\s/);
  const body = rest.slice(0, nextIdx < 0 ? 900 : Math.min(nextIdx, 900));
  const lineNum = source.slice(0, start).split("\n").length;
  return { body, lineNum };
}

export function findingsForSource(source, relPath) {
  const lines = source.split("\n");
  const interfaceMatch = /^\s*interface\s+(\w+Repository)/m.exec(source);
  const implMatch = /^\s*class\s+(Default\w+Repository)/m.exec(source);
  if (!interfaceMatch || !implMatch) return [];

  const interfaceBody = source.slice(interfaceMatch.index, implMatch.index);
  const fields = discoverPersistenceFields(source, implMatch.index);

  const readsRoom = (body) =>
    fields.some((f) => new RegExp(`\\b${f}\\.(?:${READ_VERBS})`).test(body)) || /\bdatabase\./.test(body);
  const writesRoom = (body) =>
    fields.some((f) => new RegExp(`\\b${f}\\.(?:${WRITE_VERBS})`).test(body)) || /\bdatabase\./.test(body);
  const callsApi = (body) => /\bapi\./.test(body);

  const methodSignatures = [];
  const sigRe = /^\s*(?:suspend\s+)?fun\s+(\w+)\s*\([^)]*\):\s*([^/\n{]+)/gm;
  let m;
  while ((m = sigRe.exec(interfaceBody)) !== null) {
    methodSignatures.push({ methodName: m[1], returnType: m[2].trim() });
  }

  const findings = [];
  for (const { methodName, returnType } of methodSignatures) {
    const impl = implBody(source, methodName);
    if (impl == null) continue;
    const { body, lineNum } = impl;
    const isObserve = /observe/i.test(methodName);
    const isRefresh = /refresh/i.test(methodName);

    if (isObserve) {
      if (callsApi(body) && !readsRoom(body)) {
        findings.push({
          line: lineNum,
          methodName,
          reason: `observe method '${methodName}' calls api.xxx() but never reads Room (no <dao>.observe()); a screen read must observe a Room Flow`,
        });
      }
    } else if (isRefresh) {
      if (callsApi(body) && !writesRoom(body)) {
        findings.push({
          line: lineNum,
          methodName,
          reason: `refresh method '${methodName}' fetches from api but never writes Room (no <dao>.upsert()); persist the response so observers re-emit`,
        });
      }
    } else if (returnType && !returnType.includes("Flow")) {
      // One-shot get/fetch with no observe counterpart → a screen read that must be Room-backed.
      const counterpart = `observe${methodName.charAt(0).toUpperCase()}${methodName.slice(1)}`;
      const hasCounterpart = methodSignatures.some((s) => s.methodName === counterpart);
      if (callsApi(body) && !readsRoom(body) && !writesRoom(body) && !hasCounterpart) {
        findings.push({
          line: lineNum,
          methodName,
          reason: `screen-facing method '${methodName}' returns api.xxx() directly; needs a Room-backed observe() Flow counterpart`,
        });
      }
    }
  }

  return findings.filter((f) => {
    const l = lines[f.line - 1] || "";
    const p = lines[f.line - 2] || "";
    const n = lines[f.line] || "";
    return !(
      l.includes("offline-first-guard:ignore") ||
      p.includes("offline-first-guard:ignore") ||
      n.includes("offline-first-guard:ignore")
    );
  });
}

// ---- fs / baseline glue --------------------------------------------------------------------

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
    const entries = [];
    for (const line of content.split("\n")) {
      if (line.startsWith("#") || !line.trim()) continue;
      const match = line.match(/^(.+?):(\d+):\s*(.+)$/);
      if (match) entries.push({ path: match[1], line: parseInt(match[2], 10), reason: match[3] });
    }
    return entries;
  } catch {
    return [];
  }
}

function allFindings() {
  const findings = [];
  for (const rel of walkAndroid(resolve(repo, "apps/goatos-android"))) {
    let src;
    try {
      src = readFileSync(resolve(repo, rel), "utf8");
    } catch {
      continue;
    }
    for (const f of findingsForSource(src, rel)) findings.push({ path: rel, ...f });
  }
  return findings;
}

// ---- self-test -----------------------------------------------------------------------------

function selfTest() {
  // Interface methods are on their own lines, matching the real repositories the parser targets.
  const cases = [
    {
      name: "network_only_observe",
      pass: false,
      code: `interface ItemRepository {
    fun observeItems(): Flow<Resource<L>>
}
class DefaultItemRepository(private val api: AppApi) : ItemRepository {
    override fun observeItems(): Flow<Resource<L>> = flow { emit(Resource(api.listItems())) }
}`,
    },
    {
      name: "room_backed_observe_named_dao",
      pass: true,
      code: `interface ItemRepository {
    fun observeItems(): Flow<Resource<L>>
}
class DefaultItemRepository(private val api: AppApi, private val itemCacheDao: ItemDao) : ItemRepository {
    override fun observeItems(): Flow<Resource<L>> = itemCacheDao.observe().map { Resource(it) }
}`,
    },
    {
      // The exact false positive that motivated this rewrite: a non-\`dao\`-named field AND an
      // expression body whose \`.map { }\` lambda brace the old scanner mistook for the fn body.
      name: "room_backed_observe_expression_body_with_map_lambda",
      pass: true,
      code: `interface ItemRepository {
    fun observeTaskDetail(id: String): Flow<Resource<T>>
}
class DefaultItemRepository(private val api: AppApi, private val taskDetailDao: TaskDetailCacheDao) : ItemRepository {
    override fun observeTaskDetail(id: String): Flow<Resource<T>> =
        taskDetailDao.observe(id).map { e -> e.toResource(id) }.flowOn(Dispatchers.Default)
}`,
    },
    {
      name: "refresh_without_upsert",
      pass: false,
      code: `interface ItemRepository {
    suspend fun refreshItems(): Result<Unit>
}
class DefaultItemRepository(private val api: AppApi) : ItemRepository {
    override suspend fun refreshItems(): Result<Unit> = runCatching { api.listItems() }
}`,
    },
    {
      name: "refresh_with_upsert_named_dao",
      pass: true,
      code: `interface ItemRepository {
    suspend fun refreshTimetable(): Boolean
}
class DefaultItemRepository(private val api: AppApi, private val timetableDao: RosterTimetableCacheDao) : ItemRepository {
    override suspend fun refreshTimetable(): Boolean = runCatching {
        val dto = api.timetable(); timetableDao.upsert(dto.toEntity()); true
    }.getOrDefault(false)
}`,
    },
    {
      name: "one_shot_with_observe_counterpart_ok",
      pass: true,
      code: `interface ItemRepository {
    suspend fun taskDetail(id: String): T
    fun observeTaskDetail(id: String): Flow<T>
}
class DefaultItemRepository(private val api: AppApi, private val dao: TaskDao) : ItemRepository {
    override suspend fun taskDetail(id: String): T = api.getTask(id).toDomain()
    override fun observeTaskDetail(id: String): Flow<T> = dao.observe(id).map { it.toDomain() }
}`,
    },
    {
      name: "one_shot_no_counterpart_flagged",
      pass: false,
      code: `interface ItemRepository {
    suspend fun tasks(): List<T>
}
class DefaultItemRepository(private val api: AppApi) : ItemRepository {
    override suspend fun tasks(): List<T> = api.listTasks().map { it.toDomain() }
}`,
    },
    {
      name: "ignore_comment_suppresses",
      pass: true,
      code: `interface ItemRepository {
    fun observeItems(): Flow<Resource<L>>
}
class DefaultItemRepository(private val api: AppApi) : ItemRepository {
    // offline-first-guard:ignore: legacy shim pending migration
    override fun observeItems(): Flow<Resource<L>> = flow { emit(Resource(api.listItems())) }
}`,
    },
  ];

  let failures = 0;
  for (const tc of cases) {
    const hasFinding = findingsForSource(tc.code, "test.kt").length > 0;
    if (hasFinding === tc.pass) {
      failures++;
      console.error(`offline-first-guard self-test ${tc.name}: got ${hasFinding ? "fail" : "pass"}, want ${tc.pass ? "pass" : "fail"}`);
    }
  }
  if (failures > 0) {
    console.error(`offline-first-guard: ${failures} self-test(s) failed`);
    process.exit(1);
  }
  console.log(`offline-first-guard: ${cases.length} self-tests passed`);
}

// ---- main ----------------------------------------------------------------------------------

const mode = process.argv[2];
if (mode === "--self-test") {
  selfTest();
} else if (mode === "--list") {
  const findings = allFindings();
  for (const f of findings) console.log(`${f.path}:${f.line}: ${f.methodName}: ${f.reason}`);
  console.error(`offline-first-guard --list: ${findings.length} total finding(s)`);
} else {
  const findings = allFindings();
  const baselineSet = new Set(readBaseline().map((f) => `${f.path}:${f.line}`));
  const fresh = findings.filter((f) => !baselineSet.has(`${f.path}:${f.line}`));
  if (fresh.length > 0) {
    console.error(`offline-first-guard: ${fresh.length} new offline-first violation(s) (see docs/decisions/android-offline-first.md)`);
    for (const f of fresh) console.error(`  ${f.path}:${f.line}: ${f.methodName}: ${f.reason}`);
    console.error("\nTo suppress a legitimate case, append `offline-first-guard:ignore: <reason>` on the method line.");
    process.exit(1);
  }
  console.log(
    `offline-first-guard: ok (${walkAndroid(resolve(repo, "apps/goatos-android")).length} repositories scanned; ${findings.length} known grandfathered offender(s); 0 new violations)`,
  );
}
