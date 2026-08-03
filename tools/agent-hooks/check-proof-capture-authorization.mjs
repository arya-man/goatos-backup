#!/usr/bin/env node

// check-proof-capture-authorization.mjs — proof capture is authorized by the SAME execution
// right that authorizes the work it proves.
//
// THE DEFECT CLASS (shipped 2026-08-03, phone-QA):
//   Every /app/proofs write route was gated on `TaskExecute`, the OPERATOR's general
//   task-execution grant, because proof capture grew up inside the vaccination/SOP flow.
//   Weighing later grew its OWN vertical execute permission, `WeighingExecute`. A Growth
//   Director holds WeighingExecute (and deliberately NOT TaskExecute, which would hand him
//   vaccination SOP submission). Result: he was authorized to POST a weighing observation but
//   403'd on POST /app/proofs/uploads, so no proof could ever reach SYNCED, and lump-sum
//   Submit — which requires a synced proof — was permanently disabled. He could do the work
//   and could never prove it. Nothing in CI noticed, because each half was individually
//   correct: the role map was right, the route table was right, only the JOIN between them
//   was wrong.
//
// THE RULE THIS ENFORCES:
//   If a role can execute a vertical's work, it can complete that work's evidence. Concretely:
//   every role holding ANY `*Execute` permission must be authorized for every proof-capture
//   write route. This is the check that must exist before the next vertical (breeding, feed,
//   procurement) grows its own execute permission and repeats the defect.
//
// It is a STRUCTURAL check, not a token grep: it parses the role->permission maps out of
// permissions.go and permissions_orgrole.go and the route table out of routes.go, then
// re-implements AuthorizeRoute's exact AND/OR semantics (Permissions is ANDed, AnyPermissions
// is ORed, both must hold when both are present) over the parsed data. Adding a new execute
// permission, a new role, or a new proof route is picked up with no edit to this file.
//
// Modes:
//   (default)     check the real backend/internal/permissions tree.
//   --self-test   run adversarial fixtures (including a reconstruction of the exact shipped
//                 defect) and exit.
//
// BLIND SPOTS — native Read/Grep must still catch these:
//   * Permission sets built at RUNTIME by anything other than the two map literals parsed here
//     (e.g. a grant assembled from the database, or a future init() that mutates
//     rolePermissions after composition). The 2026-08-01 permissions_orgrole.go override that
//     silently shadowed growth_director was exactly this shape; it is gone, and registerRole
//     now panics on re-declaration, but a new dynamic path would be invisible to this guard.
//   * Handler-level authorization INSIDE a proof endpoint (a second gate beyond the route
//     table). Today the proof handlers have none; if one is added, route-level authorization
//     stops being sufficient and this guard would pass a broken path.
//   * Scope/tenant/park narrowing. This guard answers "may this role reach the route at all",
//     never "may it reach THIS shed's proof".
//   * Evidence surfaces that are not /app/proofs write routes (a future signature, photo, or
//     weight-slip endpoint) — PROOF_ROUTE_PATTERN below would need widening.
//   * AdminOnly routes are skipped: their semantics short-circuit the permission lists.

