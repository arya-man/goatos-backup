// Regression-pattern checks for the live browser smoke.
// Each check maps to a bug family the team fixed repeatedly since Aug 2026
// (see the pattern letters: A charts, B containment, C table cells, chips, J raw text, D page overflow)
// plus the general text-over-text overlap check that used to live in assertReadableText.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { collectPenLabelCandidates, penLabelFindings } from "./pen-label-checks.mjs";
import { cellOverpaintDetail } from "./visible-break-rules.mjs";

export const REGRESSION_PATTERNS = Object.freeze({
  "text-overlap": "text drawn over other unrelated text",
  "text-cut-off": "text clipped by its box with no ellipsis/line-clamp",
  "A-chart-label-collapsed": "chart label rendered shorter than its font (collapsed to ~0px)",
  "A-chart-label-clipped": "chart label clipped or outside its card/chart",
  "A-chart-label-overlap": "chart labels in the same chart collide",
  "A-chart-label-column-narrow": ".wbl label column narrower than 80px",
  "A-chart-labels-truncated": "more than half the labels in one chart are ellipsised",
  "A-chart-value-missing": "bar rendered with an empty value label",
  "A-chart-empty-frame": "chart with no visible bars and no empty-state text",
  "A-svg-text-clipped": "SVG chart text outside the SVG box",
  "A-svg-text-overlap": "SVG chart texts collide",
  "A-svg-text-tiny": "SVG chart text scaled below the readable minimum",
  "B-container-overflow": "content wider than its card/KPI/dialog/drawer/modal",
  "C-cell-mid-word-wrap": "table cell text broken mid-word across lines",
  "C-cell-overpaint": "table cell text painting over the next column",
  "chip-crushed": "chip/badge text wrapped mid-word or clipped",
  "J-raw-text": "raw value/code/copy key/ISO date/doubled label leaked into the UI",
  "D-page-overflow": "element makes the page scroll horizontally",
  // P: pen / partition labels. See lib/pen-label-checks.mjs for the contract and the
  // bug history (475312f5d, e2a70f3db, 1a256a225) each of these encodes.
  "P-pen-part-doubled": 'pen label repeats its worded partition ("Godel 1 - Part 1 - Part 1")',
  "P-pen-number-doubled": 'pen label repeats its partition numeral ("Castro 1 1")',
  "P-pen-separator-wrong": 'shed and partition joined the wrong way for the convention ("Godel 1 Part 3", "Castro - 2")',
  "P-pen-whole-leaked": 'the non-partition sentinel "whole" rendered inside a pen label',
  "P-pen-partition-missing": "pen rendered bare while siblings in the same column show partitions for that shed",
});

// The farm's real (shed, partition_label) pairs, refreshed by
// scripts/refresh-pen-label-vocabulary.mjs. Read once per process.
let penVocabularyCache;
export function penLabelVocabulary() {
  if (penVocabularyCache === undefined) {
    try {
      penVocabularyCache = JSON.parse(readFileSync(new URL("./pen-label-vocabulary.json", import.meta.url), "utf8"));
    } catch {
      penVocabularyCache = null;
    }
  }
  return penVocabularyCache;
}

