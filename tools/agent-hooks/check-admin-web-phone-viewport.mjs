#!/usr/bin/env node
// check-admin-web-phone-viewport.mjs -- the admin-web dashboard is opened on PHONES too
// (maintainer rule 2026-09-14): every page, table, chart and drawer must render at a phone-width
// viewport (~390px) without clipping, without breaking, and without the page body scrolling
// sideways. This is the STATIC half of that rule: it catches the CSS/JSX shapes that break a
// phone layout before a browser ever opens. The RUNTIME half is
// `npm --prefix apps/admin-web run smoke:visual:live`, whose 390px mobile lane fails on real
// horizontal overflow, un-scrollable wide tables and clipped text; both are required for a
// browser-visible change (AGENTS.md -> "Admin-web must render on a phone").
//
// Findings (each has a self-test fixture below):
//
//   fixed-px-width      `width` / `min-width` / `flex-basis` of >= 480px (CSS) or `width` /
//                       `minWidth` / `flexBasis` >= 480 (JSX style object) on an element that
//                       is NOT allowed to be wider than a phone. A 480px+ box inside a 390px
//                       viewport either clips or drags the page sideways. ALLOWED: a `table`,
//                       `svg`, `canvas`, `pre`, `img`, `video`, `iframe` or a selector naming a
//                       scroll container (`*tablewrap*`, `*scroll*`, `*-canvas`), because those
//                       are the things that MAY be wider than the screen inside their own
//                       `overflow-x:auto` wrapper; a declaration inside `@media (min-width: ...)`,
//                       because it never applies on a phone; and `max-width`, which is a cap.
//
//   px-grid-sum         `grid-template-columns` whose FLOOR in px -- plain `Npx` tracks plus the
//                       minimum of every `minmax(Npx, ...)` / `clamp(Npx, ...)`, times a numeric
//                       `repeat(N, ...)` -- adds up to more than 360px outside a
//                       `@media (min-width: ...)` block. A grid can never shrink below its floor,
//                       so it overflows a 390px viewport (16px gutters each side) no matter what
//                       its cells hold. Use `minmax(0, 1fr)`, `auto-fit`, or put the fixed layout
//                       under a min-width media query and stack on phones.
//
//   page-overflow-hidden `overflow-x: hidden` on a rule whose SUBJECT is `html`, `body`, `.main`,
//                       `.screen`, `.wrap` or `.page`. That hides the sideways scroll instead of
//                       fixing the box that caused it -- the clipped content is simply gone on a
//                       phone, with no way to reach it. Fix the box; do not hide the symptom.
//                       (`overflow: hidden` on `body:has(.modal.on)` is a scroll LOCK and is not
//                       this; only the `-x` symptom-hider is flagged.)
//
// A declaration is NOT flagged when the same file overrides it for phones -- the same selector
// sets the same property (width family, or grid-template-columns) inside a
// `@media (max-width: <=640px)` block. That is what responsive CSS looks like, and it is the
// fix the guard asks for.
//
// Escape hatch: `phone-viewport:ignore: <reason>` on the same line or the line above. It is a
// reviewer-facing justification (a print-only layout, a canvas that owns its own pan), never a
// rubber stamp.
//
// Whole-tree + count ratchet (docs/observability/GUARDRAIL_RATCHET.md): the tree carried debt
// before this guard existed, frozen per (file, rule) -> COUNT in
// tools/admin-web-phone-viewport/baseline.json. A file fails the moment its count for a rule
// exceeds the baseline; a count BELOW the baseline also fails ("stale-high") so fixed debt is
// taken off the books in the same change (`--update-baseline`). Adding to the baseline to make
// a new finding pass is not an accepted way to land code.
//
// Known blind spots, stated so nobody mistakes a green run for phone proof: Tailwind/utility
// class widths (`w-[600px]`), widths computed at runtime, `white-space: nowrap` on a page-level
// container, and any overflow that only appears with real data. The 390px lane of
// `smoke:visual:live` is what sees those.

import { execSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const ROOT = "apps/admin-web";
const BASELINE = "tools/admin-web-phone-viewport/baseline.json";
const PX_WIDTH_LIMIT = 480;
const GRID_PX_LIMIT = 360;
const IGNORE_MARK = "phone-viewport:ignore";

const WIDE_OK_ELEMENTS = /\b(table|thead|tbody|tr|svg|canvas|pre|img|video|iframe)\b/;
const WIDE_OK_SELECTOR = /(tablewrap|scroll|-canvas|-svg|chart-svg|print)/i;
const PAGE_LEVEL_SUBJECT = /^(html|body|\.main|\.screen|\.wrap|\.page)(:[a-z-]+(\([^)]*\))?)*$/;
const PHONE_MEDIA_MAX_PX = 640;
const WIDTH_FAMILY = new Set(["width", "min-width", "flex-basis"]);

function isSource(rel) {
  return (
    rel.startsWith(`${ROOT}/`) &&
    /\.(css|tsx)$/.test(rel) &&
    !/\.(test|spec|stories)\.(tsx)$/.test(rel) &&
    !rel.includes("/node_modules/") &&
    !rel.includes("/.next/") &&
    !rel.includes("/public/") &&
    // Storybook is a DESKTOP authoring surface, not a shipped phone screen: `storybook-static/`
    // is a build artifact that re-bundles every stylesheet the guard has already read from
    // source (so each finding would be counted twice), and `.storybook/` story decorators pin
    // deliberate 1440/1280/768 preview widths. Neither is a page a phone ever loads.
    !rel.includes("/storybook-static/") &&
    !rel.includes("/.storybook/")
  );
}

function walk(dir, acc = []) {
  let entries = [];
  try {
    entries = readdirSync(dir);
  } catch {
    return acc;
  }
  for (const entry of entries) {
    const full = join(dir, entry);
    const stat = statSync(full);
    if (stat.isDirectory()) {
      if (["node_modules", ".next", "build", ".turbo", "public", "storybook-static", ".storybook"].includes(entry)) continue;
      walk(full, acc);
      continue;
    }
    const rel = relative(repo, full);
    if (isSource(rel)) acc.push(rel);
  }
  return acc;
}

function lineOf(text, index) {
  return text.slice(0, index).split("\n").length;
}

function ignoredAt(lines, lineNo) {
  const here = lines[lineNo - 1] ?? "";
  const above = lines[lineNo - 2] ?? "";
  return here.includes(IGNORE_MARK) || above.includes(IGNORE_MARK);
}

