// r2-visual-audit plugin: card fit at phone width (REVIEW-24).
//  - card-inner-scroll: at 390 a card never holds its own scroller (an element inside a .MuiCard-root
//    whose content overflows and that scrolls on x or y) unless it is a declared table scroller
//    (a table inside, the template table kit) or a chart; a nested scroller in the webview traps the
//    thumb (/sales/sold price list: template Scrollbar minWidth 360).
//  - row-overlap: a label and its figure in one flex row never overlap or touch (< 8px apart) (/weighing/analytics balance
//    rows: long pen names ran into the grams).

/** In-page probe (serialisable). */
export function probeCardFit() {
  const out = [];
  const vw = document.documentElement.clientWidth;
  for (const card of document.querySelectorAll(".MuiCard-root")) {
    for (const el of card.querySelectorAll("*")) {
      const s = getComputedStyle(el);
      if (s.display === "none" || s.visibility === "hidden") continue;
      // Sideways: only a VERTICAL list forced wider than the card (a price list with minWidth 360);
      // a horizontal strip of analytic cells scrolling sideways is the template invoice-list pattern.
      const inner = el.querySelector(".simplebar-content > *") || el.firstElementChild;
      const verticalList = !!inner && (inner.getAttribute("role") === "list" || getComputedStyle(inner).flexDirection === "column");
      const scrollsX = /(auto|scroll)/.test(s.overflowX) && el.scrollWidth > el.clientWidth + 1 && verticalList;
      const scrollsY = /(auto|scroll)/.test(s.overflowY) && el.scrollHeight > el.clientHeight + 1;
      if (vw < 600 && (scrollsX || scrollsY) && !el.querySelector("table, .apexcharts-canvas") && !el.closest("table, [role=dialog], .MuiPopover-root, .MuiDrawer-root, .MuiTabs-root, .MuiTabs-scroller")) {
        out.push({ pattern: "card-inner-scroll", detail: `${el.tagName.toLowerCase()}.${String(el.className).split(" ").slice(0, 2).join(".")} scrolls ${scrollsX ? "x" : ""}${scrollsY ? "y" : ""} inside a card at ${vw}px` });
      }
      if (s.display === "flex" && s.justifyContent === "space-between" && el.children.length === 2) {
        // The painted TEXT, not the flex box: a nowrap label overflows its shrunken box.
        const textRect = (c) => { const r = document.createRange(); r.selectNodeContents(c); const t = r.getBoundingClientRect(); return t.width > 0 ? t : c.getBoundingClientRect(); };
        const [a, b] = [...el.children].map(textRect);
        if (a.width > 0 && b.width > 0 && a.right > b.left - 8 && a.top < b.bottom - 1 && b.top < a.bottom - 1) {
          out.push({ pattern: "row-overlap", detail: `"${(el.children[0].textContent || "").trim().slice(0, 24)}" overlaps "${(el.children[1].textContent || "").trim().slice(0, 12)}"` });
        }
      }
    }
  }
  return out.slice(0, 8);
}

export default {
  name: "card-fit",
  p0: true,
  profiles: ["390-dark"],
  async run(page) {
    const found = await page.evaluate(probeCardFit);
    return found.map((f) => ({ pattern: f.pattern, label: f.pattern === "row-overlap" ? "Label and figure overlap in a card row" : "Nested scroller inside a card at phone width", detail: f.detail }));
  },
};