// Runs inside the browser (serialised by page.evaluate). Must stay self-contained.
export function collectRegressionFindings({ mobile = false, limit = 40 } = {}) {
  const found = [];
  const flagged = new Set();
  const txt = (el) => (el.textContent ?? "").replace(/\s+/g, " ").trim();
  const describe = (el) => {
    const cls = typeof el.className === "string" && el.className.trim() ? "." + el.className.trim().split(/\s+/).slice(0, 2).join(".") : "";
    return `${el.tagName.toLowerCase()}${cls} "${txt(el).slice(0, 40)}"`;
  };
  // What a person READS off the element. textContent runs the child boxes together, so a bar
  // label came out as "2026-08-10331 animals" — a string that appears nowhere on screen and
  // reads as a corrupt number. Join the text boxes with a space and collapse the runs.
  const readable = (el) => {
    if (!el) return "";
    const parts = [];
    const walk = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    for (let n = walk.nextNode(); n; n = walk.nextNode()) {
      const t = (n.textContent ?? "").replace(/\s+/g, " ").trim();
      if (t) parts.push(t);
    }
    return parts.join(" ").replace(/\s+/g, " ").trim();
  };
  const add = (pattern, el, detail, peerEl) => {
    if (found.length >= limit) return;
    const key = pattern + "|" + (el ? describe(el) : "");
    if (flagged.has(key)) return;
    flagged.add(key);
    // The headline of the thrown error is findings[0]. Mark its element so the screenshot can
    // centre on THAT one: the evidence used to scroll to the first [data-smoke-issue] in DOM
    // order, which is a different element whenever the checks do not run in document order —
    // so the sentence named a chart label while the red box sat on a table three cards away.
    if (el && found.length === 0) el.setAttribute("data-smoke-issue-first", "1");
    if (el) el.setAttribute("data-smoke-issue", pattern);
    found.push({ pattern, element: el ? describe(el) : "", detail, text: readable(el).slice(0, 60), peer: peerEl ? readable(peerEl).slice(0, 60) : "" });
  };
  const inter = (a, b) => Math.max(0, Math.min(a.right, b.right) - Math.max(a.left, b.left)) * Math.max(0, Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top));
  const overlapPx = (a, b) => Math.min(Math.min(a.right, b.right) - Math.max(a.left, b.left), Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top));

  const hiddenCache = new Map();
  const paintedHiddenCache = new Map();
  const srOnly = (el, s) =>
    el.matches(".sr-only, .visually-hidden, .visuallyhidden") ||
    (/rect\(0/.test(s.clip) && s.position === "absolute") ||
    /inset\(50%|inset\(100%/.test(s.clipPath) ||
    (s.overflow === "hidden" && el.getBoundingClientRect().width <= 1 && el.getBoundingClientRect().height <= 1);
  // Hidden = not painted for a human: excluded, aria-hidden, sr-only, closed (display/visibility/opacity) anywhere up the tree.
  const hidden = (el) => {
    if (!el || el === document.documentElement) return false;
    if (hiddenCache.has(el)) return hiddenCache.get(el);
    let result = false;
    if (el.hasAttribute("data-smoke-ignore") || el.getAttribute("aria-hidden") === "true" || el.hasAttribute("hidden") || el.hasAttribute("inert")) result = true;
    else {
      const s = getComputedStyle(el);
      if (s.display === "none" || s.visibility === "hidden" || s.visibility === "collapse" || Number(s.opacity) === 0 || srOnly(el, s)) result = true;
      else if (el.tagName === "DETAILS" || el.parentElement?.tagName === "DETAILS") {
        const d = el.tagName === "DETAILS" ? null : el.parentElement;
        if (d && !d.open && el.tagName !== "SUMMARY") result = true;
      }
      if (!result) result = hidden(el.parentElement);
    }
    hiddenCache.set(el, result);
    return result;
  };
  // Hidden TO THE EYE. Same rules as hidden(), minus aria-hidden.
  //
  // aria-hidden="true" removes a node from the ACCESSIBILITY TREE; it says nothing about paint.
  // Chart components mark their drawing aria-hidden on purpose — the figures are already carried
  // by the wrapper's role="img"/aria-label — so every bar in an inline-SVG chart is aria-hidden
  // and fully painted. Judging "does this chart look empty" by hidden() therefore called every
  // one of those charts an empty frame while the bars were plainly on screen. Anything asking
  // "what does a person SEE" must use this; anything asking "what does a screen reader get"
  // keeps using hidden().
  const paintedHidden = (el) => {
    if (!el || el === document.documentElement) return false;
    if (paintedHiddenCache.has(el)) return paintedHiddenCache.get(el);
    let result = false;
    if (el.hasAttribute("data-smoke-ignore") || el.hasAttribute("hidden") || el.hasAttribute("inert")) result = true;
    else {
      const s = getComputedStyle(el);
      if (s.display === "none" || s.visibility === "hidden" || s.visibility === "collapse" || Number(s.opacity) === 0 || srOnly(el, s)) result = true;
      else if (el.tagName === "DETAILS" || el.parentElement?.tagName === "DETAILS") {
        const d = el.tagName === "DETAILS" ? null : el.parentElement;
        if (d && !d.open && el.tagName !== "SUMMARY") result = true;
      }
      if (!result) result = paintedHidden(el.parentElement);
    }
    paintedHiddenCache.set(el, result);
    return result;
  };
  // Painted = has a box, not hidden, and (when its centre is on screen) actually hit by elementFromPoint.
  // This drops closed popovers/date pickers that are still in the DOM behind other content.
  const painted = (el) => {
    if (hidden(el)) return false;
    const r = el.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) return false;
    const cx = Math.min(Math.max(r.left + r.width / 2, r.left + 1), r.right - 1);
    const cy = Math.min(Math.max(r.top + r.height / 2, r.top + 1), r.bottom - 1);
    if (cx < 0 || cy < 0 || cx >= innerWidth || cy >= innerHeight) return true; // off-screen: cannot probe, trust styles
    const hit = document.elementFromPoint(cx, cy);
    return !!hit && (hit === el || el.contains(hit) || hit.contains(el) || hit.closest("[data-smoke-issue]") === el);
  };
  const clipsX = (el) => /auto|scroll|hidden|clip/.test(getComputedStyle(el).overflowX);
  const scrollerBetween = (el, stop) => {
    for (let p = el.parentElement; p && p !== stop; p = p.parentElement) if (clipsX(p)) return true;
    return false;
  };
  const lineCount = (el) => {
    const range = document.createRange();
    range.selectNodeContents(el);
    const tops = new Set();
    for (const r of range.getClientRects()) if (r.width > 0.5) tops.add(Math.round(r.top));
    return tops.size;
  };
  // A single word that the browser split across lines WITH NOWHERE LEGAL TO BREAK:
  // "Warmu/p", "C/B/E", a hash on 2 lines.
  //
  // The run deliberately excludes hyphens, dashes and slashes. Those are break opportunities in
  // normal typography, so "Shed-average plan" wrapping to "Shed-" / "average" is ordinary
  // wrapping that reads perfectly, not the crushed-cell bug this check is for — and counting it
  // flagged every hyphenated string in every narrow column. Scanning the runs BETWEEN those
  // characters keeps "Warmup" a single token, so the real break-all bug still reports.
  const brokenToken = (el) => {
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const text = node.textContent;
      const re = /[^\s\-‐-―−/⁄]{2,}/g;
      for (let m = re.exec(text); m; m = re.exec(text)) {
        const range = document.createRange();
        range.setStart(node, m.index);
        range.setEnd(node, m.index + m[0].length);
        const tops = new Set();
        for (const r of range.getClientRects()) if (r.width > 0.5) tops.add(Math.round(r.top));
        if (tops.size >= 2) return { token: m[0], lines: tops.size };
      }
    }
    return null;
  };
  const clipsAny = (p) => { const s = getComputedStyle(p); return s.overflowX !== "visible" || s.overflowY !== "visible"; };
  const truncatedWithEllipsis = (el) => {
    const s = getComputedStyle(el);
    return s.textOverflow === "ellipsis" && el.scrollWidth > el.clientWidth + 1;
  };
  const clippedNoEllipsis = (el) => {
    const s = getComputedStyle(el);
    const x = /hidden|clip/.test(s.overflowX) && s.textOverflow !== "ellipsis" && el.scrollWidth > el.clientWidth + 1;
    const y = /hidden|clip/.test(s.overflowY) && (s.webkitLineClamp === "none" || !s.webkitLineClamp) && el.scrollHeight > el.clientHeight + 1;
    return x ? `${el.scrollWidth - el.clientWidth}px wide text hidden` : y ? `${el.scrollHeight - el.clientHeight}px tall text hidden` : "";
  };
  const root = document.querySelector("main") ?? document.body;

  // ---------- A: HTML chart labels ----------
  const CHART = ".gcols, .mcols, .hbarlist, .wbars, .wgrouped, .wchart";
  const LABELS = ".gcval, .gclab, .gcsub, .mclab, .mcv, .mcsub, .wbl, .wbl-text, .wbv, .hblab, .hbval";
  const byChart = new Map();
  for (const el of root.querySelectorAll(LABELS)) {
    if (!txt(el) || hidden(el)) continue;
    const lr = el.getBoundingClientRect();
    // A collapsed label has ~0 height, so it cannot pass painted(); keep it when it still has width.
    if (lr.height > 1 && !painted(el)) continue;
    if (lr.width <= 0) continue;
    const chart = el.closest(CHART) ?? el.parentElement;
    if (!byChart.has(chart)) byChart.set(chart, []);
    byChart.get(chart).push(el);
  }
  for (const [chart, labels] of byChart) {
    const box = (chart.closest(".card, .kpi, [role=dialog], .drawer, .modal") ?? chart).getBoundingClientRect();
    let ellipsised = 0;
    for (const el of labels) {
      const r = el.getBoundingClientRect();
      const fs = parseFloat(getComputedStyle(el).fontSize) || 12;
      if (r.height < 0.8 * fs) add("A-chart-label-collapsed", el, `height ${Math.round(r.height)}px for ${Math.round(fs)}px text`);
      const cut = clippedNoEllipsis(el);
      if (cut) add("A-chart-label-clipped", el, cut);
      const boxEl = chart.closest(".card, .kpi, [role=dialog], .drawer, .modal") ?? chart;
      let inScroller = false;
      for (let q = el.parentElement; q && q !== boxEl; q = q.parentElement) if (clipsAny(q)) { inScroller = true; break; }
      const out = inScroller ? 0 : Math.max(box.left - r.left, r.right - box.right, box.top - r.top, r.bottom - box.bottom);
      if (out > 1) add("A-chart-label-clipped", el, `${Math.round(out)}px outside its card`);
      if (el.matches(".wbl") && r.width < 80) add("A-chart-label-column-narrow", el, `label column ${Math.round(r.width)}px (<80px)`);
      if (truncatedWithEllipsis(el)) ellipsised += 1;
    }
    if (labels.length >= 2 && ellipsised / labels.length > 0.5) add("A-chart-labels-truncated", chart, `${ellipsised}/${labels.length} labels ellipsised`);
    // Only leaf-level labels collide meaningfully (.wbl contains .wbl-text).
    const leaves = labels.filter((el) => !labels.some((o) => o !== el && el.contains(o)));
    for (let i = 0; i < leaves.length; i += 1) {
      for (let j = i + 1; j < leaves.length; j += 1) {
        const o = overlapPx(leaves[i].getBoundingClientRect(), leaves[j].getBoundingClientRect());
        if (o > 2) { add("A-chart-label-overlap", leaves[i], `collides with "${txt(leaves[j]).slice(0, 30)}" by ${Math.round(o)}px`, leaves[j]); break; }
      }
    }
  }
  for (const gcb of root.querySelectorAll(".gcb:not(.gcempty)")) {
    const bar = gcb.querySelector(".gcbar");
    if (!bar || !painted(gcb)) continue;
    const val = gcb.querySelector(".gcval");
    if (!val || !txt(val)) add("A-chart-value-missing", gcb, "bar has no value label");
  }
  for (const chart of root.querySelectorAll(CHART)) {
    if (!painted(chart)) continue;
    // paintedHidden, not hidden: an inline-SVG chart marks its drawing aria-hidden by design.
    const bars = Array.from(chart.querySelectorAll(".gcbar, .mcbar, .hbfill, .wbar, rect, path, circle, polyline, line")).filter((b) => !paintedHidden(b) && b.getBoundingClientRect().height > 0.5 && b.getBoundingClientRect().width > 0.5);
    if (bars.length === 0 && !/no data|no rows|nothing|no records|empty|0 /i.test(txt(chart.closest(".card") ?? chart))) add("A-chart-empty-frame", chart, "no visible bars and no empty-state text");
  }

  // ---------- A: SVG charts ----------
  const minFont = 8;
  for (const svg of root.querySelectorAll("svg[role=img]")) {
    if (!painted(svg) && hidden(svg)) continue;
    const sb = svg.getBoundingClientRect();
    if (sb.width < 80) continue;
    const texts = Array.from(svg.querySelectorAll("text")).filter((t) => txt(t) && !hidden(t)).map((t) => ({ t, r: t.getBoundingClientRect() })).filter(({ r }) => r.width > 0 && r.height > 0);
    for (const { t, r } of texts) {
      const out = Math.max(sb.left - r.left, r.right - sb.right, sb.top - r.top, r.bottom - sb.bottom);
      if (out > 1) add("A-svg-text-clipped", t, `${Math.round(out)}px outside the chart`);
      // Effective size = rendered glyph box height (~1.2 x font size), so compare against font height.
      const eff = r.height / 1.2;
      if (eff < minFont) add("A-svg-text-tiny", t, `renders at ~${eff.toFixed(1)}px (<${minFont}px)`);
    }
    for (let i = 0; i < texts.length; i += 1) {
      for (let j = i + 1; j < texts.length; j += 1) {
        const o = overlapPx(texts[i].r, texts[j].r);
        if (o > 2) { add("A-svg-text-overlap", texts[i].t, `collides with "${txt(texts[j].t).slice(0, 30)}" by ${Math.round(o)}px`, texts[j].t); break; }
      }
    }
  }

  // ---------- B: containment ----------
  for (const box of document.querySelectorAll(".card, .kpi, [role=dialog], .drawer, .modal")) {
    if (!painted(box) && hidden(box)) continue;
    const br = box.getBoundingClientRect();
    if (br.width <= 0) continue;
    let n = 0;
    for (const el of box.querySelectorAll("*")) {
      if (++n > 2500) break;
      const r = el.getBoundingClientRect();
      if (r.right <= br.right + 1 || r.width <= 0 || el.closest("svg") !== null && el.tagName !== "svg") continue;
      if (hidden(el) || scrollerBetween(el, box)) continue;
      // Report the outermost overflowing element only.
      const parent = el.parentElement;
      if (parent && parent !== box && parent.getBoundingClientRect().right > br.right + 1) continue;
      add("B-container-overflow", el, `${Math.round(r.right - br.right)}px past ${box.className && typeof box.className === "string" ? "." + box.className.trim().split(/\s+/)[0] : box.getAttribute("role") ?? "container"}`);
      break;
    }
  }

  // ---------- C: table cells ----------
  for (const td of root.querySelectorAll("table td")) {
    if (hidden(td)) continue;
    for (const el of [td, ...td.querySelectorAll(".celllink")]) {
      const t = txt(el);
      if (!t || el.querySelector("td, table")) continue;
      const broken = brokenToken(el);
      if (broken) { add("C-cell-mid-word-wrap", el, `"${broken.token.slice(0, 24)}" split over ${broken.lines} lines`); break; }
    }
    // C-cell-overpaint is judged in Node, not here: see collectCellOverpaintIssues below.
    // "wider than the cell" is not a break — "painted over the next column's words" is,
    // and telling them apart needs a rule that can be unit-tested.
  }

  // ---------- chips / badges ----------
  for (const chip of root.querySelectorAll(".tag, .chip, .pill, .ec, .badge, [class*=chip], [class*=tag]")) {
    const t = txt(chip);
    if (!t || !painted(chip) || chip.querySelector(".tag, .chip, .pill, .badge, [class*=chip], button, a") || chip.children.length > 2) continue;
    const s = getComputedStyle(chip);
    if (!/inline|flex/.test(s.display)) continue;
    const cut = clippedNoEllipsis(chip);
    if (cut) { add("chip-crushed", chip, `"${t.slice(0, 24)}" clipped (${cut})`); continue; }
    if (truncatedWithEllipsis(chip) && t.length <= 12) { add("chip-crushed", chip, `short chip "${t}" ellipsised to ${chip.clientWidth}px`); continue; }
    const broken = brokenToken(chip);
    if (broken) add("chip-crushed", chip, `"${broken.token.slice(0, 24)}" split over ${broken.lines} lines`);
  }

  // ---------- J: raw text ----------
  {
    const RAW = [
      [/\bNaN\b|\bundefined\b|\bnull\b|Invalid Date|\[object/, "raw value"],
      [/\b[a-z]+_[a-z_]+\b/, "snake_case code"],
      [/\b[a-z_]+\.[a-z_]+\.[a-z_]+\b/, "copy key"],
    ];
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    let checked = 0;
    for (let node = walker.nextNode(); node && checked < 6000; node = walker.nextNode()) {
      const el = node.parentElement;
      const t = (node.textContent ?? "").trim();
      if (!el || !t || el.closest("code, pre, input, textarea, script, style, noscript, svg title") || hidden(el)) continue;
      checked += 1;
      for (const [re, kind] of RAW) {
        const m = t.match(re);
        if (m && !/@|https?:|\.(com|sg|in|png|jpg|pdf|csv)\b/.test(m[0])) { add("J-raw-text", el, `${kind} "${m[0]}"`); break; }
      }
      if (el.closest("td") && /\b\d{4}-\d{2}-\d{2}\b/.test(t)) add("J-raw-text", el, `ISO date "${t.match(/\d{4}-\d{2}-\d{2}/)[0]}" in table (farm reads DD-MM-YYYY)`);
    }
    for (const el of root.querySelectorAll("td, th, span, p, h1, h2, h3, h4, strong, small, a, button, li")) {
      if (el.children.length > 3 || hidden(el)) continue;
      const t = txt(el);
      // "A – A" with a dash is a same-day range, not a doubled label.
      const m = t.match(/^(.{2,40}?)\s*[·|•]\s*\1$/);
      if (m) add("J-raw-text", el, `doubled label "${t.slice(0, 40)}"`);
    }
  }

  // ---------- D: page horizontal overflow ----------
  if (document.documentElement.scrollWidth > innerWidth + 1) {
    let culprit = null;
    let n = 0;
    for (const el of document.body.querySelectorAll("*")) {
      if (++n > 8000) break;
      const r = el.getBoundingClientRect();
      if (r.right <= innerWidth + 1 || r.width <= 0 || hidden(el)) continue;
      let scrolled = false;
      for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) if (clipsX(p)) { scrolled = true; break; }
      if (scrolled) continue;
      // Report leaf culprits (no child that itself overflows).
      if (Array.from(el.children).some((c) => c.getBoundingClientRect().right > innerWidth + 1)) continue;
      culprit = el;
      add("D-page-overflow", el, `${Math.round(r.right - innerWidth)}px past the ${innerWidth}px viewport (page scrolls sideways)`);
      if (found.filter((f) => f.pattern === "D-page-overflow").length >= 3) break;
    }
    if (!culprit) add("D-page-overflow", document.documentElement, `page is ${document.documentElement.scrollWidth - innerWidth}px wider than the ${innerWidth}px viewport`);
  }

  // ---------- general text-over-text overlap + cut-off (fixed false positives) ----------
  {
    const boxes = [];
    // A Range reports where the text WOULD run, ignoring any ancestor that clips it. An
    // ellipsised label ("Non-elevated pen" in a 112px column) therefore reports its full
    // untruncated width, which reaches under the icon sitting after the ellipsis — and the
    // check called that "text drawn over other unrelated text" while the screenshot showed a
    // clear gap. Intersect every rect with the boxes that actually clip it, so the check
    // compares what is PAINTED. Genuinely overlapping text is unaffected: clipping only ever
    // shrinks a rect to its visible part, and two visible texts still intersect.
    const clipToAncestors = (el, rect) => {
      let out = { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom };
      for (let p = el; p && p !== document.documentElement; p = p.parentElement) {
        const s = getComputedStyle(p);
        if (!/hidden|clip|auto|scroll/.test(s.overflowX) && !/hidden|clip|auto|scroll/.test(s.overflowY)) continue;
        const pr = p.getBoundingClientRect();
        if (/hidden|clip|auto|scroll/.test(s.overflowX)) { out.left = Math.max(out.left, pr.left); out.right = Math.min(out.right, pr.right); }
        if (/hidden|clip|auto|scroll/.test(s.overflowY)) { out.top = Math.max(out.top, pr.top); out.bottom = Math.min(out.bottom, pr.bottom); }
      }
      return { left: out.left, right: out.right, top: out.top, bottom: out.bottom, width: out.right - out.left, height: out.bottom - out.top };
    };
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node && boxes.length < 1500; node = walker.nextNode()) {
      const el = node.parentElement;
      if (!el || !node.textContent.trim() || el.closest("svg") || hidden(el)) continue;
      const range = document.createRange();
      range.selectNodeContents(node);
      for (const raw of range.getClientRects()) {
        const rect = clipToAncestors(el, raw);
        if (rect.width < 2 || rect.height < 2) continue;
        const cx = rect.left + rect.width / 2, cy = rect.top + rect.height / 2;
        if (cx < 0 || cy < 0 || cx >= innerWidth || cy >= innerHeight) continue; // only what elementFromPoint can confirm
        const hit = document.elementFromPoint(cx, cy);
        if (hit && (hit === el || el.contains(hit) || hit.contains(el))) boxes.push({ el, rect });
      }
    }
    outer: for (let i = 0; i < boxes.length; i += 1) {
      for (let j = i + 1; j < boxes.length; j += 1) {
        const a = boxes[i], b = boxes[j];
        if (a.el === b.el || a.el.contains(b.el) || b.el.contains(a.el)) continue;
        const o = inter(a.rect, b.rect);
        const smaller = Math.min(a.rect.width * a.rect.height, b.rect.width * b.rect.height);
        if (o > 12 && o / smaller > 0.25) {
          add("text-overlap", a.el, `overlaps ${describe(b.el)}`, b.el);
          b.el.setAttribute("data-smoke-issue", "text-overlap");
          if (found.length >= limit) break outer;
          break;
        }
      }
    }
    for (const el of root.querySelectorAll("td, th, span, p, div, label, h1, h2, h3, h4, strong, small, b")) {
      if (el.children.length > 0 || !txt(el) || el.closest("[data-truncate], svg") || el.matches(LABELS) || !painted(el)) continue;
      const cut = clippedNoEllipsis(el);
      if (cut) add("text-cut-off", el, cut);
    }
  }
  return found;
}