import { readFileSync, existsSync, mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";

const repo = process.env.PROOF_AUTHZ_GUARD_TEST_REPO
  ? resolve(process.env.PROOF_AUTHZ_GUARD_TEST_REPO)
  : resolve(import.meta.dirname, "../..");

const PERMISSIONS_DIR = "backend/internal/permissions";
// Proof-capture WRITE routes. GET is a read (downloadProof) and is gated on a read
// permission by design, so it is deliberately out of scope.
const PROOF_ROUTE_PATTERN = /^\/app\/proofs(\/|$)/;
const WRITE_METHODS = new Set(["POST", "PUT", "PATCH", "DELETE"]);

/** Returns the substring of `source` inside the brace pair that opens at `openIdx`. */
function braceBody(source, openIdx) {
  let depth = 0;
  for (let i = openIdx; i < source.length; i += 1) {
    const ch = source[i];
    if (ch === "{") depth += 1;
    else if (ch === "}") {
      depth -= 1;
      if (depth === 0) return source.slice(openIdx + 1, i);
    }
  }
  return null;
}

/**
 * Parses a Go `map[string]map[string]struct{}{ Key: { PermA: {}, PermB: {} }, ... }` literal
 * into { key -> Set(permission identifiers) }. Keys and permissions are Go IDENTIFIERS
 * (RoleGrowthDirector, WeighingExecute), which is exactly the vocabulary routes.go uses, so no
 * constant-value resolution is needed.
 */
export function parsePermissionMap(source, varName) {
  const anchor = new RegExp(`${varName}\\s*=\\s*map\\[string\\]map\\[string\\]struct\\{\\}\\{`);
  const m = anchor.exec(source);
  if (!m) return {};
  const openIdx = source.indexOf("{", m.index + m[0].length - 1);
  const body = braceBody(source, openIdx);
  if (body == null) return {};

  const out = {};
  let i = 0;
  while (i < body.length) {
    // Skip comments and whitespace so a commented-out key never registers.
    if (body.startsWith("//", i)) {
      i = body.indexOf("\n", i);
      if (i < 0) break;
      continue;
    }
    const keyMatch = /^[\s,]*([A-Za-z_][A-Za-z0-9_]*)\s*:\s*\{/.exec(body.slice(i));
    if (!keyMatch) {
      i += 1;
      continue;
    }
    const key = keyMatch[1];
    const innerOpen = i + keyMatch[0].length - 1;
    const inner = braceBody(body, innerOpen);
    if (inner == null) break;
    const perms = new Set();
    // Strip comments before harvesting identifiers, so prose in a comment ("NOT WeighingPlan")
    // can never be read as a granted permission.
    const cleaned = inner.replace(/\/\/[^\n]*/g, "");
    for (const pm of cleaned.matchAll(/([A-Za-z_][A-Za-z0-9_]*)\s*:\s*\{\s*\}/g)) {
      perms.add(pm[1]);
    }
    out[key] = perms;
    i = innerOpen + inner.length + 2;
  }
  return out;
}

/** Extracts every `SomethingExecute = "..."` permission constant identifier. */
export function parseExecutePermissions(source) {
  const out = new Set();
  for (const m of source.matchAll(/\b([A-Za-z_][A-Za-z0-9_]*Execute)\s*=\s*"/g)) {
    out.add(m[1]);
  }
  return out;
}

/** Parses the protectedRoutes table into route records. */
export function parseRoutes(source) {
  const routes = [];
  for (const m of source.matchAll(/\{OperationID:\s*"([^"]+)"[\s\S]*?\},\n/g)) {
    const entry = m[0];
    const method = /Method:\s*"([^"]+)"/.exec(entry)?.[1] ?? "";
    const pattern = /Pattern:\s*"([^"]+)"/.exec(entry)?.[1] ?? "";
    const listOf = (name) => {
      const list = new RegExp(`(?<!Any)${name}:\\s*\\[\\]string\\{([^}]*)\\}`).exec(entry)?.[1];
      if (!list) return [];
      return [...list.matchAll(/([A-Za-z_][A-Za-z0-9_]*)/g)].map((x) => x[1]);
    };
    const anyList = () => {
      const list = /AnyPermissions:\s*\[\]string\{([^}]*)\}/.exec(entry)?.[1];
      if (!list) return [];
      return [...list.matchAll(/([A-Za-z_][A-Za-z0-9_]*)/g)].map((x) => x[1]);
    };
    routes.push({
      operationID: m[1],
      method,
      pattern,
      permissions: listOf("Permissions"),
      anyPermissions: anyList(),
      adminOnly: /AdminOnly:\s*true/.test(entry),
    });
  }
  return routes;
}

/** Mirrors permissions.AuthorizeRoute: Permissions ANDed, AnyPermissions ORed, both must hold. */
export function authorizeRoute(route, held) {
  if (route.permissions.length === 0 && route.anyPermissions.length === 0) return false;
  if (route.permissions.length > 0 && !route.permissions.every((p) => held.has(p))) return false;
  if (route.anyPermissions.length > 0 && !route.anyPermissions.some((p) => held.has(p))) return false;
  return true;
}

export function findings({ rolePermissions, executePermissions, routes }) {
  const proofRoutes = routes.filter(
    (r) => PROOF_ROUTE_PATTERN.test(r.pattern) && WRITE_METHODS.has(r.method) && !r.adminOnly,
  );
  const out = [];
  if (proofRoutes.length === 0) {
    out.push({
      role: "(none)",
      message:
        "no /app/proofs write route found in protectedRoutes — the proof-capture surface moved or " +
        "became unprotected; this guard cannot enforce anything and must be updated",
    });
    return out;
  }
  for (const [role, held] of Object.entries(rolePermissions).sort(([a], [b]) => a.localeCompare(b))) {
    const executes = [...executePermissions].filter((p) => held.has(p)).sort();
    if (executes.length === 0) continue;
    for (const route of proofRoutes) {
      if (authorizeRoute(route, held)) continue;
      const missing = route.anyPermissions.length > 0
        ? `any of [${route.anyPermissions.join(", ")}]`
        : `all of [${route.permissions.join(", ")}]`;
      out.push({
        role,
        message:
          `${role} executes work (holds ${executes.join(", ")}) but is NOT authorized for ` +
          `${route.operationID} (${route.method} ${route.pattern}), which requires ${missing}. ` +
          "It can do the work and can never prove it, so every write that mandates evidence is " +
          "permanently unsubmittable for this role. Fix the ROUTE (accept the vertical's execute " +
          "permission via AnyPermissions), not the role — handing it the other vertical's broad " +
          "task.execute is a privilege escalation dressed as a bug fix.",
      });
    }
  }
  return out;
}

