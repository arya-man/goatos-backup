#!/usr/bin/env node
// refresh-template-hashes.mjs — rewrite the `verbatim` block of docs/design/template-sources.json
// (sha256 of each normalised template source + its import specifiers) from the licensed MUI
// Minimal template on this laptop. The design:guard rule `template-verbatim` (lib/template-verbatim.mjs)
// checks the repo copies against these hashes in CI, where the template is not available.
//
//   node scripts/refresh-template-hashes.mjs [--template <next-ts root>] [--report]
//
// --report also lists which mapped files currently match / drift (does not touch the baseline).

import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { normaliseTemplateSource, templateHash } from "./lib/template-verbatim.mjs";

const appDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const manifestFile = resolve(appDir, "../../docs/design/template-sources.json");
const args = process.argv.slice(2);
const templateArg = args.indexOf("--template");
const templateRoot = templateArg >= 0
  ? resolve(args[templateArg + 1])
  : join(homedir(), "mesha/mesha-ui/vendor/minimal/Minimal_TypeScript_v7.7.0/next-ts");
if (!existsSync(join(templateRoot, "src"))) {
  console.error(`template not found at ${templateRoot}`);
  process.exit(1);
}

const manifest = JSON.parse(readFileSync(manifestFile, "utf8"));
const verbatim = {};
const missing = [];
const drift = [];
for (const [rel, src] of Object.entries(manifest.sources)) {
  const abs = join(templateRoot, src);
  if (!existsSync(abs)) {
    missing.push(`${rel} -> ${src}`);
    continue;
  }
  const text = readFileSync(abs, "utf8");
  verbatim[rel] = { sha256: templateHash(text), imports: normaliseTemplateSource(text).specifiers };
  const mine = join(appDir, rel);
  if (existsSync(mine) && templateHash(readFileSync(mine, "utf8")) !== verbatim[rel].sha256) drift.push(rel);
}
manifest.verbatim = Object.fromEntries(Object.entries(verbatim).sort(([a], [b]) => a.localeCompare(b)));
writeFileSync(manifestFile, `${JSON.stringify(manifest, null, 2)}\n`);
console.log(`template_hashes=${Object.keys(verbatim).length} missing_source=${missing.length} drift=${drift.length}`);
for (const m of missing) console.log(`  no template source: ${m}`);
if (args.includes("--report")) for (const d of drift) console.log(`  drift: ${d}`);
