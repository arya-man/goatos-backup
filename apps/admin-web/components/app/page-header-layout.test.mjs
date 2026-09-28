import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

// guard: page-header-layout-twin (TR3-P0-1/P0-3). A page header whose actions sit inline with the
// title (or always below it) on a phone must hand the SAME PageHeaderLayout constant to its loading
// twin, or the skeleton wraps differently and every block under it is offset.
const appRoot = new URL("../../", import.meta.url).pathname;
const walk = (dir) =>
  readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (name === "node_modules" || name.startsWith(".")) return [];
    return statSync(path).isDirectory() ? walk(path) : /\.tsx?$/.test(name) ? [path] : [];
  });
const files = ["app", "features", "components"].flatMap((d) => walk(join(appRoot, d))).map((path) => ({ path, text: readFileSync(path, "utf8") }));

const uses = (tag) => {
  const out = [];
  const re = new RegExp(`<${tag}\\b[^>]*?\\blayout=\\{([^}]+)\\}`, "gs");
  for (const f of files) for (const m of f.text.matchAll(re)) out.push({ file: f.path.slice(appRoot.length), value: m[1].trim() });
  return out;
};

test("guard: page-header-layout-twin - every PageHeader layout is a named constant its skeleton twin also passes", () => {
  const pages = uses("PageHeader");
  const skels = uses("PageHeaderSkeleton");
  assert.ok(pages.length >= 3, "herd, health config and the SOP library declare a layout");
  for (const p of pages) {
    assert.match(p.value, /^[A-Z][A-Z0-9_]+$/, `${p.file}: PageHeader layout must be a named constant, not ${p.value}`);
    assert.ok(skels.some((s) => s.value === p.value), `${p.file}: no PageHeaderSkeleton passes layout={${p.value}}`);
  }
  for (const s of skels) assert.ok(pages.some((p) => p.value === s.value), `${s.file}: PageHeaderSkeleton layout={${s.value}} has no PageHeader using it`);
});

test("guard: page-header-layout-twin - PageHeader and PageHeaderSkeleton size the header from the same helpers", () => {
  const header = readFileSync(join(appRoot, "components/app/page-header.tsx"), "utf8");
  const blocks = readFileSync(join(appRoot, "components/app/skeletons/blocks.tsx"), "utf8");
  for (const src of [header, blocks]) {
    assert.match(src, /sx=\{pageHeaderLayoutSx\(layout\)\}/);
    assert.match(src, /sx=\{pageHeaderActionsSx\(layout\)\}/);
  }
});

// guard: filter-bar-fold-one-row (TR3-P1-1): at 390 the folded SOP bar kept search + Filters on row 1
// and dropped the ⋮ alone onto row 2 (a 240px search basis). The fold basis is shared with the twin.
test("guard: filter-bar-fold-one-row - folded FilterBar search uses the small phone basis, and so does its twin", () => {
  const widths = readFileSync(join(appRoot, "components/app/filter-field-widths.ts"), "utf8");
  const bar = readFileSync(join(appRoot, "components/app/filter-bar.tsx"), "utf8");
  const sop = readFileSync(join(appRoot, "features/sops/sop-route-skeleton.tsx"), "utf8");
  const xs = Number(widths.match(/FILTER_SEARCH_FOLD_BASIS = \{ xs: (\d+)/)?.[1]);
  // 358px row at 390: search basis + 16 gap + ~96 Filters + 16 gap + 44 ⋮ must fit.
  assert.ok(xs > 0 && xs + 16 + 96 + 16 + 44 <= 358, `fold search basis ${xs} leaves no room for the ⋮`);
  assert.match(bar, /flexBasis: foldable \? FILTER_SEARCH_FOLD_BASIS : FILTER_SEARCH_BASIS/);
  assert.match(sop, /searchBasis=\{\{ \.\.\.FILTER_SEARCH_FOLD_BASIS \}\}/);
});
