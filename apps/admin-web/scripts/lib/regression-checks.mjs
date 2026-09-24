// Regression-pattern checks for the live browser smoke.
// Each check maps to a bug family the team fixed repeatedly since Aug 2026
// (see the pattern letters: A charts, B containment, C table cells, chips, J raw text, D page overflow)
// plus the general text-over-text overlap check that used to live in assertReadableText.
import { join } from "node:path";

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
});

// Runs inside the browser (serialised by page.evaluate). Must stay self-contained.
export function collectRegressionFindings({ mobile = false, limit = 40 } = {}) {
  const found = [];
  const flagged = new Set();
  const txt = (el) => (el.textContent ?? "").replace(/\s+/g, " ").trim();
  const describe = (el) => {
    const cls = typeof el.className === "string" && el.className.trim() ? "." + el.className.trim().split(/\s+/).slice(0, 2).join(".") : "";
    return `${el.tagName.toLowerCase()}${cls} "${txt(el).slice(0, 40)}"`;
  };
  const add = (pattern, el, detail) => {
    if (found.length >= limit) return;
    const key = pattern + "|" + (el ? describe(el) : "");
    if (flagged.has(key)) return;
    flagged.add(key);
    if (el) el.setAttribute("data-smoke-issue", pattern);
    found.push({ pattern, element: el ? describe(el) : "", detail });
  };
  const inter = (a, b) => Math.max(0, Math.min(a.right, b.right) - Math.max(a.left, b.left)) * Math.max(0, Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top));
  const overlapPx = (a, b) => Math.min(Math.min(a.right, b.right) - Math.max(a.left, b.left), Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top));

  const hiddenCache = new Map();
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
  // A single word (no spaces) that the browser split across lines: "Warmu/p", "C/B/E", a hash on 2 lines.
  const brokenToken = (el) => {
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const text = node.textContent;
      const re = /\S{2,}/g;
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
        if (o > 2) { add("A-chart-label-overlap", leaves[i], `collides with "${txt(leaves[j]).slice(0, 30)}" by ${Math.round(o)}px`); break; }
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
    const bars = Array.from(chart.querySelectorAll(".gcbar, .mcbar, .hbfill, .wbar, rect, path, circle, polyline, line")).filter((b) => !hidden(b) && b.getBoundingClientRect().height > 0.5 && b.getBoundingClientRect().width > 0.5);
    // A chart card may draw its figures as a GRID rather than bars (the Time-wise pen and load
    // week tables are .wchart cards): visible table rows are painted data, not an empty frame.
    // Without this the rule passed those grids only when some cell happened to contain "0 ", and
    // flagged the 30-day load grid as empty while it showed seven loads (2026-09-24).
    const tableRows = Array.from(chart.querySelectorAll("tbody tr")).filter((row) => !hidden(row) && row.getBoundingClientRect().height > 0.5);
    if (bars.length === 0 && tableRows.length === 0 && !/no data|no rows|nothing|no records|empty|0 /i.test(txt(chart.closest(".card") ?? chart))) add("A-chart-empty-frame", chart, "no visible bars and no empty-state text");
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
        if (o > 2) { add("A-svg-text-overlap", texts[i].t, `collides with "${txt(texts[j].t).slice(0, 30)}" by ${Math.round(o)}px`); break; }
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
    const s = getComputedStyle(td);
    if (s.overflowX === "visible" && td.scrollWidth > td.clientWidth + 1 && !td.querySelector("[style*=absolute], .dot")) add("C-cell-overpaint", td, `text ${td.scrollWidth - td.clientWidth}px wider than the cell`);
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
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node && boxes.length < 1500; node = walker.nextNode()) {
      const el = node.parentElement;
      if (!el || !node.textContent.trim() || el.closest("svg") || hidden(el)) continue;
      const range = document.createRange();
      range.selectNodeContents(node);
      for (const rect of range.getClientRects()) {
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
          add("text-overlap", a.el, `overlaps ${describe(b.el)}`);
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

export async function assertRegressionPatterns(page, { routeName, viewportLabel, screenshotDir, relativeToRepo = (p) => p }) {
  const width = page.viewportSize()?.width ?? 1280;
  let findings = await page.evaluate(collectRegressionFindings, { mobile: width < 768 || /mobile|phone/i.test(viewportLabel) });
  // Audit log / dead-letter queue list event codes by design; they are internal ops tooling.
  if (/^operations-(audit|dlq)/.test(routeName)) findings = findings.filter((f) => f.pattern !== "J-raw-text");
  if (findings.length === 0) return [];
  await page.addStyleTag({ content: "[data-smoke-issue]{outline:3px solid #e11d48 !important;outline-offset:1px}" });
  await page.evaluate(() => document.querySelector("[data-smoke-issue]")?.scrollIntoView({ block: "center", inline: "center" }));
  const issuesPath = join(screenshotDir, `${viewportLabel}-${routeName}-issues.png`);
  await page.screenshot({ path: issuesPath, fullPage: false });
  console.log(`screenshot_path=${relativeToRepo(issuesPath)}`);
  const first = findings[0].pattern;
  const summary = findings.slice(0, 3).map((f) => `[${f.pattern}] ${f.element} ${f.detail}`).join("; ");
  const more = findings.length > 3 ? ` (+${findings.length - 3} more)` : "";
  throw new Error(`${routeName} ${viewportLabel} ${first}: ${summary}${more}`);
}
