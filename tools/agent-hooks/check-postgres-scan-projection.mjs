#!/usr/bin/env node

// Guard against the runtime failure class where a row scanner grows but a sibling SQL read keeps
// a private copied SELECT list. The bind-contract guard checks placeholders; this checks result
// projection reuse for packages that already declare a shared *Columns projection.

import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { execFileSync } from "node:child_process";

const root = process.cwd();
const MIN_DUPLICATE_COLUMNS = 4;
const MAX_FROM_LOOKAHEAD = 1200;

function git(args) {
  return execFileSync("git", args, { cwd: root, encoding: "utf8", maxBuffer: 128 * 1024 * 1024 });
}

function stripComments(source) {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1");
}

function splitTopLevel(list) {
  const items = [];
  let depth = 0;
  let quote = "";
  let start = 0;
  for (let i = 0; i < list.length; i += 1) {
    const ch = list[i];
    const prev = list[i - 1];
    if (quote) {
      if (ch === quote && prev !== "\\") quote = "";
      continue;
    }
    if (ch === "'" || ch === '"') {
      quote = ch;
      continue;
    }
    if (ch === "(") depth += 1;
    else if (ch === ")") depth = Math.max(0, depth - 1);
    else if (ch === "," && depth === 0) {
      items.push(list.slice(start, i).trim());
      start = i + 1;
    }
  }
  const tail = list.slice(start).trim();
  if (tail) items.push(tail);
  return items;
}

const STOP_WORDS = new Set([
  "as",
  "case",
  "cast",
  "coalesce",
  "distinct",
  "else",
  "end",
  "false",
  "null",
  "nullif",
  "select",
  "then",
  "true",
  "when",
]);

function projectionTokens(item) {
  const noStrings = item.replace(/'([^']|'')*'/g, " ");
  const tokens = [];
  for (const match of noStrings.matchAll(/[a-zA-Z_][a-zA-Z0-9_]*/g)) {
    const token = match[0].toLowerCase();
    if (STOP_WORDS.has(token)) continue;
    tokens.push(token);
  }
  // Prefer the column name side of alias.column expressions. Keeping a small set also lets
  // coalesce(l.unit, '') match l.unit in a copied private projection.
  const dotted = [...noStrings.matchAll(/\b[a-zA-Z_][a-zA-Z0-9_]*\.([a-zA-Z_][a-zA-Z0-9_]*)\b/g)].map((m) => m[1].toLowerCase());
  return new Set(dotted.length > 0 ? dotted : tokens);
}

function projectionSignature(columns) {
  return splitTopLevel(columns).flatMap((item) => [...projectionTokens(item)]).filter(Boolean);
}

function firstSelectList(sql) {
  const match = sql.match(/\bSELECT\b([\s\S]*?)\bFROM\b/i);
  return match?.[1]?.trim() ?? "";
}

function fromTables(sql) {
  return [...sql.matchAll(/\bFROM\s+(?:public\.)?([a-zA-Z_][a-zA-Z0-9_]*)\b/gi)].map((m) => m[1].toLowerCase());
}

