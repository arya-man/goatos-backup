#!/usr/bin/env node

// check-weighing-operator-scope-guard.mjs — a weighing operator must only ever see their own
// assigned campaign-shed buckets. See
// context/repo-audits/weighing-implementation-do-not-reopen-ledger.md (A-1, A-2, B-3, C-1,
// E-1) and backend/internal/weighing/adapters/postgres/repository.go:283-311 (the
// `EXISTS (... weighing_campaign_sheds ... operator_user_id=$N ...)` pattern before LIMIT).
//
// Fails on two failure modes in backend/internal/weighing/**:
//   1. paginate-before-filter — an operator-facing list/roster query applies LIMIT/cursor
//      without an operator_user_id predicate positioned before LIMIT in the same SQL string
//      (i.e. filtering is either missing or happens after the page is already cut).
//   2. missing-operator-predicate-on-execute-route — an HTTP handler for an execute-scoped
//      weighing route (list/roster reads used to drive scanning work) calls a repository
//      method without threading the authenticated actor's operator id through, or a
//      repository method meant to be operator-scoped (its own name says "ForOperator") has no
//      operator_user_id predicate in its SQL at all.
//
// Modes:
//   (default)     scan the real weighing backend tree.
//   --self-test   run adversarial good/bad fixtures for both failure modes and exit.
//
// Blind spots (native Grep/Read must still catch these): dynamically built SQL strings,
// operator scoping enforced only in a caller several layers removed from the query (this guard
// looks at the SQL string and its immediately enclosing Go function only), and any new
// operator-facing list/roster function not matched by OPERATOR_LIST_FN_RE below.