function loadRepo(root) {
  const permissionsPath = join(root, PERMISSIONS_DIR, "permissions.go");
  const orgRolePath = join(root, PERMISSIONS_DIR, "permissions_orgrole.go");
  const routesPath = join(root, PERMISSIONS_DIR, "routes.go");
  for (const p of [permissionsPath, routesPath]) {
    if (!existsSync(p)) {
      console.error(`proof-capture-authorization guard: expected ${p} to exist`);
      process.exit(1);
    }
  }
  const permissionsSrc = readFileSync(permissionsPath, "utf8");
  const routesSrc = readFileSync(routesPath, "utf8");
  const orgRoleSrc = existsSync(orgRolePath) ? readFileSync(orgRolePath, "utf8") : "";

  const rolePermissions = parsePermissionMap(permissionsSrc, "rolePermissions");
  // Org-tier roles are composed from tierPermissions at init(); a tier holding an execute
  // permission would produce composite roles with the same gap, so it is checked under its
  // tier name.
  const tierPermissions = parsePermissionMap(orgRoleSrc, "tierPermissions");
  for (const [tier, perms] of Object.entries(tierPermissions)) {
    rolePermissions[`tier:${tier}`] = perms;
  }
  return {
    rolePermissions,
    executePermissions: parseExecutePermissions(permissionsSrc),
    routes: parseRoutes(routesSrc),
  };
}

function run() {
  const parsed = loadRepo(repo);
  if (Object.keys(parsed.rolePermissions).length === 0) {
    console.error(
      "proof-capture-authorization guard: parsed ZERO roles from permissions.go — the map literal " +
        "shape changed and this guard is no longer checking anything. Fix the parser.",
    );
    process.exit(1);
  }
  if (parsed.executePermissions.size === 0) {
    console.error(
      "proof-capture-authorization guard: parsed ZERO *Execute permissions — the constant naming " +
        "convention changed and this guard is no longer checking anything. Fix the parser.",
    );
    process.exit(1);
  }
  const violations = findings(parsed);
  if (violations.length > 0) {
    console.error("proof-capture-authorization guard FAILED:\n");
    for (const v of violations) console.error(`  - ${v.message}\n`);
    console.error(
      "Rule: proof/evidence capture is authorized by the SAME execution right that authorizes the " +
        "work. See docs/decisions/proof-capture-authorization.md.",
    );
    process.exit(1);
  }
  console.log(
    `proof-capture-authorization guard: OK (${Object.keys(parsed.rolePermissions).length} roles, ` +
      `${parsed.executePermissions.size} execute permissions checked against the proof-capture routes)`,
  );
}

// ---------------------------------------------------------------------------
// Self-test
// ---------------------------------------------------------------------------

const CLEAN_PERMISSIONS_GO = `package permissions

const (
	TaskExecute     = "task.execute"
	WeighingExecute = "weighing.execute"
	TaskRead        = "task.read"
)

var rolePermissions = map[string]map[string]struct{}{
	RoleOperator: {
		TaskExecute: {}, TaskRead: {}, WeighingExecute: {},
	},
	RoleGrowthDirector: {
		// NOT TaskExecute: that would grant vaccination SOP submission.
		WeighingExecute: {}, TaskRead: {},
	},
	RoleVerifier: {
		TaskRead: {},
	},
}
`;

const CLEAN_ROUTES_GO = `package permissions

var protectedRoutes = []Route{
	{OperationID: "createProofUpload", Method: "POST", Pattern: "/app/proofs/uploads", AnyPermissions: []string{TaskExecute, WeighingExecute}},
	{OperationID: "completeProofUpload", Method: "POST", Pattern: "/app/proofs/{proof_id}/complete", AnyPermissions: []string{TaskExecute, WeighingExecute}},
	{OperationID: "downloadProof", Method: "GET", Pattern: "/app/proofs/{proof_id}/download", Permissions: []string{TaskRead}},
	{OperationID: "submitAppTask", Method: "POST", Pattern: "/app/tasks/{task_id}/submissions", Permissions: []string{TaskExecute}},
}
`;

// The EXACT shipped defect: proof routes gated on TaskExecute alone, while a role holds only
// the vertical's WeighingExecute.
const SHIPPED_DEFECT_ROUTES_GO = CLEAN_ROUTES_GO.replace(
  /AnyPermissions: \[\]string\{TaskExecute, WeighingExecute\}/g,
  "Permissions: []string{TaskExecute}",
);