function findBacktickSQL(source, file) {
  const snippets = [];
  for (const match of source.matchAll(/`([\s\S]*?)`/g)) {
    const sql = match[1];
    if (!/\bSELECT\b/i.test(sql) || !/\bFROM\b/i.test(sql)) continue;
    const prefix = source.slice(Math.max(0, match.index - 160), match.index);
    const isSQLConstant = /(?:\b(?:const|var)\s+(?:\(\s*)?)?[a-zA-Z_][a-zA-Z0-9_]*(?:SQL|Query)\s*=\s*$/m.test(prefix);
    const line = source.slice(0, match.index).split("\n").length;
    snippets.push({ file, line, sql, isSQLConstant });
  }
  return snippets;
}

function findColumnConstants(source, file) {
  const constants = [];
  for (const match of source.matchAll(/(?:\b(?:const|var)\s+(?:\(\s*)?)?([a-zA-Z_][a-zA-Z0-9_]*Columns[a-zA-Z0-9_]*)\s*=\s*`([\s\S]*?)`/g)) {
    constants.push({
      file,
      line: source.slice(0, match.index).split("\n").length,
      name: match[1],
      columns: match[2],
      signature: projectionSignature(match[2]),
    });
  }
  return constants;
}

function findColumnTables(packageSource, columnName) {
  const tables = new Set();
  const escaped = columnName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const pattern = new RegExp(`\\bSELECT\\b[\\s\\S]{0,200}?(?:${escaped}|%s)[\\s\\S]{0,${MAX_FROM_LOOKAHEAD}}?\\bFROM\\s+(?:public\\.)?([a-zA-Z_][a-zA-Z0-9_]*)\\b`, "g");
  for (const match of packageSource.matchAll(pattern)) tables.add(match[1].toLowerCase());
  return tables;
}

function copiedProjectionFinding(shared, snippet) {
  const selectList = firstSelectList(snippet.sql);
  if (!selectList) return null;
  const selectItems = splitTopLevel(selectList);
  if (selectItems.length < MIN_DUPLICATE_COLUMNS) return null;
  const querySig = projectionSignature(selectList);
  const querySet = new Set(querySig);
  const sharedPrefix = shared.signature.slice(0, Math.max(MIN_DUPLICATE_COLUMNS, Math.min(8, shared.signature.length)));
  const overlap = sharedPrefix.filter((token) => querySet.has(token)).length;
  if (overlap < Math.min(MIN_DUPLICATE_COLUMNS, sharedPrefix.length)) return null;
  return `${snippet.file}:${snippet.line}: private SELECT list appears to copy ${shared.name}; use the shared projection so scanner arity cannot drift`;
}

function copiedColumnConstantFinding(shared, candidate) {
  if (candidate.name === shared.name) return null;
  if (candidate.columns.includes(shared.name)) return null;
  const sharedStem = shared.name.slice(0, shared.name.indexOf("Columns"));
  if (!sharedStem || !candidate.name.startsWith(sharedStem) || candidate.name === `${sharedStem}Columns`) return null;
  const candidateSet = new Set(candidate.signature);
  const sharedPrefix = shared.signature.slice(0, Math.max(MIN_DUPLICATE_COLUMNS, Math.min(8, shared.signature.length)));
  const overlap = sharedPrefix.filter((token) => candidateSet.has(token)).length;
  if (overlap < Math.min(MIN_DUPLICATE_COLUMNS, sharedPrefix.length)) return null;
  return `${candidate.file}:${candidate.line}: ${candidate.name} appears to copy ${shared.name}; compose the shared projection before appending fields`;
}

function inspectSources(files, readFile = (file) => fs.readFileSync(path.join(root, file), "utf8")) {
  const packages = new Map();
  for (const file of files) {
    if (file.endsWith("_test.go")) continue;
    if (file.includes("/sqlc/")) continue;
    const source = stripComments(readFile(file));
    const dir = path.dirname(file);
    const pkg = packages.get(dir) ?? { source: "", constants: [], snippets: [] };
    pkg.source += `\n// ${file}\n${source}`;
    pkg.constants.push(...findColumnConstants(source, file));
    pkg.snippets.push(...findBacktickSQL(source, file));
    packages.set(dir, pkg);
  }

  const findings = [];
  for (const pkg of packages.values()) {
    for (const constant of pkg.constants) {
      for (const candidate of pkg.constants) {
        const finding = copiedColumnConstantFinding(constant, candidate);
        if (finding) findings.push(finding);
      }
    }
    for (const constant of pkg.constants) {
      const tables = findColumnTables(pkg.source, constant.name);
      if (tables.size === 0) continue;
      for (const snippet of pkg.snippets) {
        if (!snippet.isSQLConstant) continue;
        if (snippet.sql.includes(constant.name)) continue;
        const snippetTables = fromTables(snippet.sql);
        if (!snippetTables.some((table) => tables.has(table))) continue;
        const finding = copiedProjectionFinding(constant, snippet);
        if (finding) findings.push(finding);
      }
    }
  }
  return findings;
}

function selfTest() {
  const files = [
    "backend/internal/sales/adapters/postgres/deal_repository.go",
    "backend/internal/sales/adapters/postgres/overview_repository.go",
    "backend/internal/verification/adapters/postgres/repository.go",
  ];
  const sources = {
    [files[0]]: `
package postgres
const dealLineColumns = \`l.line_id::text, l.deal_id::text, l.line_no, l.product_type,
  coalesce(l.product_code, ''), coalesce(l.product_kind, ''), l.breed, l.quantity\`
const dealLinesForPageSQL = \`
  SELECT \` + dealLineColumns + \`
  FROM public.sales_deal_lines l
  WHERE l.tenant_id = $1\`
`,
    [files[1]]: `
package postgres
const dealLinesForClosedDealsSQL = \`
  SELECT l.line_id::text, l.deal_id::text, l.line_no, l.product_type, l.breed,
         l.animal_count, l.male_count, l.female_count
  FROM public.sales_deal_lines l
  WHERE l.tenant_id = $1\`
const countSQL = \`
  SELECT count(*), l.product_type
  FROM public.sales_deal_lines l
  GROUP BY l.product_type\`
`,
    [files[2]]: `
package postgres
const itemColumns = \`item_id::text, tenant_id::text, vertical, module, category,
  source_module, source_ref_type, source_ref_id::text\`
const itemColumnsWithLabels = \`vi.item_id::text, vi.tenant_id::text, vi.vertical, vi.module, vi.category,
  vi.source_module, vi.source_ref_type, vi.source_ref_id::text,
  operator.display_name::text, verifier.display_name::text\`
`,
  };
  const findings = inspectSources(files, (file) => sources[file]);
  if (findings.length !== 2 || !findings.some((f) => f.includes("dealLineColumns")) || !findings.some((f) => f.includes("itemColumns"))) {
    throw new Error(`self-test failed to isolate copied projection: ${findings.join("\n")}`);
  }
  sources[files[0]] = sources[files[0]].replace(
    "const dealLineColumns = `l.line_id::text, l.deal_id::text, l.line_no, l.product_type,\n  coalesce(l.product_code, ''), coalesce(l.product_kind, ''), l.breed, l.quantity`\nconst dealLinesForPageSQL = `",
    "const (\ndealLineColumns = `l.line_id::text, l.deal_id::text, l.line_no, l.product_type,\n  coalesce(l.product_code, ''), coalesce(l.product_kind, ''), l.breed, l.quantity`\ndealLinesForPageSQL = `",
  );
  sources[files[1]] = sources[files[1]].replace(
    "const dealLinesForClosedDealsSQL = `",
    "const (\ndealLinesForClosedDealsSQL = `",
  );
  const grouped = inspectSources(files, (file) => sources[file]);
  if (grouped.length !== 2 || !grouped.some((f) => f.includes("dealLineColumns")) || !grouped.some((f) => f.includes("itemColumns"))) {
    throw new Error(`self-test failed to isolate grouped const copied projection: ${grouped.join("\n")}`);
  }
  sources[files[1]] = sources[files[1]].replace(
    "SELECT l.line_id::text, l.deal_id::text, l.line_no, l.product_type, l.breed,\n         l.animal_count, l.male_count, l.female_count",
    "SELECT " + "` + dealLineColumns + `",
  );
  sources[files[2]] = sources[files[2]].replace(
    "const itemColumnsWithLabels = `vi.item_id::text, vi.tenant_id::text, vi.vertical, vi.module, vi.category,\n  vi.source_module, vi.source_ref_type, vi.source_ref_id::text,\n  operator.display_name::text, verifier.display_name::text`",
    "const itemColumnsWithLabels = itemColumns + `,\n  operator.display_name::text, verifier.display_name::text`",
  );
  const clean = inspectSources(files, (file) => sources[file]);
  if (clean.length !== 0) throw new Error(`self-test shared projection still failed: ${clean.join("\n")}`);
  console.log("postgres scan-projection guard self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const files = git(["ls-files", "backend/internal", "backend/cmd"])
  .split("\n")
  .filter((file) => file.endsWith(".go"));
const findings = inspectSources(files);
if (findings.length > 0) {
  console.error("postgres scan-projection guard FAILED:");
  for (const finding of findings) console.error(`  ${finding}`);
  process.exit(1);
}
console.log("postgres scan-projection guard passed");
