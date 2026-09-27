#!/usr/bin/env node
// Rewrite the tags/sx anatomy of every docs/design/template-derived.json entry from the licensed
// template on this laptop (the source of truth the derived file must keep).
//   node scripts/refresh-template-derived.mjs [--template <next-ts root>]
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { anatomy } from "./lib/template-derived.mjs";

const appDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const manifestFile = resolve(appDir, "../../docs/design/template-derived.json");
const args = process.argv.slice(2);
const ti = args.indexOf("--template");
const templateRoot = ti >= 0 ? resolve(args[ti + 1]) : join(homedir(), "mesha/mesha-ui/vendor/minimal/Minimal_TypeScript_v7.7.0/next-ts");
const manifest = JSON.parse(readFileSync(manifestFile, "utf8"));
for (const [rel, entry] of Object.entries(manifest.files ?? {})) {
  const src = join(templateRoot, entry.source);
  if (!existsSync(src)) { console.error(`missing template source ${entry.source} for ${rel}`); process.exitCode = 1; continue; }
  const a = anatomy(readFileSync(src, "utf8"));
  entry.tags = a.tags;
  entry.sx = a.sx;
}
writeFileSync(manifestFile, JSON.stringify(manifest, null, 2) + "\n");
console.log(`refreshed ${Object.keys(manifest.files ?? {}).length} derived entries`);
