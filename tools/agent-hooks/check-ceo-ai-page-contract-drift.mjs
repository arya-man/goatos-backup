#!/usr/bin/env node
// ceo-ai-page-contract-drift-guard (plan v3 D6 "Page-contract drift guard").
//
// Every KPI tile / chart the admin-web page contract declares is a number a CEO
// can see on a screen — and therefore a number the leadership assistant must be
// able to answer, or a documented, TYPED exclusion. This guard reads the SOURCE
// of the page contracts (backend/internal/adminui/app/service.go), finds every
// page whose copy map declares a `kpi.*` or `chart.*` key, resolves the page's
// data source(s) — each table's `data_source` route, or the page's own href when
// the page carries no table — and requires each of those surfaces to appear in
// docs/ceo-ai/coverage-matrix.md on a line that classifies it:
//
//   covered   Cube | api | view | tool | sql | ceo_ai.<view>
//   planned   PLANNED:P2[kpi.a,chart.b] | PLANNED:P3[...]   (plan v3 phase that
//             lands the read path, PLUS the tiles it defers — every kpi.<tile> /
//             chart.<tile> the page declares must be listed, so a NEW tile on a
//             page already in PLANNED debt still fails CI until it is either
//             answerable or explicitly deferred; reported as a tally so the
//             debt stays visible, never silent)
//   excluded  EXCLUDED:config | write | pii | detail | infra   (typed only)
//
// A surface that is absent, or present only as a bare `EXCLUDED`, fails: a read
// the page shows cannot be excluded without saying why (D6 "typed exclusions").
//
// What it parses (source, not runtime): `pageSpecificCopy` switch cases
// (`case "id", "id2":`), including copy helpers a case calls (`weighingWeightsCopy()`)
// and one level of `pageSpecificCopy("other-id")` indirection; `pages()` for
// `page("id", "href", ...)` and `table("id", "title", "data_source", ...)`.
//
// Blind spots, stated: a KPI whose copy key does not start with `kpi.`/`chart.`;
// a page that renders a figure with no copy key at all (the pen-vocabulary and
// contract tests cover copy, not data); and semantic drift — this proves a ROW
// exists for the page's data source, not that the bot's answer equals the tile.
// The nightly coverage eval (P2, docs/ceo-ai/coverage-matrix.md "Automatic
// coverage") is the runtime half.
import { readFileSync } from "node:fs";
import process from "node:process";

export const SERVICE_GO = "backend/internal/adminui/app/service.go";
export const COVERAGE_MATRIX = "docs/ceo-ai/coverage-matrix.md";

const COVERAGE_TOKEN_RE = /\b(Cube|api|view|tool|toolbox|sql|ceo_ai\.[a-z0-9_]+)\b/i;
const PLANNED_RE = /\bPLANNED:P[0-9][a-z]?\b/;
// A PLANNED row must enumerate the tiles it defers: PLANNED:P2[kpi.a,chart.b].
const PLANNED_TILES_RE = /\bPLANNED:P[0-9][a-z]?\[([^\]]*)\]/g;

// tileOf reduces a copy key to its tile: kpi.farm_value.detail -> kpi.farm_value,
// chart.monthly_revenue.title -> chart.monthly_revenue. A tile is the unit a
// CEO sees (one number / one chart); its label/sub/hint/aria copy keys are
// the same tile.
export function tileOf(key) {
  return key.split(".").slice(0, 2).join(".");
}

// plannedTiles returns the union of tiles every PLANNED row for the surface
// enumerates, or null when a PLANNED row for the surface carries no tile list.
export function plannedTiles(matrix, surface) {
  const lines = matrix.split("\n").filter((l) => l.includes(surface) && PLANNED_RE.test(l));
  const tiles = new Set();
  for (const line of lines) {
    const lists = [...line.matchAll(PLANNED_TILES_RE)];
    if (lists.length === 0) return null;
    for (const m of lists) {
      for (const t of m[1].split(",")) {
        const tile = t.trim();
        if (tile) tiles.add(tile);
      }
    }
  }
  return tiles;
}
const TYPED_EXCLUSION_RE = /\bEXCLUDED:(config|write|pii|detail|infra)\b/;
const BARE_EXCLUDED_RE = /\|\s*EXCLUDED\s*(\||$)/;

