#!/usr/bin/env node
// scaffold-coverage.mjs — emit ready-to-edit leadership-assistant coverage stubs
// for a module/feature so a dev never hand-types the boilerplate.
//
// Usage:
//   node tools/ceo-ai/scaffold-coverage.mjs <module-name>
//   node tools/ceo-ai/scaffold-coverage.mjs <module-name> --kpi
//   node tools/ceo-ai/scaffold-coverage.mjs <module-name> --exclude "reason"
//
// It PRINTS stubs (ceo_ai view, MCP Toolbox tool, GenAI query-class, eval golden
// Q, coverage-matrix row) to stdout. Paste each into its real file and fill in
// the derivations. Deterministic, offline, no writes.
//
// Canonical HOW-TO: .agents/skills/goatos-leadership-assistant/SKILL.md and
// references/coverage-howto.md. Guard: make leadership-assistant-coverage-guard.

import process from "node:process";

function parseArgs(argv) {
  const args = { module: "", kpi: false, exclude: null };
  const rest = [];
  for (let i = 0; i < argv.length; i += 1) {
    const a = argv[i];
    if (a === "--kpi") args.kpi = true;
    else if (a === "--exclude") args.exclude = argv[++i] ?? "";
    else if (a === "--self-test") args.selfTest = true;
    else rest.push(a);
  }
  args.module = rest[0] || "";
  return args;
}

