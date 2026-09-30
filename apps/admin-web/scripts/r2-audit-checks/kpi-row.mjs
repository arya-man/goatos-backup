// r2-visual-audit plugin: KPI rows (J2 P1-8).
// - mixed-kind: one row of KPI tiles (same top, same Grid container) holds two template widget kinds
//   (the Booking card with its outlined icon + circle beside Course tiles on /counts/analytics,
//   /counts/mortality, /health/analytics). KpiWidget stamps data-kpi-kind; one kind per row.
// - empty-slot: a KPI row stops short of its container's width while another row of the same deck is
//   full (/feed/analytics 3 + 2 at md 4 left an empty third slot). Size the short row to fill it.

/** In-page probe (serialisable). Returns [{ kind, detail }]. */
export function probeKpiRows() {
  const out = [];
  const tiles = [...document.querySelectorAll("[data-kpi-kind]")].filter((el) => {
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0 && getComputedStyle(el).visibility !== "hidden";
  });
  const decks = new Map();
  for (const el of tiles) {
    // The deck = the nearest Grid container holding the tile's Grid item.
    const deck = el.closest(".MuiGrid-container") || el.parentElement?.parentElement;
    if (!deck) continue;
    if (!decks.has(deck)) decks.set(deck, []);
    decks.get(deck).push(el);
  }
  for (const [deck, list] of decks) {
    if (list.length < 2) continue;
    const rows = new Map();
    for (const el of list) {
      const top = Math.round(el.getBoundingClientRect().top / 6);
      if (!rows.has(top)) rows.set(top, []);
      rows.get(top).push(el);
    }
    const dr = deck.getBoundingClientRect();
    const widths = [];
    for (const row of rows.values()) {
      const kinds = [...new Set(row.map((el) => el.getAttribute("data-kpi-kind")))];
      const title = (el) => (el.innerText || "").trim().split("\n").slice(0, 2).join(" ").slice(0, 30);
      if (kinds.length > 1) out.push({ kind: "mixed-kind", detail: `${kinds.join(" + ")} in one row: ${row.map(title).join(" | ")}` });
      const left = Math.min(...row.map((el) => el.getBoundingClientRect().left));
      const right = Math.max(...row.map((el) => el.getBoundingClientRect().right));
      widths.push({ span: right - left, row });
    }
    if (rows.size < 2) continue;
    const full = widths.some((w) => w.span >= dr.width * 0.9);
    for (const w of widths) {
      if (full && w.span < dr.width * 0.8 && w.row.length < list.length) out.push({ kind: "empty-slot", detail: `a row of ${w.row.length} tile(s) spans ${Math.round(w.span)}px of ${Math.round(dr.width)}px` });
    }
  }
  return out;
}

export default {
  name: "kpi-row",
  p0: true,
  profiles: ["1440-dark", "1440-light"],
  async run(page) {
    const found = await page.evaluate(probeKpiRows);
    return found.map((f) => ({ pattern: f.kind, label: f.kind === "mixed-kind" ? "KPI row mixes template widget kinds" : "KPI row leaves an empty slot", detail: f.detail }));
  },
};
