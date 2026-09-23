#!/usr/bin/env node

// check-admin-web-server-client-values.mjs — blocks the App Router defect class where a SERVER
// component imports a plain VALUE (a function or a const, not a component) out of a `"use client"`
// module and calls it during server render.
//
// THE DEFECT THIS EXISTS FOR (PR #295, /tasks). `initials()` and `statusTone()` lived in
// `features/leadership-tasks/leadership-tasks-table.tsx`, a `"use client"` module, and three
// server components imported them across 8 call sites. A function exported from a client module is
// not a function on the server — the bundler replaces it with a client *reference*. The production
// build succeeded, `tsc`, `eslint` and the whole unit suite passed, the API returned 200 with all
// 424 tasks, and the running server then logged 206 × `Attempted to call initials() from the server
// but initials is on the client.` while every user saw "This screen failed to render" (React #419)
// and zero task cards. It was fixed by extracting the helpers into a directive-free
// `task-presentation.ts`. Nothing in the repo's `check:*` suite could see it, which is why this
// guard exists.
//
// THE EXACT RULE, AND ITS BOUNDARY. Importing a client COMPONENT from a server component is how
// the App Router's client boundary is *supposed* to work and must never be flagged. So the rule
// does not ask "is this module a client module" (always true for both cases); it asks what the
// imported binding is and how it is used:
//
//   For every module under apps/admin-web that has NO `"use client"` directive (i.e. a server
//   module), for every static import whose specifier resolves to a file INSIDE apps/admin-web that
//   DOES have `"use client"`, flag the imported binding when:
//
//     (a) client-value-import — the local name is not PascalCase (`initials`, `statusTone`,
//         `TASK_SORTS`, `formatX`). A name that cannot be JSX cannot be a component under the
//         repo's own naming, so the only thing a server module can do with it is call or read it,
//         which is precisely the defect. camelCase and SCREAMING_SNAKE_CASE both land here.
//
//     (b) client-value-call — the local name IS PascalCase, but the server module uses it as a
//         CALL (`Initials(...)`) and never as JSX (`<Initials …>`). A PascalCase export that is
//         invoked rather than rendered is a function wearing a component's name, and calling it on
//         the server fails identically.
//
//     (c) client-namespace-import — `import * as mod from "<client module>"`. The members reached
//         through a namespace object cannot be classified from the import alone, and any
//         `mod.helper()` is the defect; a namespace import is also never how a component is
//         consumed. Import the component by name instead.
//
//   NOT flagged, deliberately: `import type` / inline `type` specifiers (erased before runtime,
//   so they cross the boundary for free), and a PascalCase binding that is rendered as JSX or
//   merely passed along as a value (`<Panel/>`, `permissionSlot={<Push/>}`, `lazy(() =>
//   import("./panel"))`) — the legal, normal client boundary.
//
// BLIND SPOTS, stated rather than implied (the same honesty the sibling guards keep):
//   * NAMING IS THE ORACLE for (a)/(b). A plain helper exported as `Initials` and never called in
//     the same file it is imported into is invisible here; conversely a genuine component exported
//     as `taskCard` would be a false positive. The repo's convention (components PascalCase,
//     helpers camelCase) is what makes this high-confidence, and it is a convention, not a proof.
//   * TEXT SCAN, NOT AST. Regex import parsing: a re-export chain (`server → barrel → client
//     module`) is followed only one hop, through the barrel's own `export … from` lines; deeper
//     laundering, dynamic `await import()`, and `require()` are not seen. `await import()` of a
//     client module is in fact safe on the server for a component and unsafe for a helper — it is
//     out of scope either way.
//   * A `"use server"` module counts as a server module, which is correct (a Server Action body
//     runs on the server) but means an action module that only *forwards* a client type is judged
//     by the same naming rule.
//   * USAGE, NOT REACHABILITY. (a) flags the import even if the binding is never used. That is
//     intentional: an unused client-value import is dead weight that the next edit turns into an
//     outage, and deleting it is free.
//   * Only files under apps/admin-web are scanned, and only imports that resolve inside it. A
//     `"use client"` module inside node_modules or another workspace package is not classified.
//
// Modes:
//   (default)     diff-scoped: only admin-web .ts/.tsx changed vs $ADMIN_WEB_GUARD_BASE (or
//                 origin/main). No admin-web TS changed -> PASS instantly, so an unrelated commit
//                 costs nothing.
//   --all         audit the whole apps/admin-web tree (backlog view).
//   --self-test   run the built-in adversarial fixtures and exit.
//
// Escape hatch: `server-client-boundary:ignore: <reason>` on the offending line or the line above
// it — for the rare binding that is genuinely never evaluated on the server.

import { execSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const ADMIN_WEB = "apps/admin-web";

const isAdminWebTs = (rel) =>
  rel.startsWith(`${ADMIN_WEB}/`) &&
  (rel.endsWith(".ts") || rel.endsWith(".tsx")) &&
  !rel.includes("/node_modules/") &&
  !rel.includes("/.next/") &&
  !/\.(test|spec|mock|seed|stories)\.(ts|tsx)$/.test(rel) &&
  !rel.includes("/__tests__/") &&
  !rel.includes("/__mocks__/");

const USE_CLIENT = /^\s*(?:\/\/[^\n]*\n\s*)*["']use client["']/;

export function hasUseClient(source) {
  // The directive is only a directive at the top of the module, before any statement. Comments and
  // blank lines may precede it; an occurrence further down (a string in a test fixture, a doc
  // comment quoting it) must not count.
  const head = String(source || "").replace(/^\uFEFF/, "");
  const firstStatement = head.search(/[^\s]/);
  if (firstStatement < 0) return false;
  const stripped = head
    .split("\n")
    .reduce(
      (acc, line) => {
        if (acc.done) return acc;
        const trimmed = line.trim();
        if (trimmed === "" || trimmed.startsWith("//")) return acc;
        if (acc.inBlock) {
          if (trimmed.includes("*/")) acc.inBlock = false;
          return acc;
        }
        if (trimmed.startsWith("/*")) {
          if (!trimmed.includes("*/")) acc.inBlock = true;
          return acc;
        }
        acc.first = trimmed;
        acc.done = true;
        return acc;
      },
      { first: "", done: false, inBlock: false },
    ).first;
  return /^["']use client["']\s*;?$/.test(stripped) || USE_CLIENT.test(stripped);
}

/** Parse the static import declarations of a module. Text scan — see the blind spots above. */
export function parseImports(source) {
  const out = [];
  const re = /import\s+(type\s+)?([\s\S]*?)\s+from\s*["']([^"']+)["']/g;
  let m;
  while ((m = re.exec(source)) !== null) {
    const typeOnly = Boolean(m[1]);
    const clause = m[2].trim();
    const specifier = m[3];
    const line = source.slice(0, m.index).split("\n").length;
    const specifiers = [];
    const namespace = clause.match(/^\*\s+as\s+([A-Za-z0-9_$]+)$/);
    if (namespace) {
      specifiers.push({ kind: "namespace", local: namespace[1], typeOnly });
    } else {
      const braces = clause.match(/\{([\s\S]*)\}/);
      const beforeBraces = braces ? clause.slice(0, clause.indexOf("{")) : clause;
      const defaultName = beforeBraces.replace(/,/g, "").trim();
      if (defaultName && /^[A-Za-z0-9_$]+$/.test(defaultName)) {
        specifiers.push({ kind: "default", local: defaultName, typeOnly });
      }
      if (braces) {
        for (const raw of braces[1].split(",")) {
          const piece = raw.trim();
          if (!piece) continue;
          const inlineType = /^type\s+/.test(piece);
          const named = piece.replace(/^type\s+/, "");
          const parts = named.split(/\s+as\s+/);
          const local = (parts[1] || parts[0]).trim();
          if (!/^[A-Za-z0-9_$]+$/.test(local)) continue;
          specifiers.push({
            kind: "named",
            imported: parts[0].trim(),
            local,
            typeOnly: typeOnly || inlineType,
          });
        }
      }
    }
    out.push({ specifier, line, specifiers });
  }
  return out;
}

const isPascal = (name) => /^[A-Z][A-Za-z0-9_$]*$/.test(name) && !/^[A-Z0-9_$]+$/.test(name);

/**
 * Resolve an import specifier to a repo-relative file under apps/admin-web, or null.
 * `@/x` is admin-web's tsconfig alias for `apps/admin-web/x`.
 */
export function resolveSpecifier(specifier, fromRel, { exists = (rel) => existsSync(join(repo, rel)), isDir = (rel) => { try { return statSync(join(repo, rel)).isDirectory(); } catch { return false; } } } = {}) {
  let base;
  if (specifier.startsWith("@/")) base = `${ADMIN_WEB}/${specifier.slice(2)}`;
  else if (specifier.startsWith(".")) base = relative(repo, resolve(repo, dirname(fromRel), specifier)).split("\\").join("/");
  else return null;
  if (!base.startsWith(`${ADMIN_WEB}/`)) return null;
  const candidates = [];
  if (/\.(ts|tsx)$/.test(base)) candidates.push(base);
  else {
    candidates.push(`${base}.ts`, `${base}.tsx`);
    if (isDir(base)) candidates.push(`${base}/index.ts`, `${base}/index.tsx`);
  }
  for (const candidate of candidates) if (exists(candidate)) return candidate;
  return null;
}

/**
 * Where does `name`, as exported by `rel`, actually live? One hop through a barrel's
 * `export { X } from "./y"` re-export, which is how this app's features are consumed
 * (`@/features/notifications` is a barrel and check-boundaries.sh requires it).
 */
function originOfExport(rel, name, read) {
  const source = read(rel);
  if (source == null) return { rel, source: null };
  if (hasUseClient(source)) return { rel, source };
  const re = /export\s*\{([^}]*)\}\s*from\s*["']([^"']+)["']/g;
  let m;
  while ((m = re.exec(source)) !== null) {
    const names = m[1].split(",").map((piece) => {
      const cleaned = piece.trim().replace(/^type\s+/, "");
      const parts = cleaned.split(/\s+as\s+/);
      return { local: (parts[1] || parts[0] || "").trim(), typeOnly: /^type\s+/.test(piece.trim()) };
    });
    const hit = names.find((entry) => entry.local === name);
    if (!hit || hit.typeOnly) continue;
    const target = resolveSpecifier(m[2], rel);
    if (!target) continue;
    const targetSource = read(target);
    if (targetSource == null) continue;
    return { rel: target, source: targetSource };
  }
  return { rel, source };
}

/**
 * The whole rule, with every file read injected so the self-test can build a virtual tree.
 * `files` maps repo-relative path -> source text.
 */
export function findingsForModule(rel, source, read) {
  if (hasUseClient(source)) return []; // a client module importing client values is fine
  const lines = source.split("\n");
  const ignored = (lineNo) =>
    /server-client-boundary:ignore/.test(lines[lineNo - 1] || "") ||
    /server-client-boundary:ignore/.test(lines[lineNo - 2] || "");

  const findings = [];
  for (const decl of parseImports(source)) {
    const resolved = resolveSpecifier(decl.specifier, rel);
    if (!resolved) continue;
    for (const spec of decl.specifiers) {
      if (spec.typeOnly) continue;
      const origin =
        spec.kind === "named" ? originOfExport(resolved, spec.imported, read) : { rel: resolved, source: read(resolved) };
      if (origin.source == null || !hasUseClient(origin.source)) continue;
      if (ignored(decl.line)) continue;
      const where = origin.rel === resolved ? origin.rel : `${resolved} -> ${origin.rel}`;
      if (spec.kind === "namespace") {
        findings.push({
          line: decl.line,
          rule: "client-namespace-import",
          message:
            `\`import * as ${spec.local}\` from the "use client" module ${where}: every member reached ` +
            "through it is a client reference on the server. Import the component by name, or move the " +
            "helpers into a directive-free module.",
        });
        continue;
      }
      if (!isPascal(spec.local)) {
        findings.push({
          line: decl.line,
          rule: "client-value-import",
          message:
            `\`${spec.local}\` is imported from the "use client" module ${where} into a server module. ` +
            "A non-component export of a client module is a client reference, not a callable value: " +
            `calling it during server render fails at runtime with "Attempted to call ${spec.local}() from ` +
            'the server". Extract it into a directive-free module (the task-presentation.ts pattern).',
        });
        continue;
      }
      const calledAsFunction = new RegExp(`(?<![.\\w$])${spec.local}\\s*\\(`).test(source);
      const renderedAsJsx = new RegExp(`<${spec.local}[\\s/>]`).test(source);
      if (calledAsFunction && !renderedAsJsx) {
        findings.push({
          line: decl.line,
          rule: "client-value-call",
          message:
            `\`${spec.local}\` comes from the "use client" module ${where} and is CALLED (not rendered as ` +
            "JSX) by this server module. A PascalCase export that is invoked is a plain function, and " +
            "invoking a client reference on the server fails at runtime. Extract it into a directive-free " +
            "module, or render it as a component.",
        });
      }
    }
  }
  return findings;
}

function walkTree(dir, keep) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (["node_modules", ".next", "build", ".turbo"].includes(entry.name)) continue;
      out.push(...walkTree(path, keep));
    } else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (keep(rel)) out.push(rel);
    }
  }
  return out;
}