// funcBody returns the text between the braces of `func name(...)` in src.
function funcBody(src, name) {
  const m = src.match(new RegExp("\\nfunc " + name + "\\([^)]*\\)[^{]*\\{"));
  if (!m) return null;
  let i = m.index + m[0].length;
  let depth = 1;
  for (; i < src.length && depth > 0; i++) {
    const ch = src[i];
    if (ch === "{") depth++;
    else if (ch === "}") depth--;
  }
  return src.slice(m.index + m[0].length, i - 1);
}

const KEY_RE = /"((?:kpi|chart)\.[a-z0-9_.]+)"\s*:/g;

// parsePageCopyKeys returns { pageId: [kpi/chart keys] } from pageSpecificCopy.
export function parsePageCopyKeys(src) {
  const body = funcBody(src, "pageSpecificCopy");
  if (!body) throw new Error(`${SERVICE_GO}: pageSpecificCopy not found — parser broke`);
  const cases = [...body.matchAll(/\n\tcase ((?:"[a-z0-9-]+"(?:,\s*)?)+):/g)];
  const segments = new Map(); // id -> segment text (first case wins per id)
  for (let i = 0; i < cases.length; i++) {
    const ids = [...cases[i][1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);
    const seg = body.slice(cases[i].index, cases[i + 1] ? cases[i + 1].index : undefined);
    for (const id of ids) if (!segments.has(id)) segments.set(id, seg);
  }
  const helperCache = new Map();
  const keysOf = (seg, depth) => {
    const keys = [...seg.matchAll(KEY_RE)].map((m) => m[1]);
    // copy helpers: weighingWeightsCopy(), salesBoardCopy(), ...
    for (const h of new Set([...seg.matchAll(/\b([a-zA-Z0-9]+Copy)\(\)/g)].map((m) => m[1]))) {
      if (!helperCache.has(h)) {
        const hb = funcBody(src, h);
        helperCache.set(h, hb ? [...hb.matchAll(KEY_RE)].map((m) => m[1]) : []);
      }
      keys.push(...helperCache.get(h));
    }
    // one level of pageSpecificCopy("other") indirection (sales-sold -> sales)
    if (depth < 1) {
      for (const m of seg.matchAll(/pageSpecificCopy\("([a-z0-9-]+)"\)/g)) {
        const other = segments.get(m[1]);
        if (other) keys.push(...keysOf(other, depth + 1));
      }
    }
    return keys;
  };
  const out = {};
  for (const [id, seg] of segments) out[id] = [...new Set(keysOf(seg, 0))];
  return out;
}

// parsePages returns { pageId: { href, tables: [data_source...] } } from pages().
export function parsePages(src) {
  const body = funcBody(src, "pages");
  if (!body) throw new Error(`${SERVICE_GO}: pages() not found — parser broke`);
  const heads = [...body.matchAll(/page\("([a-z0-9-]+)",\s*"([^"]*)",\s*"([^"]*)"/g)];
  const out = {};
  for (let i = 0; i < heads.length; i++) {
    const seg = body.slice(heads[i].index, heads[i + 1] ? heads[i + 1].index : undefined);
    const tables = [...seg.matchAll(/table\("[^"]+",\s*"[^"]*",\s*"([^"]+)"/g)].map((m) => m[1]);
    out[heads[i][1]] = { href: heads[i][2].split("?")[0], tables: [...new Set(tables)] };
  }
  return out;
}

// classifySurface looks the surface up in the matrix and returns
// covered | planned | excluded | bare-excluded | missing.
export function classifySurface(matrix, surface) {
  const lines = matrix.split("\n").filter((l) => l.includes(surface));
  if (lines.length === 0) return "missing";
  let sawBare = false;
  for (const line of lines) {
    if (TYPED_EXCLUSION_RE.test(line)) return "excluded";
    if (PLANNED_RE.test(line)) return "planned";
    if (BARE_EXCLUDED_RE.test(line)) {
      sawBare = true;
      continue;
    }
    if (line.trim().startsWith("|") && COVERAGE_TOKEN_RE.test(line)) return "covered";
  }
  return sawBare ? "bare-excluded" : "missing";
}

// evaluate returns { errors: [...], planned: [...], checked: n }.
export function evaluate(serviceSrc, matrix) {
  const copy = parsePageCopyKeys(serviceSrc);
  const pages = parsePages(serviceSrc);
  const errors = [];
  const planned = [];
  let checked = 0;
  for (const [id, page] of Object.entries(pages)) {
    const keys = copy[id] || [];
    if (keys.length === 0) continue;
    const surfaces = page.tables.length > 0 ? page.tables : [page.href];
    for (const surface of surfaces) {
      checked++;
      const status = classifySurface(matrix, surface);
      if (status === "planned") {
        planned.push(`${id} ${surface}`);
        // Per-tile, not per-page: every tile the page declares must be in the
        // PLANNED row's list, or a new tile on a page already in PLANNED debt
        // would be invisible to this guard.
        const listed = plannedTiles(matrix, surface);
        const tiles = [...new Set(keys.map(tileOf))];
        if (listed === null) {
          errors.push(
            `page "${id}" data source ${surface} is PLANNED in ${COVERAGE_MATRIX} but the row does not enumerate the tiles it defers — write it as PLANNED:P<n>[${tiles.join(",")}] so a new tile on this page still fails CI`,
          );
        } else {
          const missing = tiles.filter((t) => !listed.has(t));
          if (missing.length > 0) {
            errors.push(
              `page "${id}" declares tile(s) ${missing.join(", ")} that its PLANNED row for ${surface} in ${COVERAGE_MATRIX} does not list — either the assistant answers the tile (cover it) or add it to the PLANNED:P<n>[...] list with the view that lands it`,
            );
          }
        }
      } else if (status === "missing") {
        errors.push(
          `page "${id}" declares ${keys.length} kpi/chart key(s) (e.g. ${keys[0]}) but its data source ${surface} is not in ${COVERAGE_MATRIX} — add a row under "Automatic coverage" classifying it (Cube/api/view/tool/sql, PLANNED:P<n>, or EXCLUDED:<config|write|pii|detail|infra>)`,
        );
      } else if (status === "bare-excluded") {
        errors.push(
          `page "${id}" data source ${surface} is only a bare EXCLUDED row in ${COVERAGE_MATRIX} — a read the page shows cannot be excluded without a type (EXCLUDED:config|write|pii|detail|infra), and a KPI tile is a read`,
        );
      }
    }
  }
  return { errors, planned, checked };
}

function selfTest() {
  const svc = (copyCases, pageDecls, helpers = "") => `package app
${helpers}
func pages() []domain.PageContract {
	return []domain.PageContract{
${pageDecls}
	}
}

func pageSpecificCopy(id string) map[string]string {
	switch id {
${copyCases}
	}
	return map[string]string{}
}
`;
  const kpiPage = `\tcase "weighing-weights":\n\t\treturn map[string]string{"kpi.total.label": "Total weight", "chart.average.title": "Average weight by pen"}\n`;
  const plainPage = `\tcase "audit-log":\n\t\treturn map[string]string{"crumb": "Audit"}\n`;
  const pageNoTable = `\t\tpage("weighing-weights", "/weighing/weights?x=1", "/weighing/weights", "Weights", "", "module-surface", nil),`;
  const pageWithTable = `\t\tpage("health-analytics", "/health/analytics", "/health/analytics", "Health", "", "module-surface",\n\t\t\t[]domain.TableContract{table("health-deaths", "Deaths", "/health/analytics", []string{"a"}, "id")}),`;
  const auditPage = `\t\tpage("audit-log", "/operations/audit", "/operations/audit", "Audit", "", "module-surface", nil),`;

  // 1. KPI page whose surface is missing from the matrix -> FAIL
  let r = evaluate(svc(kpiPage, pageNoTable), "| something else | api | x |");
  if (r.errors.length !== 1 || !/not in/.test(r.errors[0])) throw new Error("self-test 1: missing surface was not blocked: " + JSON.stringify(r));

  // 2. Same page, matrix row covers it -> PASS
  r = evaluate(svc(kpiPage, pageNoTable), "| /weighing/weights | api (GET /weighing/leadership/growth) | ok |");
  if (r.errors.length !== 0) throw new Error("self-test 2: covered surface was blocked: " + r.errors.join("; "));

  // 3. Bare EXCLUDED row -> FAIL (a KPI is a read)
  r = evaluate(svc(kpiPage, pageNoTable), "| /weighing/weights | EXCLUDED | because |");
  if (r.errors.length !== 1 || !/bare EXCLUDED/.test(r.errors[0])) throw new Error("self-test 3: bare EXCLUDED was not blocked: " + JSON.stringify(r));

  // 4. Typed exclusion -> PASS
  r = evaluate(svc(kpiPage, pageNoTable), "| /weighing/weights | EXCLUDED:detail | operator detail |");
  if (r.errors.length !== 0) throw new Error("self-test 4: typed exclusion was blocked: " + r.errors.join("; "));

  // 5. PLANNED row enumerating every tile -> PASS but tallied
  r = evaluate(svc(kpiPage, pageNoTable), "| /weighing/weights | PLANNED:P2[kpi.total,chart.average] (ceo_ai.growth_adg_pairs) | plan v3 |");
  if (r.errors.length !== 0 || r.planned.length !== 1) throw new Error("self-test 5: planned row mis-handled: " + JSON.stringify(r));

  // 5b. PLANNED row without a tile list -> FAIL (page-level PLANNED is a loophole)
  r = evaluate(svc(kpiPage, pageNoTable), "| /weighing/weights | PLANNED:P2 (ceo_ai.growth_adg_pairs) | plan v3 |");
  if (r.errors.length !== 1 || !/does not enumerate/.test(r.errors[0])) throw new Error("self-test 5b: bare PLANNED row was accepted: " + JSON.stringify(r));

  // 5c. A NEW tile on a page already in PLANNED debt -> FAIL naming the tile
  r = evaluate(svc(kpiPage, pageNoTable), "| /weighing/weights | PLANNED:P2[kpi.total] (ceo_ai.growth_adg_pairs) | plan v3 |");
  if (r.errors.length !== 1 || !/chart\.average/.test(r.errors[0])) throw new Error("self-test 5c: unlisted tile on a PLANNED page was not blocked: " + JSON.stringify(r));

  // 5d. Tiles may be listed across several PLANNED rows for the same surface
  r = evaluate(svc(kpiPage, pageNoTable), "| /weighing/weights | PLANNED:P2[kpi.total] | a |\n| /weighing/weights | PLANNED:P3[chart.average] | b |");
  if (r.errors.length !== 0) throw new Error("self-test 5d: tiles split across PLANNED rows were rejected: " + JSON.stringify(r));

  // 6. A page with no kpi/chart keys needs no row -> PASS
  r = evaluate(svc(plainPage, auditPage), "");
  if (r.errors.length !== 0 || r.checked !== 0) throw new Error("self-test 6: page without KPI keys was checked: " + JSON.stringify(r));

  // 7. Table data_source is the surface, and a page's tables are what must be covered
  r = evaluate(svc(`\tcase "health-analytics":\n\t\treturn map[string]string{"kpi.open_cases": "Open cases"}\n`, pageWithTable), "| /health/analytics | view:ceo_ai.health_open_cases | ok |");
  if (r.errors.length !== 0 || r.checked !== 1) throw new Error("self-test 7: table data_source coverage failed: " + JSON.stringify(r));

  // 8. Copy helper resolution: keys declared in weighingWeightsCopy() count
  const helperCase = `\tcase "weighing-weights":\n\t\treturn weighingWeightsCopy()\n`;
  const helperFn = `func weighingWeightsCopy() map[string]string {\n\treturn map[string]string{"kpi.kids.label": "Kids weighed"}\n}\n`;
  r = evaluate(svc(helperCase, pageNoTable, helperFn), "");
  if (r.errors.length !== 1) throw new Error("self-test 8: helper-declared KPI keys were not detected: " + JSON.stringify(r));

  // 9. pageSpecificCopy("sales") indirection: sales-sold inherits the sales KPI keys
  const indirect = `\tcase "sales-sold":\n\t\tout := map[string]string{}\n\t\tfor k, v := range pageSpecificCopy("sales") {\n\t\t\tout[k] = v\n\t\t}\n\t\treturn out\n\tcase "sales":\n\t\treturn map[string]string{"kpi.revenue.label": "Revenue"}\n`;
  const soldPage = `\t\tpage("sales-sold", "/sales/sold", "/sales/sold", "Sold", "", "module-surface", nil),`;
  r = evaluate(svc(indirect, soldPage), "");
  if (r.errors.length !== 1 || !/sales-sold/.test(r.errors[0])) throw new Error("self-test 9: pageSpecificCopy indirection not followed: " + JSON.stringify(r));

  // 10. A mis-typed exclusion category is NOT a typed exclusion
  r = evaluate(svc(kpiPage, pageNoTable), "| /weighing/weights | EXCLUDED:whatever | nope |");
  if (r.errors.length !== 1) throw new Error("self-test 10: unknown exclusion category was accepted: " + JSON.stringify(r));

  // 11. The real contract source parses to a non-trivial page set (parser drift canary)
  try {
    const real = readFileSync(SERVICE_GO, "utf8");
    const pages = parsePages(real);
    const copy = parsePageCopyKeys(real);
    const withKeys = Object.keys(pages).filter((id) => (copy[id] || []).length > 0);
    if (Object.keys(pages).length < 20 || withKeys.length < 10) {
      throw new Error(`self-test 11: real contract parsed to ${Object.keys(pages).length} pages / ${withKeys.length} with KPI keys — parser drift`);
    }
  } catch (e) {
    if (e.code === "ENOENT") {
      // Running outside the repo root: skip the canary, the synthetic cases above still ran.
    } else {
      throw e;
    }
  }

  console.log("ceo-ai-page-contract-drift guard self-test passed (14 cases)");
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }
  const serviceSrc = readFileSync(SERVICE_GO, "utf8");
  const matrix = readFileSync(COVERAGE_MATRIX, "utf8");
  const { errors, planned, checked } = evaluate(serviceSrc, matrix);
  if (planned.length > 0) {
    console.log(`ceo-ai-page-contract-drift: ${planned.length} page surface(s) classified PLANNED (plan v3 P2/P3 debt, not coverage):`);
    for (const p of planned) console.log(`  - ${p}`);
  }
  if (errors.length > 0) {
    console.error("ceo-ai-page-contract-drift guard failed:");
    for (const e of errors) console.error(`- ${e}`);
    console.error("");
    console.error(`Fix: add the page's data-source route to ${COVERAGE_MATRIX} under "Automatic coverage" as covered (Cube/api/view/tool/sql), PLANNED:P<n>[kpi.<tile>,chart.<tile>,...] listing every deferred tile with the view that lands it, or a TYPED exclusion (EXCLUDED:config|write|pii|detail|infra). See .agents/skills/goatos-leadership-assistant/SKILL.md.`);
    process.exit(1);
  }
  console.log(`ceo-ai-page-contract-drift guard passed (${checked} page surface(s) checked)`);
}

main();