import { readFileSync, readdirSync, existsSync, mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

// WEIGHING_GUARD_TEST_REPO lets the exit-code self-test point this script at a throwaway
// fixture tree instead of the real repo (see selfTestExitCodes below).
const repo = process.env.WEIGHING_GUARD_TEST_REPO
  ? resolve(process.env.WEIGHING_GUARD_TEST_REPO)
  : resolve(import.meta.dirname, "../..");
const SELF_PATH = fileURLToPath(import.meta.url);
const WEIGHING_DIR = "backend/internal/weighing";

// Repository functions that read a page of weighing rows for an operator-scoped worklist.
const OPERATOR_LIST_FN_RE = /^(listCampaigns|listScopeRoster)$/;

function extractFunctionBody(source, fnName) {
  const re = new RegExp(`\\nfunc\\s+(?:\\([^)]*\\)\\s+)?${fnName}\\s*\\(`);
  const m = re.exec("\n" + source);
  if (!m) return null;
  const start = m.index + m[0].length;
  const rest = source.slice(start);
  const nextFn = rest.search(/\nfunc\s/);
  return rest.slice(0, nextFn < 0 ? rest.length : nextFn);
}

// Find every raw SQL string literal (backtick-delimited) in a function body.
function extractSQLStrings(body) {
  const out = [];
  const re = /`([^`]*)`/g;
  let m;
  while ((m = re.exec(body)) !== null) {
    if (/\bSELECT\b/i.test(m[1])) out.push(m[1]);
  }
  return out;
}

export function findingsForRepositorySource(rel, source) {
  const findings = [];
  const fnNameRe = /\nfunc\s+(?:\([^)]*\)\s+)?([A-Za-z0-9_]+)\s*\(/g;
  let m;
  while ((m = fnNameRe.exec("\n" + source)) !== null) {
    const fnName = m[1];
    if (!OPERATOR_LIST_FN_RE.test(fnName)) continue;
    const body = extractFunctionBody(source, fnName);
    if (body == null) continue;
    for (const sql of extractSQLStrings(body)) {
      if (!/LIMIT\s+\$?\d*\w*/i.test(sql)) continue; // not a paginated read
      const limitIdx = sql.search(/LIMIT\s+\$?\d*\w*/i);
      const operatorIdx = sql.search(/operator_user_id/i);
      if (operatorIdx < 0) {
        findings.push({
          rule: "paginate-before-filter",
          message: `${rel}: ${fnName}() SQL applies LIMIT with no operator_user_id predicate anywhere in the query — operator worklists must filter by operator_user_id in SQL before LIMIT`,
        });
      } else if (operatorIdx > limitIdx) {
        findings.push({
          rule: "paginate-before-filter",
          message: `${rel}: ${fnName}() SQL applies LIMIT before its operator_user_id predicate — filter must be in the WHERE/EXISTS clause ahead of LIMIT, never after`,
        });
      }
    }
  }
  return findings;
}

// Handler-layer check: an execute-scoped route (drives scanning work) must call the
// *ForOperator repository method or the service.List*/ListScopeRoster method with the
// authenticated actor, never with a client-supplied operator id from the request body/query.
export function findingsForHandlerSource(rel, source) {
  const findings = [];
  const routeFnNames = ["ListCampaigns", "ListScopeRoster"];

  // A route handler may delegate to a shared unexported helper in the same file -- e.g.
  // ListCampaigns and AppListCampaigns both forward to listCampaigns(w, r, fallbackScope), which
  // differ only in their default scope. The security property lives in the helper, so inspecting
  // ONLY the exported body reports a missing actor(r) on a handler that is perfectly correct, and
  // pressures the next author to inline the check back into both copies -- which is how these
  // predicates drift apart in the first place.
  //
  // Follow ONE level of same-file delegation: if the body is a lone call to another function
  // defined here, analyse that function instead. Deliberately one level and same-file only --
  // chasing further would need real call-graph analysis, and a silently-deep chain should be
  // visible to a reviewer anyway.
  const resolveBody = (fnName) => {
    const body = extractFunctionBody(source, fnName);
    if (body == null) return null;
    // Already carries the check: nothing to follow.
    if (/actor\(r\)/.test(body)) return body;
    // Otherwise, if it forwards to a same-file helper, the property lives there. Take the FIRST
    // such delegate; a handler that forwards to several is not a thin delegator and should be
    // reported as-is.
    const delegates = [...body.matchAll(/h\.([a-z][A-Za-z0-9_]*)\s*\(/g)].map((m) => m[1]);
    for (const name of delegates) {
      const inner = extractFunctionBody(source, name);
      if (inner != null) return inner;
    }
    return body;
  };

  for (const fnName of routeFnNames) {
    const body = resolveBody(fnName);
    if (body == null) continue;
    if (!/actor\(r\)/.test(body)) {
      findings.push({
        rule: "missing-operator-predicate-on-execute-route",
        message: `${rel}: handler ${fnName}() does not pass actor(r) to the service call — operator scope must come from the authenticated actor, never a client-supplied field`,
      });
    }
    if (/req\.OperatorUserID|r\.URL\.Query\(\)\.Get\("operator_user_id"\)/.test(body)) {
      findings.push({
        rule: "missing-operator-predicate-on-execute-route",
        message: `${rel}: handler ${fnName}() reads a client-supplied operator id — operator scope must be derived from the authenticated actor only`,
      });
    }
  }
  return findings;
}

function isWeighingGo(rel) {
  return rel.startsWith(`${WEIGHING_DIR}/`) && rel.endsWith(".go") && !rel.endsWith("_test.go");
}

function walk(dir, matcher, out) {
  if (!existsSync(dir)) return out;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) walk(path, matcher, out);
    else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (matcher(rel)) out.push(rel);
    }
  }
  return out;
}

function run() {
  const files = walk(resolve(repo, WEIGHING_DIR), isWeighingGo, []);
  const problems = [];
  for (const rel of files) {
    const source = readFileSync(resolve(repo, rel), "utf8");
    for (const f of findingsForRepositorySource(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
    if (rel.includes("/adapters/http/")) {
      for (const f of findingsForHandlerSource(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
    }
  }
  if (problems.length > 0) {
    console.error("weighing-operator-scope guard failed:");
    for (const p of problems) console.error(`- ${p}`);
    process.exit(1);
  }
  console.log(`weighing-operator-scope guard: ok (${files.length} weighing Go files)`);
}

function selfTest() {
  const badRepo1 = `
func (r *Repository) listCampaigns(ctx context.Context, tenantID, operatorUserID string, cursor string, limit int) (domain.CampaignPage, error) {
  rows, err := r.pool.Query(ctx, ` + "`" + `SELECT campaign_id FROM weighing_campaigns WHERE tenant_id=$1 ORDER BY created_at LIMIT $2` + "`" + `, tenantID, limit)
  return domain.CampaignPage{}, nil
}
`;
  const badRepo1b = `