// WORKING TREE COUNTS. `<base>...HEAD` alone sees only COMMITTED history, so a
// violation an agent just wrote — the exact state a PostToolUse/pre-commit hook
// runs in — was invisible and this guard exited 0. Measured 2026-09-23.
// Staged and unstaged diffs are unioned in, the same three sources
// changed_since_base() uses in tools/ci/run-local-ci.sh. Two extra cheap git
// calls; the scan stays diff-scoped and never walks the tree.
function changedFiles() {
  const base = process.env.ADMIN_WEB_GUARD_BASE || "origin/main";
  const found = new Set();
  let resolved = false;
  for (const range of [`${base}...HEAD`, "HEAD~1...HEAD"]) {
    try {
      execSync(`git rev-parse --verify --quiet ${range.split("...")[0]}^{commit}`, { cwd: repo, stdio: "ignore" });
      for (const s of execSync(`git diff --name-only --diff-filter=d ${range}`, { cwd: repo, encoding: "utf8" }).split("\n")) {
        if (s.trim()) found.add(s.trim());
      }
      resolved = true;
      break;
    } catch {
      /* try the next range */
    }
  }
  for (const args of ["diff --name-only --diff-filter=d --cached", "diff --name-only --diff-filter=d"]) {
    try {
      for (const s of execSync(`git ${args}`, { cwd: repo, encoding: "utf8" }).split("\n")) {
        if (s.trim()) found.add(s.trim());
      }
      resolved = true;
    } catch {
      /* ignore */
    }
  }
  if (!resolved) return null;
  return [...found].filter(isAdminWebTs);
}