// Table cells whose text runs past their column. The page reports geometry only:
// where the cell is, how far its words run on, and what the columns beside it
// actually paint. cellOverpaintDetail decides whether any of that is covered.
export function collectCellOverpaintCandidates() {
  const paintedTextRects = (el) => {
    const rects = [];
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node && rects.length < 12; node = walker.nextNode()) {
      const text = (node.textContent ?? "").trim();
      if (!text) continue;
      const range = document.createRange();
      range.selectNodeContents(node);
      for (const r of range.getClientRects()) {
        if (r.width > 0.5 && r.height > 0.5) {
          rects.push({ rect: { left: r.left, right: r.right, top: r.top, bottom: r.bottom }, text });
        }
      }
    }
    return rects;
  };
  const out = [];
  let index = 0;
  for (const td of document.querySelectorAll("table td")) {
    if (out.length >= 40) break;
    const style = getComputedStyle(td);
    if (style.overflowX !== "visible") continue;
    if (style.visibility === "hidden" || style.display === "none") continue;
    const overflowPx = td.scrollWidth - td.clientWidth;
    if (overflowPx <= 1) continue;
    if (td.querySelector("[style*=absolute], .dot")) continue;
    const r = td.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) continue;
    const neighbourTexts = [];
    for (let sib = td.nextElementSibling; sib; sib = sib.nextElementSibling) {
      const sr = sib.getBoundingClientRect();
      if (sr.left >= r.right + overflowPx) break;
      for (const hit of paintedTextRects(sib)) neighbourTexts.push(hit);
      if (neighbourTexts.length >= 24) break;
    }
    td.setAttribute("data-overpaint-candidate", String(index));
    out.push({
      index,
      overflowPx,
      rect: { left: r.left, right: r.right, top: r.top, bottom: r.bottom },
      text: (td.textContent ?? "").trim().replace(/\s+/g, " ").slice(0, 40),
      neighbourTexts,
    });
    index += 1;
  }
  return out;
}

