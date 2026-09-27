// r2-visual-audit plugin: letter-stacked text (R3OPS-3, FJ1-P0-3).
// A table cell / text leaf squeezed narrower than ~2 characters breaks its words one letter per line
// (the /calendar/drive/[eventId] animal roster: "G / o / d / e / l"). Any visible text leaf with >= 4
// letters whose box is under 2.2em wide and over 3 lines tall is a P0. Fix at the layout: template
// table kit (Scrollbar + Table minWidth + nowrap identity cells) or a stacked phone row, never a
// narrower font.

/** In-page probe (serialisable). Returns [{ detail }], one per distinct text. */
export function probeLetterStack() {
  const out = [];
  const seen = new Set();
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const text = (node.nodeValue || "").replace(/\s+/g, " ").trim();
    if ((text.match(/\p{L}|\p{N}/gu) || []).length < 4) continue;
    const el = node.parentElement;
    if (!el || seen.has(el)) continue;
    seen.add(el);
    const s = getComputedStyle(el);
    if (s.visibility === "hidden" || s.display === "none" || el.closest("[aria-hidden='true'], svg, script, style")) continue;
    if (s.writingMode && s.writingMode !== "horizontal-tb") continue;
    const range = document.createRange();
    range.selectNodeContents(node);
    const r = range.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) continue;
    const font = parseFloat(s.fontSize) || 14;
    const line = parseFloat(s.lineHeight) || font * 1.4;
    if (r.width < font * 2.2 && r.height > line * 3) {
      out.push({ detail: `"${text.slice(0, 24)}" in ${el.tagName.toLowerCase()}.${String(el.className).split(" ").slice(0, 2).join(".")} is ${Math.round(r.width)}px wide x ${Math.round(r.height / line)} lines` });
    }
  }
  return out.slice(0, 5);
}

export default {
  name: "text",
  p0: true,
  async run(page) {
    const found = await page.evaluate(probeLetterStack);
    return found.map((f) => ({ pattern: "letter-stack", label: "Text breaks one letter per line (column squeezed)", detail: f.detail }));
  },
};
