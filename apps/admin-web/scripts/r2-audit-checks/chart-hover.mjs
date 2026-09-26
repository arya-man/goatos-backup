// r2-visual-audit plugin: hover every chart and fail on a clipped tooltip or visible a11y text.
//
// Ravi 2026-09-27 (/sales/sold "Month by month" in dark, and the same on many pages): hovering a
// bar showed a tooltip cut off by the card / a sideways scroller, and the raw ApexCharts
// accessibility string ("bar chart with 1 data series: Revenue") showed on screen (the SVG
// <title> Apex writes becomes the browser's native hover tooltip). Static guards
// (scripts/lib/chart-template-guards.mjs) pin the code; this check proves it in the browser.
//
// For each visible ApexCharts canvas (up to MAX_CHARTS per page) it moves the real mouse across
// the plot at three points and asserts:
//   chart-hover|tooltip-clipped   the active .apexcharts-tooltip lies fully inside the viewport and
//                                 inside every ancestor that clips (overflow other than visible);
//   chart-hover|a11y-text         no SVG <title> / visible node carries Apex's generated
//                                 "<type> chart with N data series" string.
// Both are P0 (fail the visual gate).

const MAX_CHARTS = 8;
const RAW_A11Y = /\bchart with \d+ data series\b/i;

export function tooltipClip(tip, clips, viewport, tolerance = 1) {
  const out = [];
  if (tip.left < -tolerance || tip.top < -tolerance || tip.right > viewport.width + tolerance || tip.bottom > viewport.height + tolerance) out.push("viewport");
  for (const c of clips) {
    if (tip.left < c.left - tolerance || tip.right > c.right + tolerance || tip.top < c.top - tolerance || tip.bottom > c.bottom + tolerance) out.push(c.sig);
  }
  return out;
}

export default {
  name: "chart-hover",
  p0: true,
  async run(page) {
    const findings = [];
    const count = await page.evaluate((max) => {
      const vis = (el) => {
        const r = el.getBoundingClientRect();
        return r.width > 40 && r.height > 40 && getComputedStyle(el).visibility !== "hidden";
      };
      const charts = [...document.querySelectorAll(".apexcharts-canvas")].filter(vis).slice(0, max);
      charts.forEach((c, i) => c.setAttribute("data-r2-chart", String(i)));
      return charts.length;
    }, MAX_CHARTS);

    // Raw a11y text anywhere (SVG <title> = native hover tooltip, or a rendered node).
    const raw = await page.evaluate((src) => {
      const re = new RegExp(src, "i");
      const hits = [];
      for (const t of document.querySelectorAll(".apexcharts-canvas svg > title, .apexcharts-canvas svg > desc")) if (re.test(t.textContent || "")) hits.push(`svg <${t.tagName.toLowerCase()}>: "${t.textContent}"`);
      const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
      for (let n = walker.nextNode(); n; n = walker.nextNode()) {
        if (!re.test(n.textContent || "")) continue;
        const el = n.parentElement;
        if (!el || el.closest("title, desc, script, style")) continue;
        const r = el.getBoundingClientRect();
        const cs = getComputedStyle(el);
        const hidden = r.width <= 1 || r.height <= 1 || cs.visibility === "hidden" || cs.display === "none" || cs.clipPath.includes("inset(50%") || cs.clip === "rect(0px, 0px, 0px, 0px)";
        if (!hidden) hits.push(`visible text: "${(n.textContent || "").trim().slice(0, 80)}"`);
      }
      return hits;
    }, RAW_A11Y.source);
    for (const detail of raw.slice(0, 3)) findings.push({ label: "Chart shows raw ApexCharts a11y text on screen / as a hover title", pattern: "a11y-text", detail, p0: true });

    for (let i = 0; i < count; i++) {
      const handle = await page.$(`[data-r2-chart="${i}"]`);
      if (!handle) continue;
      // Centre the chart: flush against the viewport edge, a tooltip that opens below/above the plot
      // (still inside its card) would read as viewport-clipped although a reader scrolls to it.
      await handle.evaluate((el) => el.scrollIntoView({ block: "center", inline: "nearest" })).catch(() => {});
      const plot = await page.evaluate((idx) => {
        const c = document.querySelector(`[data-r2-chart="${idx}"]`);
        const g = c?.querySelector(".apexcharts-grid, .apexcharts-inner") ?? c;
        const r = g?.getBoundingClientRect();
        return r ? { left: r.left, top: r.top, width: r.width, height: r.height } : null;
      }, i);
      if (!plot || plot.width < 20 || plot.height < 20) continue;
      let clipped = null;
      for (const f of [0.08, 0.5, 0.92]) {
        await page.mouse.move(plot.left + plot.width * f, plot.top + plot.height * 0.6);
        await page.waitForTimeout(350);
        const probe = await page.evaluate((idx) => {
          const c = document.querySelector(`[data-r2-chart="${idx}"]`);
          const tip = c?.querySelector(".apexcharts-tooltip.apexcharts-active");
          if (!tip) return null;
          const t = tip.getBoundingClientRect();
          if (t.width < 2 || t.height < 2) return null;
          const clips = [];
          for (let a = c.parentElement; a && a !== document.documentElement; a = a.parentElement) {
            const cs = getComputedStyle(a);
            if (cs.overflowX !== "visible" || cs.overflowY !== "visible") {
              const r = a.getBoundingClientRect();
              const sig = `${a.tagName.toLowerCase()}${a.className && typeof a.className === "string" ? "." + a.className.trim().split(/\s+/).slice(0, 2).join(".") : ""}`;
              clips.push({ left: cs.overflowX === "visible" ? -1e9 : r.left, right: cs.overflowX === "visible" ? 1e9 : r.right, top: cs.overflowY === "visible" ? -1e9 : r.top, bottom: cs.overflowY === "visible" ? 1e9 : r.bottom, sig });
            }
          }
          return { tip: { left: t.left, top: t.top, right: t.right, bottom: t.bottom }, clips, viewport: { width: innerWidth, height: innerHeight }, title: tip.querySelector(".apexcharts-tooltip-title")?.textContent ?? "" };
        }, i);
        if (!probe) continue;
        const by = tooltipClip(probe.tip, probe.clips, probe.viewport);
        if (by.length) {
          clipped = `chart ${i + 1}: tooltip "${probe.title.slice(0, 40)}" clipped by ${by.join(", ")}`;
          break;
        }
      }
      if (clipped) findings.push({ label: "Chart tooltip is clipped on hover", pattern: "tooltip-clipped", detail: clipped, p0: true });
      await page.mouse.move(0, 0);
    }
    return findings;
  },
};
