// guard: crumb-link-truth (REVIEW-46 O76, REVIEW-48 O78). PageHeaderSkeleton's parent crumb is a link by
// default (a 44px tap box below md); a page whose parent crumb has no href renders a 22px text crumb.
// EVERY route loading.tsx is checked: the page's parent crumb is read from the `crumbs={[...]}` literals
// reachable from its page.tsx (through the feature modules it imports), the skeleton's crumbLink from
// the PageHeaderSkeleton uses reachable from its loading.tsx; a mismatch fails. A route whose crumbs
// cannot be read statically is listed in UNRESOLVED with the reason.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../../", import.meta.url));
const admin = join(root, "app", "(admin)");

/** Whether each `crumbs={[{ … }` literal's first (parent) crumb carries an href. */
export function parentCrumbLinks(src) {
  return [...src.matchAll(/crumbs=\{\[\s*\{([^{}]*(?:\([^()]*\)[^{}]*)*)\}/g)].map((m) => /\bhref\s*:/.test(m[1]));
}

/** Each `<PageHeaderSkeleton …/>` tag's props (braces balanced, so JSX props such as tabs={<Box …>} are kept whole). */
function headerSkeletonProps(src) {
  const out = [];
  for (let i = src.indexOf("<PageHeaderSkeleton"); i >= 0; i = src.indexOf("<PageHeaderSkeleton", i + 1)) {
    let depth = 0;
    let j = i + "<PageHeaderSkeleton".length;
    for (; j < src.length; j++) {
      const c = src[j];
      if (c === "{") depth++;
      else if (c === "}") depth--;
      else if (depth === 0 && (c === ">" || (c === "/" && src[j + 1] === ">"))) break;
    }
    out.push(src.slice(i, j));
  }
  return out;
}

/** The skeleton's crumbLink (default true) per PageHeaderSkeleton use that shows crumbs. */
export function skeletonCrumbLinks(src) {
  return headerSkeletonProps(src).filter((p) => !/crumbs=\{false\}/.test(p)).map((p) => !/crumbLink=\{false\}/.test(p));
}

/**
 * Routes whose page crumbs are not a `crumbs={[...]}` literal, with the parent crumb's link state read
 * by hand (`linked`, checked against the skeleton like a literal) and the reason. `linked: null` = the
 * page draws no PageHeader trail (the skeleton's crumb row is then not checked here).
 */
const UNRESOLVED = {
  "/action-center": { linked: true, reason: "crumbItems: the section crumb links to / (href: \"/\")" },
  "/protocol-adherence": { linked: true, reason: "crumbItems: the section crumb links to / (href: \"/\")" },
  "/workflows": { linked: true, reason: "crumbItems: the section crumb links to / (href: \"/\")" },
  "/workflows/[row_id]": { linked: null, reason: "the drilldown header is built from the workflow record" },
  "/sales/buyer-analytics": { linked: true, reason: "SalesChrome: [{ label: crumb, href: \"/sales\" }, …] unless the crumb repeats the title (then no trail)" },
  "/sales/farm-born": { linked: true, reason: "SalesChrome (see /sales/buyer-analytics)" },
  "/sales/farm-value": { linked: true, reason: "SalesChrome (see /sales/buyer-analytics)" },
  "/sales/market-analytics": { linked: true, reason: "SalesChrome (see /sales/buyer-analytics)" },
  "/sales/sold": { linked: true, reason: "SalesChrome (see /sales/buyer-analytics)" },
  "/sales/config": { linked: true, reason: "SalesPageHeader (SalesChrome, see /sales/buyer-analytics)" },
  "/procurement/source-entry/loads/[load_id]": { linked: null, reason: "OrderDetailsToolbar with a back link, no PageHeader trail" },
};

function resolveImport(spec, from) {
  let base;
  if (spec.startsWith("@/")) base = join(root, spec.slice(2));
  else if (spec.startsWith(".")) base = resolve(dirname(from), spec);
  else return null;
  for (const cand of [base, `${base}.tsx`, `${base}.ts`, join(base, "index.tsx"), join(base, "index.ts")]) if (existsSync(cand) && statSync(cand).isFile()) return cand;
  return null;
}

/** name -> import spec for every named / default import in `src`. */
function importMap(src) {
  const map = new Map();
  for (const m of src.matchAll(/import\s+(?:type\s+)?([\s\S]*?)\s+from\s+"([^"]+)"/g)) {
    const clause = m[1];
    const def = /^([A-Za-z_$][\w$]*)/.exec(clause);
    if (def && !clause.startsWith("{")) map.set(def[1], { spec: m[2], name: "default" });
    const named = /\{([^}]*)\}/.exec(clause);
    if (named) for (const part of named[1].split(",")) {
      const [orig, alias] = part.trim().replace(/^type\s+/, "").split(/\s+as\s+/);
      if (orig) map.set((alias ?? orig).trim(), { spec: m[2], name: orig.trim() });
    }
  }
  return map;
}

