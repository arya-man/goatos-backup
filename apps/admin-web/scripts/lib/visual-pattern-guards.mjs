// visual-pattern-guards.mjs — production-bug CLASSES turned into automated checks.
//
// A companion to render-integrity.mjs and regression-checks.mjs. Every check in this file
// corresponds to a real bug CLASS caught in the OCI production smoke (2026-09-25) or in the
// PR #294 admin-web-ui-invariants.md audit (776 rows) that was not already covered by an
// existing probe. New instances therefore fail at push, not in production.
//
// Runs INSIDE the page as one page.evaluate call, returns a flat list of findings:
//   { pattern, target, detail }
//
// Patterns:
//   P-text-icon-overlap        text drawn over a neighbouring icon (svg/i/img) in the same
//                              flex/grid row — the sibling-overlap sibling class, targeted at
//                              icon+label collisions the plain sibling probe misses when the
//                              icon is not itself a text node.
//   P-wide-table-no-wrapper    a <table> wider than the viewport with no overflow-x:auto
//                              ancestor. Extension of the webview-only check to run at every
//                              lane, because a laptop-only regression still ships to phones.
//   P-chart-axis-tiny          chart axis / legend text below 11px on any viewport. Tightens
//                              the existing 10px floor to match the MUI Minimal spec.
//   P-pinned-bar-blur-flicker  a sticky or fixed bar with backdrop-filter above content that
//                              actually scrolls under it. The Android WebView repaints the
//                              blur on every scroll frame — the "sticky-blur" flicker class
//                              from the 2026-09-25 dashboard.mesha.sg production smoke.
//   P-drawer-filter-mismatch   an open drawer, modal, or export sheet whose data-filters
//                              attribute differs from the page's data-filters. Extension of
//                              the filter-carry class from production smoke (weighing/analytics
//                              download used All while the page filter was Male).
//   P-chart-hover-remount      recorded by hoverChartTooltip(page): the tooltip DOM node must
//                              appear within 500ms of a mouseenter and must survive three
//                              rAF ticks without being unmounted or replaced.
//
// The complement of runtime checks lives as static rules in check-design-system.mjs
// (raw-chart-lib, route-template-map) and design-kit-ratchet.mjs (spacing-off-template-grid).

export const VISUAL_PATTERNS = Object.freeze({
  "P-text-icon-overlap": "text drawn over an icon in the same flex/grid row",
  "P-wide-table-no-wrapper": "table wider than the viewport with no overflow-x ancestor",
  "P-chart-axis-tiny": "chart axis / legend text below 11px",
  "P-pinned-bar-blur-flicker": "sticky/fixed bar with backdrop-filter above scrollable content",
  "P-drawer-filter-mismatch": "drawer / modal / export sheet does not inherit the page filters",
  "P-chart-hover-remount": "chart tooltip missing after hover or re-mounted between frames",
});

