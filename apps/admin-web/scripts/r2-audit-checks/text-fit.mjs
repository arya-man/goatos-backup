// r2-visual-audit plugin: text that reads broken (FIXJ1, J2/J3 judge findings).
// - button-label-wrap (J3 P1-3): a Button / LinkButton label breaks onto 2+ lines ("Open / the /
//   draft" in the /vaccination/plan draft Alert at 390). Buttons hold one line; group them with
//   ActionAlert / a wrapping Stack instead of squeezing.
// - axis-label-overlap (J3 P1-4): two unrotated Apex x-axis labels intersect ("SirohiAnantapur
//   Sheep" on /weighing/weights at 390). Rotate, trim or shorten them below 600px.
// - raw-id-text (J2 P1-3/P1-4): a table cell, card header or list line shows a raw id: an 8-hex
//   hash (`cee6e124`), a UUID, or a snake_case table name (`goat_identity_events`).
// - placeholder-clipped (J2 P2-3): a text field's placeholder is wider than the field.
// - dead-primary (J2 P1-1): a disabled contained button in the page header (a dead primary action;
//   DECIDED no dead controls: render it only when it works, or show the reason as text).

/** In-page probe (serialisable). Returns [{ kind, detail }]. */
export function probeTextFit() {
  const out = [];
  const visible = (el) => {
    const r = el.getBoundingClientRect();
    const s = getComputedStyle(el);
    return r.width > 0 && r.height > 0 && s.visibility !== "hidden" && s.display !== "none" && !el.closest("[aria-hidden=true], .sr-only, .MuiSkeleton-root");
  };
  const lineCount = (el) => {
    const range = document.createRange();
    range.selectNodeContents(el);
    const tops = new Set();
    for (const r of range.getClientRects()) if (r.width > 1 && r.height > 1) tops.add(Math.round(r.top / 4));
    return tops.size;
  };
  const root = document.querySelector(".minimal__layout__main__content, main") || document.body;

  // button-label-wrap
  const seenBtn = new Set();
  for (const btn of root.querySelectorAll(".MuiButton-root")) {
    if (!visible(btn)) continue;
    const label = [...btn.childNodes].filter((n) => n.nodeType === 3 || (n.nodeType === 1 && !n.matches(".MuiButton-startIcon, .MuiButton-endIcon, svg, .MuiTouchRipple-root")));
    const text = (btn.innerText || "").trim().replace(/\s+/g, " ");
    if (!text || !/\s/.test(text) || seenBtn.has(text)) continue;
    let lines = 0;
    for (const n of label) {
      if (n.nodeType === 3) {
        const range = document.createRange();
        range.selectNodeContents(n);
        const tops = new Set([...range.getClientRects()].filter((r) => r.width > 1).map((r) => Math.round(r.top / 4)));
        lines = Math.max(lines, tops.size);
      } else lines = Math.max(lines, lineCount(n));
    }
    if (lines >= 2) {
      seenBtn.add(text);
      out.push({ kind: "button-label-wrap", detail: `"${text.slice(0, 40)}" on ${lines} lines (${Math.round(btn.getBoundingClientRect().width)}px wide)` });
    }
  }

  // axis-label-overlap
  for (const chart of root.querySelectorAll(".apexcharts-canvas")) {
    if (!visible(chart)) continue;
    const labels = [...chart.querySelectorAll(".apexcharts-xaxis-label")].filter((t) => {
      if (!visible(t)) return false;
      const tr = t.getAttribute("transform") || "";
      return !/rotate\(\s*-?[1-9]/.test(tr) && (t.textContent || "").trim() !== "";
    });
    const boxes = labels.map((t) => ({ text: (t.textContent || "").trim(), r: t.getBoundingClientRect() }));
    let hit = null;
    for (let i = 0; i < boxes.length && !hit; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        const a = boxes[i].r, b = boxes[j].r;
        const ix = Math.min(a.right, b.right) - Math.max(a.left, b.left);
        const iy = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
        if (ix > 2 && iy > 2) { hit = `"${boxes[i].text.slice(0, 24)}" x "${boxes[j].text.slice(0, 24)}" overlap ${Math.round(ix)}px`; break; }
      }
    }
    if (hit) out.push({ kind: "axis-label-overlap", detail: hit });
  }

  // raw-id-text
  const HEX8 = /^(?=[0-9a-f]*\d)(?=[0-9a-f]*[a-f])[0-9a-f]{8}$/;
  const UUID = /\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/;
  const SNAKE = /^[a-z][a-z0-9]*(?:_[a-z0-9]+){2,}$/;
  const seenId = new Set();
  const textEls = root.querySelectorAll("td, .MuiCardHeader-title, .MuiCardHeader-subheader, .MuiCardHeader-action, .MuiListItemText-primary, .MuiListItemText-secondary, .minimal__label__root, .MuiChip-label");
  for (const el of textEls) {
    if (!visible(el) || el.closest("input, textarea, code, [data-raw-id-ok]")) continue;
    const tokens = (el.innerText || "").split(/[\s·,()→]+/).map((t) => t.trim()).filter(Boolean);
    for (const tok of tokens) {
      const kind = HEX8.test(tok) ? "hash" : UUID.test(tok) ? "uuid" : SNAKE.test(tok) ? "snake_case" : null;
      if (!kind || seenId.has(tok)) continue;
      seenId.add(tok);
      out.push({ kind: "raw-id-text", detail: `${kind} "${tok}" in ${el.tagName.toLowerCase()}.${String(el.className).split(" ")[0]}` });
    }
  }

  // placeholder-clipped (J2 P2-3): a text field's placeholder wider than the field ("…breed or gatev").
  const ctx2d = document.createElement("canvas").getContext("2d");
  for (const input of root.querySelectorAll("input[placeholder]:not([type=hidden])")) {
    if (!visible(input) || !ctx2d || input.value) continue;
    const cs = getComputedStyle(input);
    ctx2d.font = `${cs.fontStyle} ${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`;
    const room = input.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
    const need = ctx2d.measureText(input.placeholder).width;
    if (room > 0 && need > room + 2) out.push({ kind: "placeholder-clipped", detail: `"${input.placeholder.slice(0, 40)}" needs ${Math.round(need)}px in ${Math.round(room)}px` });
  }

  // dead-primary
  const header = document.querySelector("[data-page-header]");
  if (header) {
    for (const btn of header.querySelectorAll(".MuiButton-contained.Mui-disabled, .MuiButton-contained[disabled], .MuiButton-contained[aria-disabled=true]")) {
      if (!visible(btn)) continue;
      out.push({ kind: "dead-primary", detail: `"${(btn.innerText || "").trim().slice(0, 40)}" is disabled in the page header` });
    }
  }
  return out;
}

const LABELS = {
  "button-label-wrap": "Button label breaks onto several lines",
  "axis-label-overlap": "Chart axis labels overlap",
  "raw-id-text": "Raw id / table name shown as text",
  "dead-primary": "Disabled primary action in the page header",
  "placeholder-clipped": "Field placeholder cut by the field edge",
};

export default {
  name: "text-fit",
  p0: true,
  async run(page) {
    const found = await page.evaluate(probeTextFit);
    return found.map((f) => ({ pattern: f.kind, label: LABELS[f.kind], detail: f.detail }));
  },
};