/** The file that defines export `name` of module `file` (following re-exports). */
function definingFile(file, name, depth = 0) {
  if (!file || depth > 5) return null;
  const src = readFileSync(file, "utf8");
  if (name === "default" ? /export\s+default\b/.test(src) : new RegExp(`export\\s+(?:async\\s+)?(?:function|const|class)\\s+${name}\\b`).test(src)) return file;
  for (const m of src.matchAll(/export\s+\{([^}]*)\}\s+from\s+"([^"]+)"/g)) {
    for (const part of m[1].split(",")) {
      const [orig, alias] = part.trim().split(/\s+as\s+/);
      if ((alias ?? orig)?.trim() === name) return definingFile(resolveImport(m[2], file), orig.trim(), depth + 1);
    }
  }
  for (const m of src.matchAll(/export\s+\*\s+from\s+"([^"]+)"/g)) {
    const hit = definingFile(resolveImport(m[1], file), name, depth + 1);
    if (hit) return hit;
  }
  return null;
}

/** Files that define the JSX components rendered from `entry`, followed `depth` levels into features/. */
function rendered(entry, depth) {
  const seen = new Set([entry]);
  let frontier = [entry];
  for (let d = 0; d < depth; d++) {
    const next = [];
    for (const f of frontier) {
      const src = readFileSync(f, "utf8");
      const imports = importMap(src);
      // JSX components and imported render helpers called directly (renderSopModulePage(...)).
      const used = new Set([...[...src.matchAll(/<([A-Z][\w$]*)/g)].map((m) => m[1]), ...[...src.matchAll(/\b(render[A-Z][\w$]*)\(/g)].map((m) => m[1])]);
      for (const tag of used) {
        const imp = imports.get(tag);
        if (!imp) continue;
        const def = definingFile(resolveImport(imp.spec, f), imp.name);
        if (def && !seen.has(def) && inFeatures(def)) { seen.add(def); next.push(def); }
      }
    }
    frontier = next;
  }
  return [...seen];
}

const inFeatures = (f) => relative(root, f).startsWith("features/");
const walk = (dir) => readdirSync(dir).flatMap((n) => {
  const full = join(dir, n);
  return statSync(full).isDirectory() ? walk(full) : n === "loading.tsx" ? [full] : [];
});

test("self-test: parent crumb href detection", () => {
  assert.deepEqual(parentCrumbLinks('crumbs={[{ label: copy(c, "crumb"), href: "/counts/herd" }, { label: t }]}'), [true]);
  assert.deepEqual(parentCrumbLinks('crumbs={[{ label: copy(c, "crumb") }, { label: t }]}'), [false]);
  assert.deepEqual(skeletonCrumbLinks("<PageHeaderSkeleton crumbLink={false} titleWidth={1} />"), [false]);
  assert.deepEqual(skeletonCrumbLinks("<PageHeaderSkeleton actionWidths={[1]} />"), [true]);
  assert.deepEqual(skeletonCrumbLinks("<PageHeaderSkeleton crumbs={false} />"), []);
  assert.deepEqual(skeletonCrumbLinks("<PageHeaderSkeleton\n  actions={2}\n  tabs={<Box sx={{ mb: 2 }}><TabsSkeleton count={3} /></Box>}\n  crumbLink={false}\n/>"), [false]);
});

test("crumb-link-truth: every route skeleton's crumbLink equals its page's parent crumb", () => {
  const problems = [];
  const checked = [];
  for (const loading of walk(admin)) {
    const dir = dirname(loading);
    const route = "/" + relative(admin, dir).split("/").filter((s) => !/^\(.*\)$/.test(s)).join("/");
    const page = join(dir, "page.tsx");
    if (!existsSync(page)) continue;
    const skelLinks = rendered(loading, 2).flatMap((f) => skeletonCrumbLinks(readFileSync(f, "utf8")));
    if (!skelLinks.length) continue; // the skeleton shows no crumb row
    const pageLinks = rendered(page, 5).flatMap((f) => parentCrumbLinks(readFileSync(f, "utf8")));
    let linked;
    if (route in UNRESOLVED) linked = UNRESOLVED[route].linked;
    else if (!pageLinks.length) {
      problems.push(`${route}: no crumbs={[...]} literal reachable from page.tsx (list it in UNRESOLVED with the reason)`);
      continue;
    } else linked = pageLinks.every(Boolean) ? true : pageLinks.every((l) => !l) ? false : undefined;
    if (linked === null) continue;
    if (linked === undefined) {
      problems.push(`${route}: its reachable PageHeaders disagree on the parent crumb link (list it in UNRESOLVED)`);
      continue;
    }
    checked.push(route);
    for (const l of skelLinks) if (l !== linked) problems.push(`${route}: PageHeaderSkeleton crumbLink must be ${linked} (the page's parent crumb ${linked ? "links" : "has no href"})`);
  }
  // The resolver must keep reaching the tree's skeletons (54 route skeletons on 2026-09-28).
  assert.ok(checked.length >= 50, `only ${checked.length} route skeletons resolved: the import walk broke`);
  assert.deepEqual(problems, []);
});
