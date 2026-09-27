// template-verbatim — every file mapped in docs/design/template-sources.json must equal its MUI
// Minimal template source byte-for-byte, except two mechanical edits:
//   1. a leading "use client" directive (added or removed);
//   2. module specifiers of import / export-from statements (the template's `src/...` alias and
//      relative paths become `@/components/minimal/...`, `@/layouts/...`, `@/theme/...` or a
//      relative path; package specifiers such as `@mui/material/Box` must stay identical).
// Everything else (props, sx, copy, behaviour, comments) is the template's. Ravi 2026-09-27: "use
// the SAME mesha-ui template across the pages and just put our content."
//
// The template is licensed and not committed, so template-sources.json stores, per file, the
// sha256 of the NORMALISED template source plus the template's specifier list; this module checks
// the repo file against those. `node scripts/refresh-template-hashes.mjs` regenerates them from
// ~/mesha/mesha-ui/vendor/minimal/Minimal_TypeScript_v7.7.0/next-ts.
//
// Ratchet: docs/design/template-verbatim-baseline.json lists files that still drift. It is
// shrink-only: a listed file that now matches must be removed, and no new file may drift.

import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

const SPECIFIER = /(\bfrom\s*|\bimport\s*\(\s*|\bimport\s+)(['"])([^'"\n]+)\2/g;
const DIRECTIVE = /^\s*(['"])use client\1;?[ \t]*\n/;
// Where a template-internal specifier (src/... or ./...) may point in this repo. lib/template-config/
// holds the template config points we own (number locale), never components.
const LOCAL_TARGET = /^(\.{1,2}\/|@\/lib\/template-config\/|@\/components\/minimal\/|@\/layouts\/|@\/theme\/|@\/theme$)/;

export function normaliseTemplateSource(text) {
  const specifiers = [];
  let body = String(text).replace(/\r\n/g, "\n").replace(DIRECTIVE, "");
  body = body.replace(SPECIFIER, (_m, head, _q, spec) => {
    specifiers.push(spec);
    return `${head}''`;
  });
  body = body.replace(/^\n+/, "");
  return { body, specifiers };
}

export function templateHash(text) {
  return createHash("sha256").update(normaliseTemplateSource(text).body).digest("hex");
}

const isLocalTemplateSpecifier = (spec) => spec.startsWith("src/") || spec.startsWith(".");

/** Returns `{ file, line, snippet }` hits. `root` = apps/admin-web. */
export function templateVerbatimFindings(root, manifestFile, baselineFile) {
  const hits = [];
  if (!existsSync(manifestFile)) return hits;
  let manifest;
  try {
    manifest = JSON.parse(readFileSync(manifestFile, "utf8"));
  } catch {
    return [{ file: "docs/design/template-sources.json", line: 1, snippet: "template-sources.json is not valid JSON" }];
  }
  const sources = manifest.sources ?? {};
  const verbatim = manifest.verbatim ?? {};
  let baseline = [];
  if (existsSync(baselineFile)) {
    try {
      baseline = JSON.parse(readFileSync(baselineFile, "utf8")).drift ?? [];
    } catch {
      hits.push({ file: "docs/design/template-verbatim-baseline.json", line: 1, snippet: "baseline is not valid JSON" });
    }
  }
  const allowed = new Set(baseline);
  for (const rel of Object.keys(sources)) {
    const abs = join(root, rel);
    if (!existsSync(abs)) continue; // unsourced-minimal-file reports stale entries
    const expect = verbatim[rel];
    if (!expect?.sha256) {
      if (!allowed.has(rel)) hits.push({ file: rel, line: 1, snippet: `no template source / verbatim hash in template-sources.json (not a template file? move it out of the template folders; else run node scripts/refresh-template-hashes.mjs)` });
      continue;
    }
    const { body, specifiers } = normaliseTemplateSource(readFileSync(abs, "utf8"));
    const hash = createHash("sha256").update(body).digest("hex");
    const problems = [];
    if (hash !== expect.sha256) problems.push(`differs from template ${sources[rel]} beyond import paths / "use client"`);
    const tpl = expect.imports ?? [];
    if (hash === expect.sha256 && specifiers.length === tpl.length) {
      specifiers.forEach((spec, i) => {
        const want = tpl[i];
        if (isLocalTemplateSpecifier(want)) {
          if (!LOCAL_TARGET.test(spec)) problems.push(`import '${spec}' replaces template '${want}' with a non-template module`);
        } else if (spec !== want) {
          problems.push(`import '${spec}' must stay '${want}' as in the template`);
        }
      });
    }
    if (problems.length && !allowed.has(rel)) hits.push({ file: rel, line: 1, snippet: problems[0] });
    if (!problems.length && allowed.has(rel)) {
      hits.push({ file: "docs/design/template-verbatim-baseline.json", line: 1, snippet: `${rel} is verbatim now: remove it from the drift baseline (shrink-only)` });
    }
  }
  for (const rel of baseline) {
    if (!(rel in sources)) hits.push({ file: "docs/design/template-verbatim-baseline.json", line: 1, snippet: `stale baseline entry ${rel} (not in template-sources.json)` });
  }
  return hits;
}