func (r *Repository) listScopeRoster(ctx context.Context, tenantID, campaignID, campaignShedID, operatorUserID string, cursor string, limit int) (domain.RosterPage, error) {
  rows, err := r.pool.Query(ctx, ` + "`" + `SELECT animal_id FROM weighing_expected_animals WHERE tenant_id=$1 LIMIT $2` + "`" + `, tenantID, limit)
  filtered := []domain.Roster{}
  for _, row := range rows {
    if row.OperatorUserID == operatorUserID { filtered = append(filtered, row) }
  }
  return domain.RosterPage{}, nil
}
`;
  const goodRepo1 = `
func (r *Repository) listCampaigns(ctx context.Context, tenantID, operatorUserID string, cursor string, limit int) (domain.CampaignPage, error) {
  rows, err := r.pool.Query(ctx, ` + "`" + `SELECT campaign_id FROM weighing_campaigns WHERE tenant_id=$1 AND EXISTS (SELECT 1 FROM weighing_campaign_sheds scope WHERE scope.operator_user_id=$6::uuid) ORDER BY created_at LIMIT $2` + "`" + `, tenantID, limit)
  return domain.CampaignPage{}, nil
}
`;

  const findingsBad1 = findingsForRepositorySource("fake_repo.go", badRepo1);
  if (!findingsBad1.some((f) => f.rule === "paginate-before-filter")) {
    throw new Error(`self-test failed: mode 1 (missing predicate) not flagged. got: ${JSON.stringify(findingsBad1)}`);
  }
  const findingsBad1b = findingsForRepositorySource("fake_repo.go", badRepo1b);
  if (!findingsBad1b.some((f) => f.rule === "paginate-before-filter")) {
    throw new Error(`self-test failed: mode 1 (post-query filter) not flagged. got: ${JSON.stringify(findingsBad1b)}`);
  }
  if (findingsForRepositorySource("fake_repo.go", goodRepo1).length !== 0) {
    throw new Error("self-test failed: mode 1 false positive on EXISTS-before-LIMIT pattern");
  }

  const badHandler2 = `
func (h *Handler) ListCampaigns(w http.ResponseWriter, r *http.Request) {
  operatorID := r.URL.Query().Get("operator_user_id")
  page, err := h.service.ListCampaigns(r.Context(), operatorID, r.URL.Query().Get("cursor"), 20)
  h.respond(w, r, page, err)
}
`;
  const goodHandler2 = `
func (h *Handler) ListCampaigns(w http.ResponseWriter, r *http.Request) {
  page, err := h.service.ListCampaigns(r.Context(), actor(r), r.URL.Query().Get("cursor"), 20)
  h.respond(w, r, page, err)
}
`;
  const findingsBad2 = findingsForHandlerSource("fake_handler.go", badHandler2);
  if (!findingsBad2.some((f) => f.rule === "missing-operator-predicate-on-execute-route")) {
    throw new Error(`self-test failed: mode 2 not flagged. got: ${JSON.stringify(findingsBad2)}`);
  }
  if (findingsForHandlerSource("fake_handler.go", goodHandler2).length !== 0) {
    throw new Error("self-test failed: mode 2 false positive on actor(r)-derived scope");
  }

  // Mode 3 -- delegation. A route handler may be a thin forwarder to a shared same-file helper
  // (ListCampaigns/AppListCampaigns both forward to listCampaigns, differing only in default
  // scope). The guard follows one level so it checks where the property actually lives; these two
  // fixtures pin BOTH directions, because a resolver that follows delegation without still
  // catching a dropped actor would silently turn this guard off.
  const delegatingGood = `
func (h *Handler) ListCampaigns(w http.ResponseWriter, r *http.Request) {
	h.listCampaigns(w, r, domain.CampaignListScopeAll)
}