async function collectCellOverpaintIssues(page) {
  const candidates = await page.evaluate(collectCellOverpaintCandidates);
  const issues = [];
  for (const candidate of candidates) {
    const detail = cellOverpaintDetail(candidate);
    if (detail) issues.push({ index: candidate.index, detail, text: candidate.text });
  }
  await page.evaluate((marks) => {
    for (const el of document.querySelectorAll("[data-overpaint-candidate]")) {
      const i = Number(el.getAttribute("data-overpaint-candidate"));
      if (marks.includes(i)) el.setAttribute("data-smoke-issue", "C-cell-overpaint");
      el.removeAttribute("data-overpaint-candidate");
    }
  }, issues.map((issue) => issue.index));
  return issues.map((issue) => ({
    pattern: "C-cell-overpaint",
    element: `table cell "${issue.text}"`,
    detail: issue.detail,
  }));
}

// Pen labels are judged in Node, not in the page: the vocabulary is the farm's own data
// and the rules are worth unit-testing directly (see pen-label-checks.test.mjs). The page
// only reports what it renders and where, then gets told which candidates to outline.
async function collectPenLabelIssues(page) {
  const manifest = penLabelVocabulary();
  if (!manifest || !(manifest.sheds ?? []).length) return [];
  const candidates = await page.evaluate(collectPenLabelCandidates);
  const issues = penLabelFindings(candidates, manifest);
  const marks = issues.map((issue) => ({ index: issue.index, pattern: issue.pattern }));
  await page.evaluate((list) => {
    for (const el of document.querySelectorAll("[data-pen-candidate]")) {
      const i = Number(el.getAttribute("data-pen-candidate"));
      const mark = list.find((m) => m.index === i);
      if (mark) el.setAttribute("data-smoke-issue", mark.pattern);
      el.removeAttribute("data-pen-candidate");
    }
  }, marks);
  return issues.map((issue) => ({ pattern: issue.pattern, element: `pen label "${issue.text}"`, detail: issue.detail }));
}