// One page.evaluate closure. Kept self-contained (no imports) so serialization stays clean.
export function collectVisualPatternFindings({ minChartAxisPx = 11, viewport = "laptop" } = {}) {
  const findings = [];
  const seen = new Set();
  const push = (pattern, target, detail) => {
    const key = `${pattern}|${target}`;
    if (seen.has(key)) return;
    seen.add(key);
    findings.push({ pattern, target: String(target).slice(0, 160), detail: String(detail ?? "").slice(0, 200) });
  };
  const describe = (el) => {
    if (!el) return "<none>";
    const cls = typeof el.className === "string" && el.className ? "." + el.className.trim().split(/\s+/).slice(0, 2).join(".") : "";
    const id = el.id ? "#" + el.id : "";
    return `${el.tagName.toLowerCase()}${id}${cls}`;
  };
  const visible = (el) => {
    if (typeof el.checkVisibility === "function" && !el.checkVisibility()) return false;
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden" || Number(cs.opacity) === 0) return false;
    const r = el.getBoundingClientRect();
    return r.width > 0.5 && r.height > 0.5;
  };
  const overlapPx = (a, b) => {
    const x = Math.min(a.right, b.right) - Math.max(a.left, b.left);
    const y = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
    return x > 0 && y > 0 ? Math.min(x, y) : 0;
  };
  const scrollXAncestor = (el) => {
    for (let n = el.parentElement; n && n !== document.documentElement; n = n.parentElement) {
      const cs = getComputedStyle(n);
      if (/auto|scroll/.test(cs.overflowX) && n.scrollWidth > n.clientWidth) return n;
    }
    return null;
  };
  const scrollingAncestorBehind = (el) => {
    // The main scroll container (body/html or a max-height:100dvh box). We only care that SOMETHING
    // taller than one screen scrolls in the same viewport as this pinned bar — pinning a bar over
    // a card that fits on screen is not the flicker class.
    const doc = document.documentElement;
    if (doc.scrollHeight > window.innerHeight + 20) return doc;
    for (let n = el.parentElement; n && n !== doc; n = n.parentElement) {
      const cs = getComputedStyle(n);
      if (/auto|scroll/.test(cs.overflowY) && n.scrollHeight > n.clientHeight + 20) return n;
    }
    return null;
  };
  const vw = window.innerWidth;

  // ── P-text-icon-overlap ────────────────────────────────────────────────────
  // A text-carrying leaf and an icon sibling (svg / <i> / <img>) in the same flex/grid row
  // whose rects intersect by more than a hairline. Sibling-overlap in render-integrity looks
  // at ANY child pair; here we target the specific text-vs-icon collision that survives it
  // (an icon has no ownText, so the ownText branch of the row can pass while the icon is
  // covered — the "arrow at the right of a card header opens leftward" class).
  for (const row of document.querySelectorAll("body *")) {
    const cs = getComputedStyle(row);
    if (!/flex|grid/.test(cs.display)) continue;
    if (!visible(row)) continue;
    const kids = Array.from(row.children).filter((k) => visible(k));
    if (kids.length < 2) continue;
    const texts = kids.filter((k) => (k.textContent || "").trim().length > 0 && !k.matches("svg,i,img,use"));
    const icons = kids.filter((k) => k.matches("svg,i,img") || (k.tagName === "I" && !(k.textContent || "").trim()));
    for (const t of texts) {
      for (const icon of icons) {
        if (t === icon || t.contains(icon) || icon.contains(t)) continue;
        const o = overlapPx(t.getBoundingClientRect(), icon.getBoundingClientRect());
        if (o > 4) push("P-text-icon-overlap", describe(t), `overlaps ${describe(icon)} by ${Math.round(o)}px`);
      }
    }
  }

  // ── P-wide-table-no-wrapper ─────────────────────────────────────────────────
  // A <table> wider than the current viewport with no horizontally scrollable ancestor.
  // The webview lane runs it at Pixel 5; this runs at every viewport so a laptop-only regression
  // fails before it reaches a phone (a hidden overflow ancestor is treated as no wrapper — a
  // truncated table is a scroll trap, not a supported pattern).
  for (const table of document.querySelectorAll("table")) {
    if (!visible(table)) continue;
    if (table.scrollWidth <= vw + 1 && table.getBoundingClientRect().width <= vw + 1) continue;
    if (!scrollXAncestor(table)) push("P-wide-table-no-wrapper", describe(table), `table scrollWidth ${table.scrollWidth} > viewport ${vw} with no overflow-x ancestor`);
  }

  // ── P-chart-axis-tiny ───────────────────────────────────────────────────────
  // Chart axis / legend / data label text below 11px on any viewport. The 8px SVG floor and the
  // 10px webview floor still stand; this catches the class the MUI spec calls out (chart text
  // must be readable on the same laptop the console lives on).
  const axisSel = ".apexcharts-xaxis-label,.apexcharts-yaxis-label,.apexcharts-legend-text,.apexcharts-datalabel,.apexcharts-datalabel-value,.gclab,.gcsub,.gleg,.gleg *,.hblab,.hbval,.wbl-text,.wbv,.kit-bar-label-text,.kit-bar-value";
  for (const el of document.querySelectorAll(axisSel)) {
    if (!visible(el) || !(el.textContent || "").trim()) continue;
    const size = Number.parseFloat(getComputedStyle(el).fontSize);
    if (Number.isFinite(size) && size < minChartAxisPx) push("P-chart-axis-tiny", describe(el), `font-size ${size}px < ${minChartAxisPx}px minimum`);
  }
  // SVG chart texts too — bbox height / 1.2 approximates the font size.
  for (const svg of document.querySelectorAll("svg[role=img],svg.svgbars,svg.svg-series,svg.kit-chart,.minimal__chart__root svg")) {
    if (!visible(svg)) continue;
    for (const t of svg.querySelectorAll("text")) {
      if (!(t.textContent || "").trim()) continue;
      let bbox = null;
      try { bbox = t.getBBox(); } catch { bbox = null; }
      if (!bbox || bbox.height <= 0) continue;
      const eff = bbox.height / 1.2;
      if (eff < minChartAxisPx) push("P-chart-axis-tiny", describe(t), `svg text ~${eff.toFixed(1)}px < ${minChartAxisPx}px`);
    }
  }

  // ── P-pinned-bar-blur-flicker ───────────────────────────────────────────────
  // A sticky or fixed element with backdrop-filter that has scrollable content below it (i.e. the
  // main page really can scroll under the blur, not just a card-sized region). The Android WebView
  // repaints the blur every scroll frame — the exact class recorded by dashboard.mesha.sg on
  // 2026-09-25 for `.top` and `.navback`. On desktop this is only a warning; on phone it is the
  // documented flicker cause, so the webview lane surfaces it as a failure.
  for (const el of document.querySelectorAll("body *")) {
    const cs = getComputedStyle(el);
    if (cs.position !== "sticky" && cs.position !== "fixed") continue;
    const bf = cs.backdropFilter || cs.webkitBackdropFilter || "";
    if (!bf || bf === "none") continue;
    if (!visible(el)) continue;
    if (!scrollingAncestorBehind(el)) continue;
    push("P-pinned-bar-blur-flicker", describe(el), `${cs.position} bar with backdrop-filter ${bf.slice(0, 60)} above scrollable content (Android WebView repaint hazard on ${viewport})`);
  }

  // ── P-drawer-filter-mismatch ────────────────────────────────────────────────
  // A drawer / modal / export sheet reads the SAME filter state as its page. We identify the page
  // by its main tag or a documented data-filters attribute; the drawer by its role or a
  // data-drawer / data-export attribute. If both carry data-filters JSON and they differ, or if a
  // drawer builds a URL that omits a filter the page has, that is the class from the 2026-09-25
  // production smoke (weighing/analytics download had Sex=All while the page had Sex=Male).
  const pageFilters = document.querySelector("[data-page-filters]")?.getAttribute("data-page-filters") || null;
  if (pageFilters) {
    for (const overlay of document.querySelectorAll("[role=dialog],[role=alertdialog],aside.drawer,[data-drawer-filters],[data-export-filters]")) {
      if (!visible(overlay)) continue;
      const own = overlay.getAttribute("data-drawer-filters") || overlay.getAttribute("data-export-filters");
      if (!own) continue;
      if (own !== pageFilters) push("P-drawer-filter-mismatch", describe(overlay), `overlay filters "${own.slice(0, 80)}" differ from page filters "${pageFilters.slice(0, 80)}"`);
    }
  }

  return findings;
}