function writeFixture(files) {
  const dir = mkdtempSync(join(tmpdir(), "proof-authz-guard-"));
  mkdirSync(join(dir, PERMISSIONS_DIR), { recursive: true });
  for (const [name, content] of Object.entries(files)) {
    writeFileSync(join(dir, PERMISSIONS_DIR, name), content);
  }
  return dir;
}

function assert(condition, label) {
  if (!condition) {
    console.error(`proof-capture-authorization guard SELF-TEST FAILED: ${label}`);
    process.exit(1);
  }
}

function selfTest() {
  // Parser sanity — a guard that parses nothing passes everything.
  const roles = parsePermissionMap(CLEAN_PERMISSIONS_GO, "rolePermissions");
  assert(Object.keys(roles).length === 3, "expected 3 roles parsed from the clean fixture");
  assert(roles.RoleGrowthDirector.has("WeighingExecute"), "growth director must parse as holding WeighingExecute");
  assert(
    !roles.RoleGrowthDirector.has("TaskExecute"),
    "a comment mentioning TaskExecute must NOT parse as a grant",
  );
  const execs = parseExecutePermissions(CLEAN_PERMISSIONS_GO);
  assert(execs.has("TaskExecute") && execs.has("WeighingExecute"), "both execute permissions must parse");
  const routes = parseRoutes(CLEAN_ROUTES_GO);
  assert(routes.length === 4, `expected 4 routes parsed, got ${routes.length}`);
  const create = routes.find((r) => r.operationID === "createProofUpload");
  assert(create.anyPermissions.length === 2, "createProofUpload must parse its AnyPermissions list");
  assert(create.permissions.length === 0, "AnyPermissions must not be mis-parsed as Permissions");

  // CLEAN fixture: no findings.
  const clean = findings({
    rolePermissions: roles,
    executePermissions: execs,
    routes,
  });
  assert(clean.length === 0, `clean fixture must produce no findings, got ${clean.length}`);

  // ADVERSARIAL fixture: the exact shipped defect must be caught.
  const defect = findings({
    rolePermissions: roles,
    executePermissions: execs,
    routes: parseRoutes(SHIPPED_DEFECT_ROUTES_GO),
  });
  assert(defect.length > 0, "the shipped defect (WeighingExecute role vs TaskExecute-only proof routes) MUST be caught");
  assert(
    defect.every((f) => f.role === "RoleGrowthDirector"),
    "only the role that actually lacks access may be flagged (operator holds TaskExecute and is fine)",
  );

  // ADVERSARIAL fixture 2: a NEW vertical grows an execute permission and forgets proof.
  const futureVertical = CLEAN_PERMISSIONS_GO
    .replace('TaskRead        = "task.read"', 'TaskRead        = "task.read"\n\tBreedingExecute = "breeding.execute"')
    .replace("RoleVerifier: {\n\t\tTaskRead: {},", "RoleVerifier: {\n\t\tTaskRead: {}, BreedingExecute: {},");
  const futureRoles = parsePermissionMap(futureVertical, "rolePermissions");
  const futureExecs = parseExecutePermissions(futureVertical);
  assert(futureExecs.has("BreedingExecute"), "a newly added *Execute constant must be discovered automatically");
  const futureFindings = findings({
    rolePermissions: futureRoles,
    executePermissions: futureExecs,
    routes,
  });
  assert(
    futureFindings.some((f) => f.role === "RoleVerifier"),
    "a new vertical's executor with no proof access MUST be caught with no edit to this guard",
  );

  // End-to-end through the real file loader, so a parser/loader mismatch cannot hide.
  const defectRepo = writeFixture({
    "permissions.go": CLEAN_PERMISSIONS_GO,
    "routes.go": SHIPPED_DEFECT_ROUTES_GO,
  });
  try {
    const loaded = loadRepo(defectRepo);
    assert(findings(loaded).length > 0, "loader path must also catch the shipped defect");
  } finally {
    rmSync(defectRepo, { recursive: true, force: true });
  }

  const cleanRepo = writeFixture({
    "permissions.go": CLEAN_PERMISSIONS_GO,
    "routes.go": CLEAN_ROUTES_GO,
  });
  try {
    const loaded = loadRepo(cleanRepo);
    assert(findings(loaded).length === 0, "loader path must pass the clean fixture");
  } finally {
    rmSync(cleanRepo, { recursive: true, force: true });
  }

  console.log(
    "proof-capture-authorization guard: self-test passed " +
      "(clean=0 findings, shipped defect caught, new-vertical defect caught, loader path verified)",
  );
}

if (process.argv.includes("--self-test")) {
  selfTest();
} else {
  run();
}