// A changed file is not the only way this defect appears: moving a helper INTO a `"use client"`
// module breaks the untouched server modules that import it. So when a client module changed, its
// in-tree importers are pulled into scope too.
function importersOf(targets, files) {
  const hits = new Set();
  for (const rel of files) {
    let source;
    try {
      source = readFileSync(join(repo, rel), "utf8");
    } catch {
      continue;
    }
    for (const decl of parseImports(source)) {
      const resolved = resolveSpecifier(decl.specifier, rel);
      if (resolved && targets.has(resolved)) hits.add(rel);
    }
  }
  return hits;
}

function selfTest() {
  // A virtual tree, so the fixtures are adversarial rather than whatever happens to be on disk.
  const tree = {
    // The real defect, reproduced: a client module exporting both a component and two helpers.
    [`${ADMIN_WEB}/features/leadership-tasks/leadership-tasks-table.tsx`]:
      '"use client";\nexport function initials(name) { return name[0]; }\nexport function statusTone(s) { return s; }\nexport function LeadershipTasksTable() { return null; }\n',
    [`${ADMIN_WEB}/features/leadership-tasks/task-presentation.ts`]:
      "export function initials(name) { return name[0]; }\nexport function statusTone(s) { return s; }\n",
    [`${ADMIN_WEB}/features/notifications/notification-bell.tsx`]:
      '"use client";\nexport function NotificationBell() { return null; }\nexport function notificationBadgeCount() { return 0; }\n',
    [`${ADMIN_WEB}/features/notifications/index.ts`]:
      'export { NotificationBell, notificationBadgeCount } from "./notification-bell";\n',
    [`${ADMIN_WEB}/features/notifications/model.ts`]: "export const EMPTY = {};\n",
  };
  const read = (rel) => (rel in tree ? tree[rel] : null);
  const rel = `${ADMIN_WEB}/features/leadership-tasks/task-board-card.tsx`;
  const check = (source) => findingsForModule(rel, source, read);
  const rules = (source) => check(source).map((f) => f.rule);

  const bad = [
    // 1. THE PRODUCTION DEFECT, verbatim in shape.
    [
      "client-value-import",
      'import { initials } from "./leadership-tasks-table";\nexport function TaskBoardCard({ t }) { return <span>{initials(t.owner)}</span>; }\n',
    ],
    // 2. Two helpers at once, and a legal component import in the SAME declaration: the component
    //    must not be flagged and the helpers must be.
    [
      "client-value-import",
      'import { LeadershipTasksTable, initials, statusTone } from "./leadership-tasks-table";\nexport function P() { return <LeadershipTasksTable tone={statusTone("open")} who={initials("a")} />; }\n',
    ],
    // 3. SCREAMING_SNAKE const — not PascalCase, still a value.
    [
      "client-value-import",
      'import { STATUS_TONES } from "./leadership-tasks-table";\nexport function P() { return <i>{STATUS_TONES.open}</i>; }\n',
    ],
    // 4. A helper laundered through the feature BARREL (one re-export hop).
    [
      "client-value-import",
      'import { notificationBadgeCount } from "@/features/notifications";\nexport function P() { return <b>{notificationBadgeCount()}</b>; }\n',
    ],
    // 5. PascalCase but CALLED, never rendered: a function wearing a component's name.
    [
      "client-value-call",
      'import { LeadershipTasksTable } from "./leadership-tasks-table";\nexport function P() { return LeadershipTasksTable({ rows: [] }); }\n',
    ],
    // 6. Namespace import of a client module.
    [
      "client-namespace-import",
      'import * as table from "./leadership-tasks-table";\nexport function P() { return <i>{table.initials("a")}</i>; }\n',
    ],
    // 7. Alias does not launder it: `initials as fmt`.
    [
      "client-value-import",
      'import { initials as fmt } from "./leadership-tasks-table";\nexport function P() { return <i>{fmt("a")}</i>; }\n',
    ],
    // 8. The `@/`-alias spelling of the same defect.
    [
      "client-value-import",
      'import { initials } from "@/features/leadership-tasks/leadership-tasks-table";\nexport function P() { return <i>{initials("a")}</i>; }\n',
    ],
    // 9. A "use server" action module is a server module too.
    [
      "client-value-import",
      '"use server";\nimport { initials } from "./leadership-tasks-table";\nexport async function a(n) { return initials(n); }\n',
    ],
    // 10. The directive quoted inside a doc comment must not make this module look like a client
      //     module and thereby exempt itself.
    [
      "client-value-import",
      '/**\n * Mounted under a "use client" shell.\n */\nimport { initials } from "./leadership-tasks-table";\nexport function P() { return <i>{initials("a")}</i>; }\n',
    ],
  ];
  for (const [rule, source] of bad) {
    if (!rules(source).includes(rule)) {
      throw new Error(`self-test: '${rule}' not flagged for: ${source.split("\n")[0]}`);
    }
  }
  // Fixture 2 must flag exactly the two helpers and NOT the component.
  const mixed = check(bad[1][1]);
  if (mixed.length !== 2 || !mixed.every((f) => f.rule === "client-value-import")) {
    throw new Error(`self-test: mixed import should flag exactly the 2 helpers, got ${JSON.stringify(mixed)}`);
  }

  const good = [
    // A. THE LEGAL CLIENT BOUNDARY: a server component rendering a client component. The case this
    //    guard exists to leave alone — if this ever flags, the App Router is unusable.
    'import { LeadershipTasksTable } from "./leadership-tasks-table";\nexport function Page() { return <LeadershipTasksTable rows={[]} />; }\n',
    // B. The same, through the barrel, and passed as a prop element rather than rendered directly.
    'import { NotificationBell } from "@/features/notifications";\nexport function Shell() { return <NotificationBell permissionSlot={<NotificationBell />} />; }\n',
    // C. The FIX for the real defect: the helpers now come from a directive-free module.
    'import { initials, statusTone } from "./task-presentation";\nexport function P({ t }) { return <i className={statusTone(t.s)}>{initials(t.owner)}</i>; }\n',
    // D. A type-only import of a client module — erased before runtime.
    'import type { TableProps } from "./leadership-tasks-table";\nexport function P(p: TableProps) { return <i /> ; }\n',
    // E. Inline `type` specifier beside a legal component import.
    'import { LeadershipTasksTable, type TableProps } from "./leadership-tasks-table";\nexport function P(p: TableProps) { return <LeadershipTasksTable {...p} />; }\n',
    // F. A client module is allowed to import client values (out of scope by the directive).
    '"use client";\nimport { initials } from "./leadership-tasks-table";\nexport function P() { return <i>{initials("a")}</i>; }\n',
    // G. Imports that resolve to non-client modules are irrelevant.
    'import { EMPTY } from "@/features/notifications/model";\nexport function P() { return <i>{String(EMPTY)}</i>; }\n',
    // H. A bare package import is never classified (node_modules is out of scope).
    'import { Bell } from "lucide-react";\nexport function P() { return <Bell />; }\n',
    // I. The escape hatch.
    '// server-client-boundary:ignore: handed to a client child untouched, never evaluated here\nimport { initials } from "./leadership-tasks-table";\nexport const fn = initials;\n',
    // J. A PascalCase component that is only forwarded as a value, never called: legal (this is
    //    what a lazy wrapper or a component map looks like).
    'import { LeadershipTasksTable } from "./leadership-tasks-table";\nexport const REGISTRY = { table: LeadershipTasksTable };\n',
  ];
  for (const source of good) {
    const f = check(source);
    if (f.length) {
      throw new Error(`self-test: false positive on: ${source.split("\n")[0]} -> ${JSON.stringify(f)}`);
    }
  }

  // The directive detector itself, since every rule hangs off it.
  for (const source of ['"use client";\n', "\n\n'use client'\n", '// c\n\n"use client";\n', '/* x */\n"use client";\n']) {
    if (!hasUseClient(source)) throw new Error(`self-test: hasUseClient missed ${JSON.stringify(source)}`);
  }
  for (const source of ['const s = "use client";\n', 'export const x = 1;\n"use client";\n', ""]) {
    if (hasUseClient(source)) throw new Error(`self-test: hasUseClient false positive on ${JSON.stringify(source)}`);
  }

  console.log("admin-web-server-client-values self-test: ok (10 adversarial defects flagged, 10 legal shapes clean)");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const all = process.argv.includes("--all");
const tree = walkTree(join(repo, ADMIN_WEB), isAdminWebTs);
let files;
if (all) {
  files = tree;
} else {
  const changed = changedFiles();
  if (changed === null) {
    console.log("admin-web-server-client-values: skipped (no git diff base; run with --all to audit the whole tree)");
    process.exit(0);
  }
  if (changed.length === 0) {
    console.log("admin-web-server-client-values: ok (no admin-web TS changed)");
    process.exit(0);
  }
  const changedClient = new Set(
    changed.filter((rel) => {
      try {
        return hasUseClient(readFileSync(join(repo, rel), "utf8"));
      } catch {
        return false;
      }
    }),
  );
  files = [...new Set([...changed, ...importersOf(changedClient, tree)])];
}

const read = (rel) => {
  try {
    return readFileSync(join(repo, rel), "utf8");
  } catch {
    return null;
  }
};

const findings = [];
for (const rel of files) {
  const source = read(rel);
  if (source == null) continue;
  for (const f of findingsForModule(rel, source, read)) findings.push({ ...f, rel });
}

if (findings.length) {
  console.error(
    `admin-web-server-client-values: ${findings.length} server->client value import(s) — these fail at ` +
      "RUNTIME in a production build while tsc, eslint and the unit suite stay green",
  );
  for (const f of findings) console.error(`- ${f.rule} ${f.rel}:${f.line}: ${f.message}`);
  console.error(
    "Fix: move the non-component exports into a module with no \"use client\" directive (see " +
      "apps/admin-web/features/leadership-tasks/task-presentation.ts). If a case is genuinely never " +
      "evaluated on the server, append `server-client-boundary:ignore: <reason>` on the import line.",
  );
  process.exit(1);
}
console.log(
  `admin-web-server-client-values: ok (${files.length} admin-web module(s) scanned; no server module calls into a "use client" module)`,
);
