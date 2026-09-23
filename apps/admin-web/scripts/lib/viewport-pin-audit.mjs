// Why is this reload check pinned to one width, and may it be widened?
//
// 65 runnable entries run at ONE viewport. Not one of them says why. An
// undocumented pin is indistinguishable from an oversight, so half the reload
// surface was unjudged at the other width with nothing recording whether that
// was a decision or a gap.
//
// Widening them blind is the one thing that must not happen: admin-web branches
// on width in JavaScript (`use-is-mobile`, `useIsDesktop`) as well as in CSS, so
// the DOM genuinely differs between 1440 and 390. A check widened onto a width
// whose page does not draw that element fires on a correct page, which §2 calls
// worse than no check at all.
//
// So this derives the answer from the repo instead of guessing at it, and FAILS
// CLOSED: anything it cannot prove safe stays pinned and is reported as a named
// finding for someone with a browser, never widened on a hunch.
//
// It reads source. It opens nothing.
import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";

/** Widths the sweep visits. A max-width block applies when the width is <= its bound. */
export const WIDTHS = Object.freeze({ laptop: 1440, mobile: 390 });

/** Every css selector an entry depends on — what it asserts AND what it clicks. */
export function selectorsOf(entry) {
  const out = [];
  const take = (t) => { if (t && typeof t === "object" && t.css) out.push(t.css); };
  for (const step of entry.steps ?? []) take(step.click);
  for (const expect of entry.expect ?? []) {
    take(expect.visible); take(expect.absent); take(expect.count); take(expect.equals);
    take(expect.compare?.left); take(expect.compare?.right);
  }
  return out;
}

/** The class names a selector depends on, in source order. */
export function classesIn(selector) {
  return [...String(selector).matchAll(/\.([A-Za-z0-9_-]+)/g)].map((m) => m[1]);
}

/**
 * Media blocks in a stylesheet, as { bound, body }.
 *
 * Only `max-width` is parsed because that is all this stylesheet uses (126 of
 * 126 width queries). A query shape this does not understand is returned with a
 * bound of Infinity, so it counts as applying at EVERY width — the cautious
 * reading, which can only ever hold a widen back.
 */
export function mediaBlocks(css) {
  const blocks = [];
  const re = /@media([^{]*)\{/g;
  for (let m = re.exec(css); m; m = re.exec(css)) {
    const header = m[1];
    if (!/width/.test(header)) continue;
    const max = header.match(/max-width\s*:\s*(\d+)px/);
    const bound = max ? Number(max[1]) : Number.POSITIVE_INFINITY;
    // Walk braces from the opening one to find this block's body.
    let depth = 1;
    let i = m.index + m[0].length;
    const start = i;
    for (; i < css.length && depth > 0; i += 1) {
      if (css[i] === "{") depth += 1;
      else if (css[i] === "}") depth -= 1;
    }
    blocks.push({ bound, body: css.slice(start, i - 1) });
  }
  return blocks;
}

/** Does any media block that APPLIES at `width` set display:none on this class? */
export function hiddenAtWidth(css, className, width) {
  for (const block of mediaBlocks(css)) {
    if (width > block.bound) continue; // this block does not apply at that width
    const re = new RegExp(`\\.${className}\\b[^{}]*\\{[^}]*display\\s*:\\s*none`, "s");
    if (re.test(block.body)) return { hidden: true, bound: block.bound };
  }
  return { hidden: false };
}

const VIEWPORT_JS = /useIsMobile|useIsDesktop|useMediaQuery|matchMedia|window\.innerWidth/;

/** Source files that mention a class name, and whether any of them branches on width in JS. */
export function sourcesFor(root, className, cache) {
  if (cache?.has(className)) return cache.get(className);
  const files = cache?.get("__files__") ?? listSources(root);
  cache?.set("__files__", files);
  const hits = [];
  for (const { file, text } of files) {
    if (text.includes(className)) hits.push({ file, viewportJs: VIEWPORT_JS.test(text) });
  }
  cache?.set(className, hits);
  return hits;
}

/** The folders this audit claims to have read. All of them must exist. */
export const SOURCE_ROOTS = Object.freeze(["app", "features", "components"]);

/**
 * Read every component source, and SAY WHAT ARRIVED.
 *
 * This used to swallow a missing folder — "a missing folder is not a verdict" —
 * and that was wrong in the most dangerous direction available. A folder that
 * failed to read contributed zero files silently, every class came back
 * "no source mentions this", and the audit still printed a fraction. Measured:
 * with the source root missing it reported 11 of 66 rather than failing, and
 * with an EMPTY stylesheet it reported 34 of 66 — HIGHER than the true 33,
 * because nothing looks hidden when there is no CSS. A missing input made it
 * recommend MORE widening, which is how a check gets widened onto a width that
 * does not draw the element and accuses a correct page.
 *
 * So: a root that cannot be read is an error, not a zero, and the census is
 * returned beside the answer so a reader can see what the number was counted
 * from.
 */
// Reading 623 files and 5.3 MB on every call made the tests slow enough to
// matter, and this lane must not add CI time. Cached per root; the census is
// still reported from what was read, so caching hides nothing.
const sourceCache = new Map();

function listSources(root) {
  if (sourceCache.has(root)) return sourceCache.get(root);
  const out = [];
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      if (name === "node_modules" || name === ".next" || name.startsWith(".")) continue;
      const full = path.join(dir, name);
      const st = statSync(full);
      if (st.isDirectory()) walk(full);
      else if (/\.(tsx|ts)$/.test(name) && !/\.test\./.test(name)) out.push({ file: full, text: readFileSync(full, "utf8") });
    }
  };
  for (const sub of SOURCE_ROOTS) {
    const dir = path.join(root, sub);
    try {
      walk(dir);
    } catch (error) {
      throw new Error(`this audit claims to read ${sub}/ and could not: ${String(error?.message ?? error)}. A folder that contributes nothing silently would make every class read as "no source mentions this" and the audit would still print a fraction.`);
    }
  }
  if (!out.length) {
    throw new Error(`this audit read no component source at all under ${root}, so any fraction it printed would be counted from nothing`);
  }
  sourceCache.set(root, out);
  return out;
}