function normalizeModule(raw) {
  const snake = String(raw)
    .trim()
    .replace(/[^a-zA-Z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .toLowerCase();
  return snake;
}

export function exclusionRow(module, reason) {
  const safe = (reason || "").replace(/\|/g, "/").trim() || "TODO: state why not leadership-relevant";
  return `| ${module} | EXCLUDED | ${safe} |`;
}

export function coverageRow(module, kpi) {
  const path = kpi
    ? `Cube:${module} (+ ceo_ai.${module}_base view)`
    : `ceo_ai.${module}_current view (+ mcp:${module} tool / read_api)`;
  return `| ${module} | ${path} | draft — fill sources |`;
}

export function scaffold({ module, kpi, exclude }) {
  const m = normalizeModule(module);
  if (!m) throw new Error("module name required");
  const out = [];
  out.push(`# Leadership-assistant coverage scaffold for: ${m}`);
  out.push(`# Canonical HOW-TO: .agents/skills/goatos-leadership-assistant/references/coverage-howto.md`);
  out.push("");

  if (exclude !== null) {
    out.push("## 1. Exclusion — docs/ceo-ai/coverage-matrix.md");
    out.push("");
    out.push(exclusionRow(m, exclude));
    out.push("");
    out.push("# Done. An exclusion is a valid coverage outcome; no view/tool/metric needed.");
    return out.join("\n");
  }

  if (kpi) {
    out.push("## 1a. Cube base view — backend/migrations/postgres/<NNNNNN>_add_" + m + "_reporting.sql");
    out.push("<!-- goose single-file convention: name is NNNNNN_<desc>.sql (NOT .up.sql), with -- +goose markers. -->");
    out.push("```sql");
    out.push("-- +goose Up");
    out.push("-- +goose StatementBegin");
    out.push(`CREATE OR REPLACE VIEW ceo_ai.${m}_base AS`);
    out.push(`-- _grain: one row per <grain>; IST business day; tenant-scoped; NO god-CTE`);
    out.push("SELECT");
    out.push("  t.tenant_id,");
    out.push("  -- numerator / denominator columns the Cube metric aggregates");
    out.push(`  count(*) AS ${m}_count`);
    out.push(`FROM <source_table> t`);
    out.push("GROUP BY 1;");
    out.push(`GRANT SELECT ON ceo_ai.${m}_base TO mesha_cube_readonly;`);
    out.push("-- +goose StatementEnd");
    out.push("-- +goose Down");
    out.push("-- +goose StatementBegin");
    out.push(`DROP VIEW IF EXISTS ceo_ai.${m}_base;`);
    out.push("-- +goose StatementEnd");
    out.push("```");
    out.push("");
    out.push("## 1b. Cube model — Cube schema files (outside the browser)");
    out.push("```javascript");
    out.push(`cube('${m}', {`);
    out.push(`  sql: 'SELECT * FROM ceo_ai.${m}_base',`);
    out.push("  measures: { value: { type: 'count' /* or sum/avg/ratio */ } },");
    out.push("  dimensions: { tenant_id: { sql: 'tenant_id', type: 'string' } },");
    out.push("});");
    out.push("```");
    out.push("");
  }

  out.push("## 2. ceo_ai view — backend/migrations/postgres/<NNNNNN>_add_" + m + "_reporting.sql");
  out.push("<!-- goose single-file convention: name is NNNNNN_<desc>.sql (NOT .up.sql), with -- +goose markers. -->");
  out.push("```sql");
  out.push("-- +goose Up");
  out.push("-- +goose StatementBegin");
  out.push(`CREATE OR REPLACE VIEW ceo_ai.${m}_current AS`);
  out.push(`-- _grain: one row per <grain>`);
  out.push("SELECT");
  out.push("  t.tenant_id,");
  out.push("  l.name       AS park_label,");
  out.push("  count(*)     AS animal_count,");
  out.push(`  NULL::text   -- TODO(no source yet): map or type-placeholder every leadership column`);
  out.push(`FROM <source_table> t`);
  out.push("LEFT JOIN locations l ON l.location_id = t.park_id");
  out.push("GROUP BY 1, 2;");
  out.push(`GRANT SELECT ON ceo_ai.${m}_current TO mesha_ceo_readonly;`);
  out.push("-- +goose StatementEnd");
  out.push("-- +goose Down");
  out.push("-- +goose StatementBegin");
  out.push(`DROP VIEW IF EXISTS ceo_ai.${m}_current;`);
  out.push("-- +goose StatementEnd");
  out.push("```");
  out.push("");

  out.push("## 3. MCP Toolbox tool — docs/ceo-ai/mcp-toolbox-tools.yaml");
  out.push("```yaml");
  out.push(`  ceo_ai_${m}:`);
  out.push("    kind: postgres-sql");
  out.push("    source: ceo-ai-readonly");
  out.push(`    description: "${m} leadership summary (aggregate, tenant-scoped)"`);
  out.push("    parameters:");
  out.push("      - name: tenant_id");
  out.push("        type: string");
  out.push("        description: server-session tenant, never user text");
  out.push("    statement: |");
  out.push(`      SELECT * FROM ceo_ai.${m}_current WHERE tenant_id = $1::uuid LIMIT 100;`);
  out.push("```");
  out.push("  # remember to add ceo_ai_" + m + " to the toolset list");
  out.push("");

  out.push("## 4. GenAI query-class — assistant query-space / context/agents/ceo-bot-analytics-context.md");
  out.push("```json");
  out.push("{");
  out.push(`  "intent": "${m}_summary",`);
  out.push(`  "description": "Leadership summary for ${m}.",`);
  out.push(`  "examples": ["What is the ${m} status?", "${m} by park today"],`);
  const tools = kpi
    ? `["Cube:${m}", "read_api:/${m}", "mcp:ceo_ai_${m}", "sql_fallback:ceo_ai.${m}_current"]`
    : `["read_api:/${m}", "mcp:ceo_ai_${m}", "sql_fallback:ceo_ai.${m}_current"]`;
  out.push(`  "target_tools": ${tools},`);
  out.push(`  "params": {"park_label": "optional", "period": "IST"},`);
  out.push(`  "grounding": "aggregate-first; blocked != 0; human labels; per-obligation vs per-goat where relevant"`);
  out.push("}");
  out.push("```");
  out.push("");

  out.push("## 5. Eval golden Q — tools/ceo-ai/eval/golden/<domain>.json");
  out.push("```json");
  out.push("{");
  out.push(`  "id": "${m}-summary",`);
  out.push(`  "class": "${m}_summary",`);
  out.push(`  "question": "What is the ${m} status right now?",`);
  const tiers = kpi ? `["cube", "api"]` : `["api", "sql"]`;
  out.push(`  "expect": { "grounded": true, "aggregate_first": true, "tiers_any_of": ${tiers} },`);
  out.push(`  "oracle": { "kind": "scalar_int", "sql": "SELECT count(*) FROM <source_table> WHERE tenant_id = :'tenant_id'::uuid" }`);
  out.push("}");
  out.push("```");
  out.push("");

  out.push("## 6. Coverage-matrix row — docs/ceo-ai/coverage-matrix.md");
  out.push("");
  out.push(coverageRow(m, kpi));
  return out.join("\n");
}

function selfTest() {
  const a = scaffold({ module: "feed adherence", kpi: false, exclude: null });
  if (!a.includes("ceo_ai.feed_adherence_current")) throw new Error("self-test: view stub missing normalized name");
  if (!a.includes("feed_adherence_summary")) throw new Error("self-test: query-class intent missing");
  const k = scaffold({ module: "mortality_rate", kpi: true, exclude: null });
  if (!k.includes("cube('mortality_rate'")) throw new Error("self-test: kpi cube stub missing");
  if (!k.includes("ceo_ai.mortality_rate_base")) throw new Error("self-test: kpi base view missing");
  const x = scaffold({ module: "task-options", kpi: false, exclude: "operator-only picker" });
  if (!x.includes("| task_options | EXCLUDED | operator-only picker |")) throw new Error("self-test: exclusion row wrong");
  if (x.includes("CREATE OR REPLACE VIEW")) throw new Error("self-test: exclusion should not emit a view");
  let threw = false;
  try { scaffold({ module: "", kpi: false, exclude: null }); } catch { threw = true; }
  if (!threw) throw new Error("self-test: empty module should throw");
  console.log("scaffold-coverage self-test passed");
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.selfTest) { selfTest(); return; }
  if (!args.module) {
    console.error("usage: node tools/ceo-ai/scaffold-coverage.mjs <module-name> [--kpi] [--exclude \"reason\"]");
    process.exit(2);
  }
  console.log(scaffold(args));
}

main();