// gridFloorPx is the smallest width a grid-template-columns value can ever lay out to: plain
// px tracks, the MIN of minmax()/clamp(), numeric repeat() multiplied, auto-fit/auto-fill and
// fr/auto/% tracks counted as 0.
function gridFloorPx(value) {
  const floorOf = (track) => {
    const t = track.trim();
    let m = /^minmax\(\s*([^,]+),/.exec(t) || /^clamp\(\s*([^,]+),/.exec(t);
    if (m) return floorOf(m[1]);
    m = /^(\d+(?:\.\d+)?)px$/.exec(t);
    return m ? Number(m[1]) : 0;
  };
  const splitTop = (s) => {
    const out = [];
    let depth = 0;
    let cur = "";
    for (const ch of s) {
      if (ch === "(") depth += 1;
      if (ch === ")") depth -= 1;
      if (/\s/.test(ch) && depth === 0) {
        if (cur) out.push(cur);
        cur = "";
        continue;
      }
      cur += ch;
    }
    if (cur) out.push(cur);
    return out;
  };
  let total = 0;
  for (const track of splitTop(value)) {
    const rep = /^repeat\(\s*([^,]+),(.*)\)$/s.exec(track);
    if (rep) {
      const n = /^\d+$/.test(rep[1].trim()) ? Number(rep[1].trim()) : 1;
      total += n * splitTop(rep[2]).reduce((acc, t) => acc + floorOf(t), 0);
      continue;
    }
    total += floorOf(track);
  }
  return total;
}

// A rule's subject is the last compound of each comma-separated selector.
function selectorSubjects(selector) {
  return selector.split(",").map((part) => part.trim().split(/\s*[\s>+~]\s*/).pop() ?? "");
}

function normalizeSelector(selector) {
  return selector.replace(/\s+/g, " ").trim();
}

// ---- CSS -------------------------------------------------------------------------------------
// A small block walker: tracks the enclosing @media prelude and the selector of each rule so a
// declaration can be judged by where it sits. Comments are blanked (keeping newlines) first.

function stripComments(css) {
  return css.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "));
}

// walkCss calls visit(rule) for every style rule with its selector, body, body offset, and the
// media context it sits in (inside a min-width query; inside a phone-width max-width query).
function walkCss(css, visit) {
  const stack = []; // { kind: 'media'|'rule'|'other', prelude, minWidthMedia, phoneMedia }
  let i = 0;
  let preludeStart = 0;
  while (i < css.length) {
    const ch = css[i];
    if (ch === "{") {
      const prelude = css.slice(preludeStart, i).trim();
      const kind = prelude.startsWith("@media") ? "media" : prelude.startsWith("@") ? "other" : "rule";
      const maxPx = kind === "media" ? /max-width\s*:\s*(\d+)px/.exec(prelude) : null;
      stack.push({
        kind,
        prelude,
        minWidthMedia: kind === "media" && /min-width\s*:/.test(prelude),
        phoneMedia: Boolean(maxPx && Number(maxPx[1]) <= PHONE_MEDIA_MAX_PX),
      });
      preludeStart = i + 1;
      i += 1;
      continue;
    }
    if (ch === "}") {
      const body = css.slice(preludeStart, i);
      const top = stack[stack.length - 1];
      if (top && top.kind === "rule") {
        visit({
          selector: top.prelude,
          body,
          bodyStart: preludeStart,
          minWidthMedia: stack.some((s) => s.minWidthMedia),
          phoneMedia: stack.some((s) => s.phoneMedia),
        });
      }
      stack.pop();
      preludeStart = i + 1;
      i += 1;
      continue;
    }
    if (ch === ";") {
      // Declarations are judged at the closing brace of their rule; a stray `;` between rules
      // just moves the prelude start.
      const top = stack[stack.length - 1];
      if (!top || top.kind !== "rule") preludeStart = i + 1;
      i += 1;
      continue;
    }
    i += 1;
  }
}

function declarations(body) {
  return [...body.matchAll(/([a-zA-Z-]+)\s*:\s*([^;]+)/g)].map((m) => ({
    prop: m[1].toLowerCase(),
    value: m[2].trim(),
    offset: m.index ?? 0,
  }));
}

function scanCss(rel, source) {
  const css = stripComments(source);
  const lines = source.split("\n");
  const findings = [];

  // Pass 1: what the file already overrides for phones. selector -> set of property families.
  const phoneOverrides = new Map();
  walkCss(css, ({ selector, body, phoneMedia }) => {
    if (!phoneMedia) return;
    for (const part of selector.split(",")) {
      const key = normalizeSelector(part);
      const families = phoneOverrides.get(key) ?? new Set();
      // Only a declaration that is itself phone-safe counts as an override: a 640px width set
      // INSIDE the phone query is the defect, not the fix.
      for (const d of declarations(body)) {
        const px = /^(\d+(?:\.\d+)?)px$/.exec(d.value);
        const wide = px && Number(px[1]) >= PX_WIDTH_LIMIT;
        if ((WIDTH_FAMILY.has(d.prop) && !wide) || d.prop === "max-width") families.add("width");
        if ((d.prop === "grid-template-columns" && gridFloorPx(d.value) <= GRID_PX_LIMIT) || d.prop === "display") families.add("grid");
      }
      phoneOverrides.set(key, families);
    }
  });
  const overriddenForPhones = (selector, family) =>
    selector.split(",").every((part) => phoneOverrides.get(normalizeSelector(part))?.has(family));

  // Pass 2: judge every declaration outside a min-width query.
  walkCss(css, ({ selector, body, bodyStart, minWidthMedia }) => {
    if (minWidthMedia) return;
    for (const d of declarations(body)) {
      const lineNo = lineOf(css, bodyStart + d.offset);
      if (ignoredAt(lines, lineNo)) continue;
      if (WIDTH_FAMILY.has(d.prop)) {
        const px = /^(\d+(?:\.\d+)?)px$/.exec(d.value);
        if (
          px &&
          Number(px[1]) >= PX_WIDTH_LIMIT &&
          !WIDE_OK_ELEMENTS.test(selector) &&
          !WIDE_OK_SELECTOR.test(selector) &&
          !overriddenForPhones(selector, "width")
        ) {
          findings.push({ file: rel, line: lineNo, rule: "fixed-px-width", detail: `${selector} { ${d.prop}: ${d.value} }` });
        }
      }
      if (d.prop === "grid-template-columns") {
        const floor = gridFloorPx(d.value);
        if (floor > GRID_PX_LIMIT && !overriddenForPhones(selector, "grid")) {
          findings.push({ file: rel, line: lineNo, rule: "px-grid-sum", detail: `${selector} { grid-template-columns: ${d.value} } (${floor}px floor)` });
        }
      }
      if (d.prop === "overflow-x" && /\bhidden\b/.test(d.value) && selectorSubjects(selector).some((s) => PAGE_LEVEL_SUBJECT.test(s))) {
        findings.push({ file: rel, line: lineNo, rule: "page-overflow-hidden", detail: `${selector} { overflow-x: ${d.value} }` });
      }
    }
  });
  return findings;
}

// ---- JSX -------------------------------------------------------------------------------------
// Inline style objects: `style={{ width: 640 }}`, `minWidth: "720px"`, `flexBasis: 560`.
// A JSX element name is read back from the nearest `<tag` before the match so a `<table
// style={{minWidth: 900}}>` stays allowed.

function scanTsx(rel, source) {
  const lines = source.split("\n");
  const findings = [];
  const re = /\b(width|minWidth|flexBasis)\s*:\s*(?:"|')?(\d+(?:\.\d+)?)(?:px)?(?:"|')?\s*[,}]/g;
  for (const m of source.matchAll(re)) {
    const n = Number(m[2]);
    if (n < PX_WIDTH_LIMIT) continue;
    const at = m.index ?? 0;
    const lineNo = lineOf(source, at);
    if (ignoredAt(lines, lineNo)) continue;
    const before = source.slice(Math.max(0, at - 600), at);
    const tag = [...before.matchAll(/<([A-Za-z][A-Za-z0-9.]*)/g)].pop()?.[1] ?? "";
    if (WIDE_OK_ELEMENTS.test(tag) || WIDE_OK_SELECTOR.test(before.slice(-200))) continue;
    findings.push({ file: rel, line: lineNo, rule: "fixed-px-width", detail: `<${tag || "?"} style ${m[1]}: ${m[2]}>` });
  }
  return findings;
}

function scanFile(rel, root = repo) {
  const source = readFileSync(join(root, rel), "utf8");
  return rel.endsWith(".css") ? scanCss(rel, source) : scanTsx(rel, source);
}

// ---- baseline ratchet --------------------------------------------------------------------------

function countByKey(findings) {
  const counts = new Map();
  for (const f of findings) {
    const key = `${f.file}|${f.rule}`;
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  return counts;
}

function loadBaseline() {
  if (!existsSync(join(repo, BASELINE))) return new Map();
  const parsed = JSON.parse(readFileSync(join(repo, BASELINE), "utf8"));
  return new Map(Object.entries(parsed.entries ?? {}));
}

function writeBaseline(counts) {
  const entries = Object.fromEntries([...counts.entries()].sort(([a], [b]) => a.localeCompare(b)));
  writeFileSync(
    join(repo, BASELINE),
    `${JSON.stringify({ _comment: "Count of known admin-web phone-viewport findings per file|rule. Shrink-only: regenerate with `node tools/agent-hooks/check-admin-web-phone-viewport.mjs --update-baseline` after FIXING debt; never grow it to land a new finding.", entries }, null, 2)}\n`,
  );
}

function main() {
  const args = process.argv.slice(2);
  if (args.includes("--self-test")) return selfTest();
  const files = walk(join(repo, ROOT));
  const findings = files.flatMap((rel) => scanFile(rel));
  const counts = countByKey(findings);

  if (args.includes("--list")) {
    for (const f of findings) console.log(`${f.file}:${f.line} [${f.rule}] ${f.detail}`);
    console.log(`admin-web-phone-viewport: ${findings.length} finding(s) listed (baseline not consulted)`);
    return 0;
  }

  if (args.includes("--update-baseline")) {
    const old = loadBaseline();
    for (const [key, n] of counts) {
      const was = old.get(key) ?? 0;
      if (n > was) console.log(`  +${was === 0 ? "NEW" : "MORE"} ${key} ${was} -> ${n}`);
    }
    for (const [key, was] of old) {
      const n = counts.get(key) ?? 0;
      if (n < was) console.log(`  -${n === 0 ? "GONE" : "LESS"} ${key} ${was} -> ${n}`);
    }
    writeBaseline(counts);
    console.log(`admin-web-phone-viewport: baseline written (${counts.size} file|rule entries, ${findings.length} findings)`);
    return 0;
  }

  const baseline = loadBaseline();
  const increased = [];
  const decreased = [];
  for (const [key, n] of counts) {
    const was = baseline.get(key) ?? 0;
    if (n > was) increased.push({ key, was, n });
  }
  for (const [key, was] of baseline) {
    const n = counts.get(key) ?? 0;
    if (n < was) decreased.push({ key, was, n });
  }
  if (increased.length) {
    console.error("admin-web-phone-viewport: FAIL -- new phone-breaking layout findings (the dashboard is opened on phones; see AGENTS.md 'Admin-web must render on a phone'):");
    for (const { key, was, n } of increased) {
      console.error(`  ${key}: ${was} -> ${n}`);
      for (const f of findings.filter((x) => `${x.file}|${x.rule}` === key)) console.error(`    ${f.file}:${f.line} [${f.rule}] ${f.detail}`);
    }
    console.error("Fix the box (fluid width, minmax(0,1fr), a min-width media query, or an overflow-x:auto wrapper on the table/chart), or justify with `phone-viewport:ignore: <reason>`.");
  }
  if (decreased.length) {
    console.error("admin-web-phone-viewport: FAIL -- baseline is stale-high (debt was fixed; take it off the books in the same change with --update-baseline):");
    for (const { key, was, n } of decreased) console.error(`  ${key}: ${was} -> ${n}`);
  }
  if (increased.length || decreased.length) return 1;
  console.log(`admin-web-phone-viewport: PASS (${files.length} files, ${findings.length} baselined findings across ${counts.size} file|rule entries, zero new)`);
  return 0;
}

// ---- self-test ---------------------------------------------------------------------------------

function selfTest() {
  const dir = mkdtempSync(join(tmpdir(), "phone-viewport-"));
  const cases = [
    // fixed-px-width
    { name: "css-fixed-width-on-div", file: "a.css", src: ".panel{width:640px}", want: ["fixed-px-width"] },
    { name: "css-min-width-on-div", file: "a.css", src: ".grid-card{min-width:520px}", want: ["fixed-px-width"] },
    { name: "css-table-min-width-ok", file: "a.css", src: ".main table.people-table{min-width:1120px}", want: [] },
    { name: "css-tablewrap-ok", file: "a.css", src: ".lt-tablewrap{min-width:900px}", want: [] },
    { name: "css-max-width-ok", file: "a.css", src: ".wrap{max-width:1320px}", want: [] },
    { name: "css-under-limit-ok", file: "a.css", src: ".chip{min-width:120px}", want: [] },
    { name: "css-inside-min-width-media-ok", file: "a.css", src: "@media (min-width:900px){.panel{width:640px}}", want: [] },
    { name: "css-inside-max-width-media-still-fails", file: "a.css", src: "@media (max-width:600px){.panel{width:640px}}", want: ["fixed-px-width"] },
    { name: "css-ignore-marker", file: "a.css", src: "/* phone-viewport:ignore: print sheet */\n.print-sheet{width:794px}", want: [] },
    { name: "css-comment-does-not-hide", file: "a.css", src: "/* width:100px */.panel{width:640px}", want: ["fixed-px-width"] },
    { name: "css-phone-override-exempts", file: "a.css", src: ".modal{width:620px}@media (max-width:600px){.modal{width:auto}}", want: [] },
    { name: "css-tablet-override-does-not-exempt", file: "a.css", src: ".modal{width:620px}@media (max-width:1024px){.modal{width:auto}}", want: ["fixed-px-width"] },
    { name: "css-override-of-another-selector-does-not-exempt", file: "a.css", src: ".modal{width:620px}@media (max-width:600px){.drawer{width:auto}}", want: ["fixed-px-width"] },
    { name: "tsx-inline-width", file: "a.tsx", src: 'const x = <div style={{ width: 640 }} />;', want: ["fixed-px-width"] },
    { name: "tsx-inline-min-width-px-string", file: "a.tsx", src: 'const x = <section style={{ minWidth: "720px", gap: 4 }} />;', want: ["fixed-px-width"] },
    { name: "tsx-inline-table-ok", file: "a.tsx", src: 'const x = <table style={{ minWidth: 900 }} />;', want: [] },
    { name: "tsx-inline-small-ok", file: "a.tsx", src: 'const x = <div style={{ width: 240 }} />;', want: [] },
    { name: "tsx-ignore-marker", file: "a.tsx", src: '// phone-viewport:ignore: canvas owns its own pan\nconst x = <div style={{ width: 640 }} />;', want: [] },
    // px-grid-sum
    { name: "css-grid-px-sum", file: "a.css", src: ".metagrid{grid-template-columns:200px 200px 120px}", want: ["px-grid-sum"] },
    { name: "css-grid-fluid-ok", file: "a.css", src: ".metagrid{grid-template-columns:repeat(2,minmax(0,1fr))}", want: [] },
    { name: "css-grid-small-px-ok", file: "a.css", src: ".row{grid-template-columns:26px minmax(0,1fr)}", want: [] },
    { name: "css-grid-in-min-width-media-ok", file: "a.css", src: "@media (min-width:1024px){.metagrid{grid-template-columns:200px 200px 120px}}", want: [] },
    { name: "css-grid-minmax-counts-floor-only", file: "a.css", src: ".g{grid-template-columns:minmax(0,1fr) minmax(300px,360px)}", want: [] },
    { name: "css-grid-minmax-floor-over-limit", file: "a.css", src: ".g{grid-template-columns:minmax(200px,1fr) minmax(200px,1fr)}", want: ["px-grid-sum"] },
    { name: "css-grid-repeat-multiplies", file: "a.css", src: ".g{grid-template-columns:repeat(3,130px)}", want: ["px-grid-sum"] },
    { name: "css-grid-auto-fit-counts-once", file: "a.css", src: ".g{grid-template-columns:repeat(auto-fit,minmax(240px,1fr))}", want: [] },
    { name: "css-grid-phone-override-exempts", file: "a.css", src: ".g{grid-template-columns:200px 200px}@media (max-width:600px){.g{grid-template-columns:1fr}}", want: [] },
    // page-overflow-hidden
    { name: "css-body-overflow-x-hidden", file: "a.css", src: "body{overflow-x:hidden}", want: ["page-overflow-hidden"] },
    { name: "css-html-body-list-overflow-x-hidden", file: "a.css", src: "html,body{overflow-x:hidden}", want: ["page-overflow-hidden"] },
    { name: "css-main-overflow-x-hidden", file: "a.css", src: ".main{overflow-x:hidden}", want: ["page-overflow-hidden"] },
    { name: "css-body-scroll-lock-is-not-flagged", file: "a.css", src: "body:has(.modal.on){overflow:hidden}", want: [] },
    { name: "css-card-overflow-hidden-ok", file: "a.css", src: ".card{overflow-x:hidden}", want: [] },
    { name: "css-descendant-of-main-is-not-page-level", file: "a.css", src: ".main table.x td .celllink{overflow-x:hidden}", want: [] },
    { name: "css-mainx-not-page-level", file: "a.css", src: ".mainx-badge{overflow-x:hidden}", want: [] },
    // two findings in one file count twice (the ratchet is count-aware)
    { name: "css-two-findings", file: "a.css", src: ".a{width:640px}.b{width:700px}", want: ["fixed-px-width", "fixed-px-width"] },
  ];
  let failed = 0;
  try {
    for (const c of cases) {
      const rel = `${ROOT}/${c.file}`;
      const full = join(dir, rel);
      execSync(`mkdir -p "${join(dir, ROOT)}"`);
      writeFileSync(full, c.src);
      const got = scanFile(rel, dir).map((f) => f.rule);
      const ok = JSON.stringify(got) === JSON.stringify(c.want);
      if (!ok) {
        failed += 1;
        console.error(`  self-test FAIL ${c.name}: got ${JSON.stringify(got)} want ${JSON.stringify(c.want)}`);
      }
    }
    // The count ratchet: a second finding in an already-baselined file must read as an increase.
    const counts = countByKey([
      { file: "x.css", rule: "fixed-px-width" },
      { file: "x.css", rule: "fixed-px-width" },
    ]);
    if (counts.get("x.css|fixed-px-width") !== 2) {
      failed += 1;
      console.error("  self-test FAIL count-ratchet: stacked findings must count, not merely exist");
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
  if (failed) {
    console.error(`admin-web-phone-viewport self-test: ${failed} case(s) failed`);
    return 1;
  }
  console.log(`admin-web-phone-viewport self-test: PASS (${cases.length} cases)`);
  return 0;
}

process.exit(main());