// One plain sentence per bug family, for the humans who read the sweep.
//
// The rule: name what a person SEES. No selectors, no tag names, no property names, no pattern
// codes, no pixel counts — those live on the machine-readable `finding_*` log lines and in the
// red-boxed screenshot, and a farm manager cannot act on `span.wbl ... 18px (<80px)`.
export function findingSentence(f) {
  const q = (s) => (s && s.trim() ? `"${s.trim()}"` : "an item");
  const t = q(f.text);
  const p = q(f.peer);
  switch (f.pattern) {
    case "text-overlap": return `${t} is printed on top of ${p}`;
    case "text-cut-off": return `${t} is cut off, with no "..." to show there is more`;
    case "A-chart-label-collapsed": return `the chart label ${t} has been squashed until there is no room to read it`;
    case "A-chart-label-clipped": return `the chart label ${t} is cut off at the edge of its card`;
    case "A-chart-label-overlap": return `the chart labels ${t} and ${p} are printed on top of each other`;
    case "A-chart-label-column-narrow": return `the labels down the side of the chart are squeezed so narrow that ${t} is cut to a couple of characters`;
    case "A-chart-labels-truncated": return `most of the labels in this chart are cut short`;
    case "A-chart-value-missing": return `a bar in this chart has no number beside it`;
    case "A-chart-empty-frame": return `the chart ${t} is an empty frame: nothing is drawn and nothing says why`;
    case "A-svg-text-clipped": return `the chart text ${t} runs outside the chart`;
    case "A-svg-text-overlap": return `the chart labels ${t} and ${p} collide`;
    case "A-svg-text-tiny": return `the chart text ${t} is far too small to read`;
    case "B-container-overflow": return `${t} runs outside the card it belongs to`;
    case "C-cell-mid-word-wrap": return `a word in the table cell ${t} is broken across two lines`;
    case "C-cell-overpaint": return `the table cell ${t} spills over the next column`;
    case "chip-crushed": return `the label ${t} is crushed out of shape`;
    case "J-raw-text":
      if (/ISO date/.test(f.detail ?? "")) return `a date in the table is written year-first, where the farm reads the day first`;
      if (/doubled label/.test(f.detail ?? "")) return `the label ${t} is printed twice over`;
      return `${t} shows an internal code instead of wording a person would use`;
    case "D-page-overflow": return `${t} is wider than the screen, so the page scrolls sideways`;
    // Pen / partition labels (see lib/pen-label-checks.mjs). These findings arrive with the
    // pen's own text, so the sentence can quote the name the farm would read.
    case "P-pen-part-doubled": return `the pen ${t} is shown with its part number twice`;
    case "P-pen-number-doubled": return `the pen ${t} is shown with its number twice`;
    case "P-pen-partition-missing": return `the pen ${t} is shown without its part number, while its neighbours show theirs`;
    case "P-pen-separator-wrong": return `the pen ${t} has its shed and part joined in the wrong style`;
    case "P-pen-whole-leaked": return `the pen ${t} shows the word whole instead of the shed name`;
    default: return `${t} does not look right`;
  }
}

