// tailwind-banned.mjs — guard `tailwind-banned` (FIXJ7, J1B P1-1).
//
// Tailwind + shadcn was a second global styling layer under the MUI theme: app/globals.css loaded
// `@import "tailwindcss"` (preflight on top of MUI CssBaseline, `* { @apply border-border }`), a
// hand scrollbar beside the template SimpleBar, tailwind.config.ts carried the shadcn palette
// (`hsl(var(--primary))`), components.json + tailwind-merge stayed installed and <body> wore
// `font-sans antialiased`. No className used a Tailwind utility. All of it is removed; the scrollbar
// lives in the MUI theme (theme/core/components/css-baseline.tsx, CssBaseline overrides).
//
// One finding per:
//   - stylesheet with `@import "tailwindcss"`, `@tailwind`, `@apply`, `@config`, `@source`, `@plugin`
//   - tailwind.config.* / components.json (shadcn) / a postcss config naming tailwind
//   - source importing tailwind-merge / tailwindcss / @tailwindcss/* / shadcn, or a Vite
//     tailwindcss() plugin (the Storybook config)
//   - Tailwind / shadcn package in the app's package.json
//   - a `font-sans` / `antialiased` utility class on <html> / <body>
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";

export const TAILWIND_PACKAGES = Object.freeze(["tailwindcss", "@tailwindcss/postcss", "@tailwindcss/vite", "tailwind-merge", "shadcn", "tailwindcss-animate", "tw-animate-css"]);
const CSS_AT = /@(?:import\s+["']tailwindcss(?:\/[^"']*)?["']|tailwind\b|apply\b|config\b|source\b|plugin\b)/g;
const SRC_IMPORT = /(?:from\s*|import\s*\(\s*|require\(\s*|^\s*import\s+)["'](tailwind-merge|tailwindcss(?:\/[^"']*)?|@tailwindcss\/[^"']+|shadcn(?:\/[^"']*)?)["']/gm;
const BODY_UTIL = /<(?:html|body)\b[^>]*\bclassName=["'`][^"'`]*\b(font-sans|antialiased)\b/g;
const SKIP = new Set(["node_modules", ".next", "storybook-static", ".git", ".codex-goatos-render", "public", "visual-baselines"]);

const lineOf = (text, index) => text.slice(0, index).split("\n").length;
const toRel = (root, abs) => relative(root, abs).split(sep).join("/");

function walk(dir, out = []) {
  let names = [];
  try { names = readdirSync(dir); } catch { return out; }
  for (const name of names) {
    if (SKIP.has(name)) continue;
    const full = join(dir, name);
    let st;
    try { st = statSync(full); } catch { continue; }
    if (st.isDirectory()) walk(full, out);
    else out.push(full);
  }
  return out;
}

export function tailwindBannedFindings(root) {
  const out = [];
  for (const abs of walk(root)) {
    const rel = toRel(root, abs);
    const name = rel.split("/").pop();
    if (/^tailwind\.config\.[cm]?[jt]s$/.test(name)) { out.push({ file: rel, line: 1, snippet: "Tailwind config: delete it; styles are the MUI theme + sx" }); continue; }
    if (rel === "components.json") { out.push({ file: rel, line: 1, snippet: "shadcn components.json: delete it; components are MUI / the Minimal template" }); continue; }
    // Guard sources and their fixtures name the banned strings on purpose.
    if (/\.test\.mjs$/.test(name) || rel.startsWith("scripts/")) continue;
    if (/^postcss\.config\.[cm]?[jt]s$/.test(name)) {
      const src = readFileSync(abs, "utf8");
      if (/tailwind/.test(src)) out.push({ file: rel, line: lineOf(src, src.search(/tailwind/)), snippet: "postcss runs Tailwind: remove the plugin (delete the config if nothing is left)" });
      continue;
    }
    if (name.endsWith(".css")) {
      if (rel.startsWith("components/minimal/") || rel.startsWith("layouts/template/")) continue;
      const src = readFileSync(abs, "utf8").replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "));
      for (const m of src.matchAll(CSS_AT)) out.push({ file: rel, line: lineOf(src, m.index), snippet: `${m[0].trim()}: Tailwind directive; style through the MUI theme (CssBaseline overrides) or sx` });
      continue;
    }
    if (/\.(tsx?|jsx?|mjs|cjs)$/.test(name)) {
      const src = readFileSync(abs, "utf8");
      for (const m of src.matchAll(SRC_IMPORT)) out.push({ file: rel, line: lineOf(src, m.index), snippet: `imports ${m[1]}: Tailwind / shadcn is not a dependency of admin-web` });
      if (/tailwindcss\s*\(/.test(src) && /@tailwindcss\/vite|tailwindcss/.test(src)) {
        const i = src.search(/tailwindcss\s*\(/);
        out.push({ file: rel, line: lineOf(src, i), snippet: "Vite tailwindcss() plugin: remove it (Storybook styles come from the MUI theme stack)" });
      }
      for (const m of src.matchAll(BODY_UTIL)) out.push({ file: rel, line: lineOf(src, m.index), snippet: `Tailwind utility "${m[1]}" on <html>/<body>: the MUI theme (CssBaseline + AppBaseline) sets font and smoothing` });
    }
  }
  const pkgFile = join(root, "package.json");
  if (existsSync(pkgFile)) {
    try {
      const pkg = JSON.parse(readFileSync(pkgFile, "utf8"));
      for (const field of ["dependencies", "devDependencies", "peerDependencies", "optionalDependencies"]) {
        for (const dep of TAILWIND_PACKAGES) if (pkg[field]?.[dep]) out.push({ file: "package.json", line: 1, snippet: `${field}.${dep}: remove it; admin-web styles with the MUI theme only` });
      }
    } catch {}
  }
  return out;
}