func (h *Handler) listCampaigns(w http.ResponseWriter, r *http.Request, fallback domain.CampaignListScope) {
	caller := actor(r)
	page, err := h.service.ListCampaigns(r.Context(), caller, fallback)
}
`;
  if (findingsForHandlerSource("fake_handler.go", delegatingGood).length !== 0) {
    throw new Error("self-test failed: mode 3 false positive on a handler delegating to a helper that uses actor(r)");
  }

  const delegatingBad = delegatingGood.replace(
    "caller := actor(r)",
    `caller := domain.Actor{UserID: r.URL.Query().Get("operator_user_id")}`,
  );
  if (findingsForHandlerSource("fake_handler.go", delegatingBad).length === 0) {
    throw new Error("self-test failed: mode 3 accepted a delegate that takes operator scope from the query string");
  }

  console.log("weighing-operator-scope guard: self-test passed (3/3 failure modes)");
}

// Spawns THIS script as a child process against a throwaway fixture repo and asserts the real
// process exit code (not just in-process finding text). One case per failure mode plus a clean
// baseline.
function runOneExitCodeCase(label, { repoGo, httpGo }, expectFailure) {
  const dir = mkdtempSync(join(tmpdir(), "weighing-operator-scope-guard-exitcode-"));
  try {
    const repoDir = join(dir, "backend/internal/weighing/adapters/postgres");
    const httpDir = join(dir, "backend/internal/weighing/adapters/http");
    mkdirSync(repoDir, { recursive: true });
    mkdirSync(httpDir, { recursive: true });
    writeFileSync(join(repoDir, "fixture_repo.go"), repoGo ?? "package postgres\n");
    writeFileSync(join(httpDir, "fixture_handler.go"), httpGo ?? "package http\n");
    const result = spawnSync(process.execPath, [SELF_PATH], {
      env: { ...process.env, WEIGHING_GUARD_TEST_REPO: dir },
      encoding: "utf8",
    });
    const failed = result.status !== 0;
    if (failed !== expectFailure) {
      throw new Error(
        `exit-code self-test failed [${label}]: expected exit ${expectFailure ? "non-zero" : "0"}, got ${result.status}\nstdout: ${result.stdout}\nstderr: ${result.stderr}`,
      );
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

function selfTestExitCodes() {
  const cleanRepoGo = `package postgres

func (r *Repository) listCampaigns(ctx context.Context, tenantID, operatorUserID string, cursor string, limit int) (domain.CampaignPage, error) {
  rows, err := r.pool.Query(ctx, ` + "`" + `SELECT campaign_id FROM weighing_campaigns WHERE tenant_id=$1 AND EXISTS (SELECT 1 FROM weighing_campaign_sheds scope WHERE scope.operator_user_id=$6::uuid) ORDER BY created_at LIMIT $2` + "`" + `, tenantID, limit)
  return domain.CampaignPage{}, nil
}
`;
  const cleanHttpGo = `package http

func (h *Handler) ListCampaigns(w http.ResponseWriter, r *http.Request) {
  page, err := h.service.ListCampaigns(r.Context(), actor(r), r.URL.Query().Get("cursor"), 20)
  h.respond(w, r, page, err)
}
`;

  runOneExitCodeCase("clean tree", { repoGo: cleanRepoGo, httpGo: cleanHttpGo }, false);

  runOneExitCodeCase(
    "mode 1: paginate-before-filter (missing operator predicate)",
    {
      repoGo: `package postgres

func (r *Repository) listCampaigns(ctx context.Context, tenantID, operatorUserID string, cursor string, limit int) (domain.CampaignPage, error) {
  rows, err := r.pool.Query(ctx, ` + "`" + `SELECT campaign_id FROM weighing_campaigns WHERE tenant_id=$1 ORDER BY created_at LIMIT $2` + "`" + `, tenantID, limit)
  return domain.CampaignPage{}, nil
}
`,
      httpGo: cleanHttpGo,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 2: missing-operator-predicate-on-execute-route (client-supplied operator id)",
    {
      repoGo: cleanRepoGo,
      httpGo: `package http

func (h *Handler) ListCampaigns(w http.ResponseWriter, r *http.Request) {
  operatorID := r.URL.Query().Get("operator_user_id")
  page, err := h.service.ListCampaigns(r.Context(), operatorID, r.URL.Query().Get("cursor"), 20)
  h.respond(w, r, page, err)
}
`,
    },
    true,
  );

  console.log("weighing-operator-scope guard: exit-code self-test passed (clean=0, 2/2 violations=non-zero)");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  selfTestExitCodes();
} else {
  run();
}
