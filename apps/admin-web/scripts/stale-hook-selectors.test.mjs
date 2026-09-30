// guard: stale-hook-selectors (FIXJ7, J1B P2-4).
//
// A DOM hook that queries a class nothing renders any more is dead code that looks alive:
// components/app/scroll-edges.tsx kept querying `.tablewrap, .twrap, .tblwrap, .feed-scroll,
// .kit-scroll-x, .subtabs` and the /tasks click-matrix smoke counted `.drawer.on` and
// `.lt-fgroup.open` long after the template move removed them, so the fades never showed and the
// smoke's state fingerprint silently ignored every open dialog. Every class a listed hook queries
// must still be rendered by the app source (a className string / template literal in app/,
// components/, features/ or layouts/). MUI (`Mui*`) state classes are rendered by MUI itself.
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

/** Class tokens in CSS selector strings (the quoted arguments of querySelector-like calls). */
export function selectorClasses(selectorText) {
  const out = new Set();
  for (const m of selectorText.matchAll(/(?<![\d])\.(-{0,2}[_a-zA-Z][\w-]*)/g)) {
    if (!/^Mui/.test(m[1])) out.add(m[1]);
  }
  return out;
}

/** Classes queried by one hook region of a source file. */
function hookRegion(file, start, end) {
  const src = readFileSync(join(root, file), "utf8");
  const a = src.indexOf(start);
  assert.ok(a >= 0, `${file}: hook region "${start}" not found (update the guard with the hook)`);
  const b = end ? src.indexOf(end, a) : src.length;
  const region = src.slice(a, b < 0 ? src.length : b);
  // Only the selector strings: querySelector(All)/locator/closest arguments and the SELECTOR const.
  const strings = [...region.matchAll(/(?:querySelector(?:All)?|locator|closest|SELECTOR\s*=)\s*\(?\s*(["'`])((?:(?!\1)[\s\S])*)\1/g)].map((m) => m[2]);
  const out = new Set();
  for (const s of strings) for (const c of selectorClasses(s)) out.add(c);
  return out;
}

function appSourceText() {
  const parts = [];
  const walk = (dir) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      if (e.name === "node_modules") continue;
      const abs = join(dir, e.name);
      if (e.isDirectory()) walk(abs);
      else if (/\.tsx?$/.test(e.name) && !/\.(test|stories)\.tsx?$/.test(e.name) && !e.name.endsWith("scroll-edges.tsx")) parts.push(readFileSync(abs, "utf8"));
    }
  };
  for (const d of ["app", "components", "features", "layouts"]) walk(join(root, d));
  return parts.join("\n");
}

/** Queried classes that no className in the app renders. */
export function staleClasses(queried, appText) {
  return [...queried].filter((c) => !new RegExp(`["'\`\\s]${c.replace(/[-]/g, "\\-")}(?=["'\`\\s$}])`).test(appText));
}

const HOOKS = [
  // [file, region start, region end]
  ["components/app/scroll-edges.tsx", "export const SCROLL_EDGE_SELECTOR", "\n"],
  ["scripts/smoke-tasks-click-matrix-live.mjs", "async function fingerprint(page)", "return { ...dom"],
  ["scripts/smoke-tasks-click-matrix-live.mjs", "// WHAT IS OPEN OVER THE PAGE", "const overlayOpen"],
];

test("stale-hook-selectors self-test: a class nothing renders is caught, a rendered one is not", () => {
  const app = 'const a = <div className="kit-tabs x" />; const b = `ltb-card-root${d ? " --dragging" : ""}`;';
  assert.deepEqual(staleClasses(new Set(["kit-tabs", "tablewrap", "--dragging", "ltb-card-root"]), app), ["tablewrap"]);
  assert.deepEqual([...selectorClasses('.drawer.on, .MuiDrawer-paper[role="dialog"], [data-x]')].sort(), ["drawer", "on"]);
});

test("DOM hooks only query classes the app still renders", () => {
  const app = appSourceText();
  const stale = [];
  for (const [file, start, end] of HOOKS) for (const c of staleClasses(hookRegion(file, start, end), app)) stale.push(`${file}: .${c}`);
  assert.deepEqual(stale, [], `hooks query classes nothing renders: ${stale.join(", ")}`);
});