const VIEWPORT_WORD = (label) => (/mobile|phone/i.test(label) ? "on the phone" : "on the laptop");

export async function assertRegressionPatterns(page, { routeName, viewportLabel, screenshotDir, relativeToRepo = (p) => p }) {
  const width = page.viewportSize()?.width ?? 1280;
  let findings = await page.evaluate(collectRegressionFindings, { mobile: width < 768 || /mobile|phone/i.test(viewportLabel) });
  // Audit log / dead-letter queue list event codes by design; they are internal ops tooling.
  if (/^operations-(audit|dlq)/.test(routeName)) findings = findings.filter((f) => f.pattern !== "J-raw-text");
  findings = findings.concat(await collectCellOverpaintIssues(page));
  findings = findings.concat(await collectPenLabelIssues(page));
  if (findings.length === 0) return [];
  await page.addStyleTag({ content: "[data-smoke-issue]{outline:3px solid #e11d48 !important;outline-offset:1px}" });
  // Centre the screenshot on the element the headline sentence is about, not on whichever
  // flagged element happens to come first in the document.
  await page.evaluate(() => (document.querySelector("[data-smoke-issue-first]") ?? document.querySelector("[data-smoke-issue]"))?.scrollIntoView({ block: "center", inline: "center" }));
  const issuesPath = join(screenshotDir, `${viewportLabel}-${routeName}-issues.png`);
  await page.screenshot({ path: issuesPath, fullPage: false });
  console.log(`screenshot_path=${relativeToRepo(issuesPath)}`);
  // The page's own heading is what a reader recognises; the route id is for the machine log.
  const pageName = await page
    .evaluate(() => {
      const h = document.querySelector("main h1, h1");
      return (h?.textContent ?? "").replace(/\s+/g, " ").trim().slice(0, 60);
    })
    .catch(() => "");
  for (const f of findings) console.log(`finding_pattern=${viewportLabel}:${routeName}|${f.pattern}|${f.element}|${f.detail}`);
  const summary = findings.slice(0, 3).map(findingSentence).join("; ");
  const more = findings.length > 3 ? ` (+${findings.length - 3} more)` : "";
  const where = pageName ? `${pageName} ${VIEWPORT_WORD(viewportLabel)}` : `${routeName} ${VIEWPORT_WORD(viewportLabel)}`;
  throw new Error(`${where}: ${summary}${more}`);
}