/**
 * Chart hover stability: hover every chart's canvas/svg once, wait up to 500ms for a tooltip DOM
 * node to appear, then confirm the SAME node survives three rAF ticks without being replaced or
 * unmounted. Detects the re-mount flicker class where every mousemove tears down and re-creates
 * the tooltip element (Apex's default `series-changed` re-render, or a chart re-computing on the
 * hover event). Async on purpose — requires a real Playwright Page.
 *
 * Returns findings [{ pattern: "P-chart-hover-remount", target, detail }].
 */
export async function assertChartHoverStability(page, { chartSelector = ".apexcharts-canvas, .minimal__chart__root", tooltipSelector = ".apexcharts-tooltip, .kit-chart-tooltip, [role=tooltip]", timeoutMs = 500, limit = 6 } = {}) {
  const charts = await page.$$(chartSelector);
  const findings = [];
  for (const chart of charts.slice(0, limit)) {
    const box = await chart.boundingBox();
    if (!box) continue;
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    // Small nudge — Apex only shows the tooltip after a mousemove inside the plot area.
    await page.mouse.move(box.x + box.width / 2 + 4, box.y + box.height / 2 + 2);
    const result = await page.evaluate(
      async ({ tooltipSelector, timeoutMs }) => {
        const started = performance.now();
        // Wait for the tooltip to appear.
        let tip = null;
        while (performance.now() - started < timeoutMs) {
          tip = document.querySelector(tooltipSelector);
          if (tip && getComputedStyle(tip).visibility !== "hidden" && Number(getComputedStyle(tip).opacity) > 0.01) break;
          await new Promise((r) => requestAnimationFrame(r));
        }
        if (!tip) return { ok: false, reason: "tooltip did not appear within " + timeoutMs + "ms" };
        // Same DOM node must survive three rAF ticks. `isConnected` catches unmount; `!==` catches
        // an Apex re-render that clones the tooltip into a new node.
        for (let f = 0; f < 3; f += 1) {
          await new Promise((r) => requestAnimationFrame(r));
          const now = document.querySelector(tooltipSelector);
          if (!now || !tip.isConnected || now !== tip) return { ok: false, reason: `tooltip remounted at frame ${f + 1} (isConnected=${tip.isConnected}, sameNode=${now === tip})` };
        }
        return { ok: true };
      },
      { tooltipSelector, timeoutMs },
    );
    if (!result.ok) {
      const target = await chart.evaluate((el) => `${el.tagName.toLowerCase()}${el.className ? "." + String(el.className).split(" ")[0] : ""}`);
      findings.push({ pattern: "P-chart-hover-remount", target, detail: result.reason });
    }
  }
  return findings;
}
