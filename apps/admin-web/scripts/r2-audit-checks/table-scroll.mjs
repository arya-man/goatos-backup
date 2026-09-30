// r2-visual-audit plugin: wide tables at phone width (FIXJ11, J3B N-P1-1 + P2-1).
//  - sticky-identity: every table that scrolls sideways inside its card keeps its first (identity)
//    and last (action) body cells on screen when the row is scrolled: after scrollLeft = 160 the
//    first cell keeps its x, the last cell keeps its right edge, both paint an opaque background
//    and both win elementFromPoint (nothing scrolls over them). The rule lives in
//    components/app/table/sticky-first-column.ts (PHONE_STICKY_EDGES_SX + AppBaseline); it went
//    missing once when the legacy stylesheet was deleted (c4bd347ff) and no gate noticed.
//  - table-scroll-trap: a table's sideways scroller never ALSO scrolls vertically (a 19px inner
//    vertical scroll on /vaccination/care-coverage ate the first vertical swipe over the table).
//    A webview reader's vertical swipe must always scroll the page.

/** In-page probe (serialisable). */
export function probeTableScroll() {
  const out = [];
  const opaque = (el) => {
    const m = /rgba?\(([^)]+)\)/.exec(getComputedStyle(el).backgroundColor || "");
    if (!m) return false;
    const parts = m[1].split(/[,\s/]+/).filter(Boolean);
    return parts.length < 4 || Number(parts[3]) >= 0.99;
  };
  const scrollerOf = (el) => {
    for (let c = el.parentElement; c && c !== document.body; c = c.parentElement) {
      const s = getComputedStyle(c);
      if (/(auto|scroll)/.test(s.overflowX) && c.scrollWidth > c.clientWidth + 1) return c;
    }
    return null;
  };
  const seen = new Set();
  for (const table of document.querySelectorAll("table")) {
    if (table.closest("[role=dialog], .MuiDrawer-root, .MuiPopover-root, [aria-hidden=true]")) continue;
    const tb = table.getBoundingClientRect();
    if (tb.width < 1 || tb.height < 1) continue;
    const scroller = scrollerOf(table);
    if (!scroller || seen.has(scroller)) continue;
    seen.add(scroller);
    const name = (table.getAttribute("aria-label") || scroller.getAttribute("aria-label") || table.querySelector("th")?.textContent || "table").trim().slice(0, 40);
    const ss = getComputedStyle(scroller);
    // The template Scrollbar (simplebar) scrolls on its content wrapper; its y overflow is the trap.
    if (/(auto|scroll)/.test(ss.overflowY) && scroller.scrollHeight > scroller.clientHeight + 1) {
      out.push({ pattern: "table-scroll-trap", detail: `"${name}" scroller also scrolls vertically (${scroller.scrollHeight} > ${scroller.clientHeight})` });
    }
    // A phone list that stacks its rows (tr as grid / block) has no columns to pin.
    const row = [...table.querySelectorAll(":scope > tbody > tr")].find((r) => {
      if (getComputedStyle(r).display !== "table-row") return false;
      const cells = [...r.children].filter((c) => /^T[DH]$/.test(c.tagName));
      return cells.length > 1 && !cells[0].hasAttribute("colspan") && cells[0].getBoundingClientRect().height > 0;
    });
    if (!row) continue;
    const cells = [...row.children].filter((c) => /^T[DH]$/.test(c.tagName));
    const first = cells[0];
    const last = cells[cells.length - 1];
    const before = { first: first.getBoundingClientRect(), last: last.getBoundingClientRect() };
    const x0 = scroller.scrollLeft;
    const shift = Math.min(160, scroller.scrollWidth - scroller.clientWidth);
    if (shift < 24) continue;
    scroller.scrollLeft = x0 + shift;
    const sb = scroller.getBoundingClientRect();
    const check = (cell, edge, b0) => {
      const b = cell.getBoundingClientRect();
      // Pinned = it did not move, or it now rests on the scroller's edge (border-spacing aside).
      const moved = edge === "first"
        ? Math.min(Math.abs(b.left - b0.left), Math.abs(b.left - sb.left))
        : Math.min(Math.abs(b.right - b0.right), Math.abs(b.right - sb.right));
      if (moved > 1.5) return `the ${edge} column scrolled away (${Math.round(moved)}px)`;
      if (!opaque(cell)) return `the ${edge} column is see-through (${getComputedStyle(cell).backgroundColor})`;
      const px = edge === "first" ? Math.min(b.right - 4, b.left + 8) : Math.max(b.left + 4, b.right - 8);
      const py = b.top + Math.min(b.height / 2, 12);
      if (py < 0 || py > innerHeight) return null;
      const hit = document.elementFromPoint(px, py);
      if (hit && !cell.contains(hit)) return `the ${edge} column is painted over by a scrolling cell`;
      return null;
    };
    const firstFail = check(first, "first", before.first);
    // The last column is held to the rule only as the row's action column (⋮ RowMenu / IconButton).
    const isAction = !!last.querySelector("[data-row-menu]") || [...last.children].some((c) => c.matches(".MuiIconButton-root"));
    const lastFail = last !== first && isAction ? check(last, "last", before.last) : null;
    scroller.scrollLeft = x0;
    for (const f of [firstFail, lastFail]) if (f) out.push({ pattern: "sticky-identity", detail: `"${name}" at ${innerWidth}px: ${f}` });
  }
  return out.slice(0, 8);
}

export default {
  name: "table-scroll",
  p0: true,
  profiles: ["390-dark"],
  async run(page) {
    const found = await page.evaluate(probeTableScroll);
    return found.map((f) => ({
      pattern: f.pattern,
      label: f.pattern === "sticky-identity" ? "Wide table loses its identity / action column when scrolled sideways at 390" : "Table scroller traps the vertical swipe at 390",
      detail: f.detail,
    }));
  },
};