/** Wording that says out loud the check is about one width. Never widen one of these. */
export const WIDTH_WORDS = /\b(on phones?|phone|mobile|narrow|laptop|desktop|columns?|side by side|fits the screen|horizontal page scroll|stack)\b/i;

/**
 * Should this pinned entry be widened to the other width?
 *
 * Returns exactly one verdict, each with a sentence naming THIS entry's reason.
 * Only `widenable` permits a change; every other verdict keeps the pin.
 */
export function classifyPin(entry, { css, root, cache }) {
  const pinned = (entry.viewports ?? [])[0];
  const target = pinned === "laptop" ? "mobile" : "laptop";
  const width = WIDTHS[target];
  const selectors = selectorsOf(entry);

  if (!selectors.length) {
    return { verdict: "unknown", target, why: `"${entry.title}" asserts nothing this audit can resolve to a class, so whether it holds at the ${target} width cannot be derived from the repo` };
  }
  if (WIDTH_WORDS.test(entry.title)) {
    return { verdict: "by-design", target, why: `"${entry.title}" says in its own title that it is about one width, so it is pinned on purpose and must not be widened` };
  }
  for (const selector of selectors) {
    for (const className of classesIn(selector)) {
      const hit = hiddenAtWidth(css, className, width);
      if (hit.hidden) {
        return { verdict: "hidden-by-css", target, why: `"${entry.title}" needs .${className}, which the stylesheet hides below ${hit.bound}px, so it is genuinely not on the page at the ${target} width` };
      }
      const sources = sourcesFor(root, className, cache);
      const branching = sources.find((s) => s.viewportJs);
      if (branching) {
        return { verdict: "viewport-js", target, why: `"${entry.title}" needs .${className}, drawn by ${path.basename(branching.file)}, which decides in JavaScript what to render at each width — so whether it appears at the ${target} width cannot be settled without opening the page` };
      }
      if (!sources.length) {
        return { verdict: "unknown", target, why: `"${entry.title}" needs .${className}, which no admin-web source file mentions, so this audit cannot say whether the ${target} width draws it` };
      }
    }
  }
  return { verdict: "widenable", target, why: `"${entry.title}" depends only on classes the stylesheet never hides at the ${target} width and that no width-branching component draws, so the same check holds there` };
}

/** Audit every pinned entry. Coverage as a fraction; a reason on every entry that stays pinned. */
export function auditPins(entries, { css, root }) {
  if (typeof css !== "string" || css.trim().length === 0) {
    throw new Error("this audit claims to read the stylesheet and got nothing. With no CSS nothing looks hidden, so it would report MORE checks as safe to widen than are — the one direction that ends in a correct page being accused.");
  }
  const cache = new Map();
  const pinned = entries.filter((e) => (e.viewports ?? []).length === 1);
  const rows = pinned.map((entry) => ({ sha: entry.sha, route: entry.route, ...classifyPin(entry, { css, root, cache }) }));
  const widenable = rows.filter((r) => r.verdict === "widenable");
  const files = cache.get("__files__") ?? [];
  return {
    // What the fraction was counted FROM, printed beside it.
    read: {
      sourceFiles: files.length,
      sourceCharacters: files.reduce((n, f) => n + f.text.length, 0),
      stylesheetCharacters: css.length,
      mediaBlocks: mediaBlocks(css).length,
    },
    entriesPinned: pinned.length,
    widenable: widenable.length,
    stillPinned: rows.filter((r) => r.verdict !== "widenable"),
    fraction: `${widenable.length}/${pinned.length} pinned checks provably hold at the other width`,
    rows,
  };
}
