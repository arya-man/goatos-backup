// legacy-class-denylist.mjs — the FROZEN list of legacy class names (J1B P2-2, FIXJ7).
//
// The legacy-class guards (legacy-free-zone, the legacy-class-use ratchet, the story scan) used to
// build their banned list from the stylesheets that exist NOW. FIXJ6 deleted frame.css,
// minimal-theme.css and mesha-theme.css, so that list shrank to `simplebar-*` and a reintroduced
// `className="btn"` / `"chip"` / `"tbl"` passed unseen. The list is now a static manifest,
// scripts/legacy-class-denylist.json: every class selector the legacy stylesheets defined at
// 7e181ce32 (the last commit that had them all). `hooks` are JS / sx / test hooks with no stylesheet
// behind them; they are exempt, everything else in `classes` is banned in className.
//
//   node scripts/lib/legacy-class-denylist.mjs --from 7e181ce32   regenerate `classes` from git history
//                                                                   (keeps `hooks`; the list never shrinks)

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const appDir = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
export const DENYLIST_FILE = "scripts/legacy-class-denylist.json";
const EXCLUDED_PREFIX = /^(?:Mui|apexcharts|fc-|simplebar|iconify)/;

/** { classes: Set, hooks: Map<token, reason> } from the committed manifest. */
export function readLegacyDenylist(root = appDir) {
  // A guard fixture root (design:guard --self-test) carries no manifest: it reads the app's own.
  const file = existsSync(join(root, DENYLIST_FILE)) ? join(root, DENYLIST_FILE) : join(appDir, DENYLIST_FILE);
  const data = JSON.parse(readFileSync(file, "utf8"));
  return { classes: new Set(data.classes ?? []), hooks: new Map(Object.entries(data.hooks ?? {})) };
}

/** The banned className tokens: frozen legacy classes minus the documented hooks. */
export function bannedLegacyClasses(root = appDir) {
  const { classes, hooks } = readLegacyDenylist(root);
  for (const hook of hooks.keys()) classes.delete(hook);
  return classes;
}

/** Class selectors in one stylesheet's text (selectors only, `:not(...)` arguments dropped). */
export function selectorClasses(cssText) {
  const out = new Set();
  const text = cssText.replace(/\/\*[\s\S]*?\*\//g, "").replace(/url\([^)]*\)/g, "");
  for (const m of text.matchAll(/([^{}]+)\{/g)) {
    if (m[1].trim().startsWith("@")) continue;
    const sel = m[1].replace(/:(?:not)\((?:[^()]|\([^()]*\))*\)/g, "");
    for (const c of sel.matchAll(/\.(-?[_a-zA-Z][\w-]*)/g)) if (!EXCLUDED_PREFIX.test(c[1])) out.add(c[1]);
  }
  return out;
}

function regenerate(sha) {
  const git = (...args) => execFileSync("git", args, { cwd: appDir, encoding: "utf8", maxBuffer: 1e8 });
  const top = git("rev-parse", "--show-toplevel").trim();
  const prefix = resolve(appDir).slice(top.length + 1);
  const files = git("ls-tree", "-r", "--full-tree", "--name-only", sha, "--", prefix).split("\n").filter((f) =>
    f.endsWith(".css") && !f.includes("/components/minimal/") && !f.includes("/layouts/template/") && !f.endsWith(".module.css")
    && !f.includes("/.storybook/") && !f.endsWith("/fonts.css") && !f.endsWith("/minimal-tokens.css"));
  const found = new Set();
  for (const f of files) for (const c of selectorClasses(git("show", `${sha}:${f}`))) found.add(c);
  const file = join(appDir, DENYLIST_FILE);
  const data = JSON.parse(readFileSync(file, "utf8"));
  const merged = new Set([...(data.classes ?? []), ...found]); // never shrinks
  data.source = sha;
  data.classes = [...merged].sort();
  writeFileSync(file, `${JSON.stringify(data, null, 1)}\n`);
  console.log(`legacy-class-denylist: ${data.classes.length} classes from ${files.length} stylesheets at ${sha}`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const i = process.argv.indexOf("--from");
  if (i < 0 || !process.argv[i + 1]) {
    console.error("usage: node scripts/lib/legacy-class-denylist.mjs --from <sha>");
    process.exit(2);
  }
  regenerate(process.argv[i + 1]);
}
