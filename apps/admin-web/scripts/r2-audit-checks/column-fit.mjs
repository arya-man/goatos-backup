// r2-visual-audit plugin: page blocks fit the content column (TR1-#5, guard: url-panel-min-width).
// The page root (`.wrap > .screen`) is a one-column grid whose children get `min-width: 0`. A
// `display: contents` wrapper (the UrlSuspense / UrlPanel box) hands ITS children to that grid
// without the rule, so one wide child (a 960px table, a chart) sizes the single track to its
// min-content and every block — header, KPI row, toolbar — renders wider than the column and is
// clipped at the right edge (/sales/loads: 1561px blocks in a 1060px column). Any page block
// (a child of the root, looking through `display: contents`) wider than the root is a P0.

/** In-page probe (serialisable). Returns [{ detail }]. */
export function probeColumnFit() {
  const out = [];
  const root = document.querySelector(".wrap > *, [data-page-root]");
  if (!root) return out;
  const width = root.getBoundingClientRect().width;
  if (width <= 0) return out;
  const blocks = [];
  const collect = (el) => {
    for (const child of el.children) {
      const s = getComputedStyle(child);
      if (s.display === "contents") collect(child);
      else if (s.display !== "none" && s.position !== "fixed" && s.position !== "absolute") blocks.push(child);
    }
  };
  collect(root);
  for (const el of blocks) {
    const w = el.getBoundingClientRect().width;
    if (w > width + 2) {
      out.push({ detail: `${el.tagName.toLowerCase()}.${String(el.className).split(" ").filter(Boolean).slice(0, 2).join(".")} is ${Math.round(w)}px wide in a ${Math.round(width)}px column` });
    }
  }
  return out.slice(0, 5);
}

export default {
  name: "layout",
  p0: true,
  async run(page) {
    const found = await page.evaluate(probeColumnFit);
    return found.map((f) => ({ pattern: "wider-than-column", label: "Page block wider than the content column", detail: f.detail }));
  },
};
