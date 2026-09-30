// r2-visual-audit plugin: text that reads broken (FIXJ1, J2/J3 judge findings).
// - button-label-wrap (J3 P1-3): a Button / LinkButton label breaks onto 2+ lines ("Open / the /
//   draft" in the /vaccination/plan draft Alert at 390). Buttons hold one line; group them with
//   ActionAlert / a wrapping Stack instead of squeezing.
// - axis-label-overlap (J3 P1-4): two unrotated Apex x-axis labels intersect ("SirohiAnantapur
//   Sheep" on /weighing/weights at 390). Rotate, trim or shorten them below 600px. No `profiles`
//   filter: the FULL audit runs it on every route at 1440 dark, 1440 light and 390 dark (the fast
//   pre-push lane caps its touched routes at 8; /feed/analytics "Feed mix" 60,000 x 80,000 at 390
//   was only caught by a full run; guard: chart-label-every-route in text-fit.test.mjs).
// - raw-id-text (J2 P1-3/P1-4): a table cell, card header or list line shows a raw id: an 8-hex
//   hash (`cee6e124`), a UUID, or a snake_case table name (`goat_identity_events`).
// - placeholder-clipped (J2 P2-3): a text field's placeholder is wider than the field.
// - dead-primary (J2 P1-1, widened J2B P2-8): a disabled contained button (or a disabled Apply /
//   Save / Submit of any variant) ANYWHERE in the page -- header, page body -- and, through the
//   drawers lane, inside an open drawer / dialog (probeDeadControls). DECIDED no dead controls:
//   render it only when it works (hide it until something is staged / changed), or show the reason
//   as text. Pagers and a button showing its loading spinner are not dead.
// - header-primary-height (J2B P2-10): a header contained action taller than 40px at desktop.
// - field-collapsed (J2B P1-1, drawers lane): a form field in a drawer / dialog body narrower than
//   40% of the widest field beside it (the /people Add person Department select at ~60px, label
//   "Dep…"). Form-column fields share one width, like the template's form TextFields.

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
    // Apex nests the label in a <tspan> beside a <title> copy: read the tspan, or the detail prints
    // every label twice ("80,00080,000").
    const boxes = labels.map((t) => ({ text: ((t.querySelector("tspan") || t).textContent || "").trim(), r: t.getBoundingClientRect() }));
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

  // header-primary-height (J2B P2-10): on desktop a header contained action is the template's 36px
  // medium button (/people "Add person" stood 44px beside 36px primaries on every other page).
  const hdr = document.querySelector("[data-page-header]");
  if (hdr && innerWidth >= 1200 && !matchMedia("(pointer: coarse)").matches) {
    for (const btn of hdr.querySelectorAll(".MuiButton-contained")) {
      if (!visible(btn)) continue;
      const h = Math.round(btn.getBoundingClientRect().height);
      if (h > 40) out.push({ kind: "header-primary-height", detail: `"${(btn.innerText || "").trim().slice(0, 40)}" is ${h}px tall (template header action 36px)` });
    }
  }
  return out;
}

/**
 * In-page probe (serialisable): dead controls under `scopeSel` (the page content column, or the
 * open overlay paper `[data-r2-paper]`). Returns [{ kind: "dead-primary", detail }].
 */
export function probeDeadControls(scopeSel) {
  const out = [];
  const visible = (el) => {
    const r = el.getBoundingClientRect();
    const s = getComputedStyle(el);
    return r.width > 0 && r.height > 0 && s.visibility !== "hidden" && s.display !== "none" && !el.closest(".sr-only, .MuiSkeleton-root");
  };
  const ACTION_RE = /^(apply|save|submit|update|confirm|download|export)\b/i;
  const seen = new Set();
  for (const scope of document.querySelectorAll(scopeSel)) {
    for (const btn of scope.querySelectorAll(".MuiButton-root.Mui-disabled, .MuiButton-root[disabled], .MuiButton-root[aria-disabled=true]")) {
      if (seen.has(btn) || !visible(btn)) continue;
      seen.add(btn);
      if (btn.closest(".MuiTablePagination-root, .MuiPagination-root, [data-pager], [aria-busy=true]") || btn.matches(".MuiButton-loading, [aria-busy=true]")) continue;
      const text = (btn.innerText || btn.getAttribute("aria-label") || "").trim().replace(/\s+/g, " ");
      if (!btn.classList.contains("MuiButton-contained") && !ACTION_RE.test(text)) continue;
      const where = btn.closest("[data-page-header]") ? "page header" : btn.closest(".MuiDrawer-paper, .MuiDialog-paper, [role=dialog]") ? "drawer / dialog" : "page body";
      out.push({ kind: "dead-primary", detail: `"${text.slice(0, 40)}" is disabled in the ${where}` });
    }
  }
  return out;
}

/**
 * In-page probe (serialisable): form fields in the overlay paper `scopeSel` narrower than 40% of the
 * widest form field there (a collapsed select next to full-width siblings). Returns
 * [{ kind: "field-collapsed", detail }].
 */
export function probeFieldWidths(scopeSel) {
  const out = [];
  const paper = document.querySelector(scopeSel);
  if (!paper) return out;
  const fields = [...paper.querySelectorAll(".MuiTextField-root, .MuiAutocomplete-root")].filter((el) => {
    const r = el.getBoundingClientRect();
    const s = getComputedStyle(el);
    return r.width > 0 && r.height > 0 && s.visibility !== "hidden" && !el.closest("table, .MuiTablePagination-root, [role=toolbar]") && !el.parentElement?.closest(".MuiAutocomplete-root");
  });
  if (fields.length < 2) return out;
  const widest = Math.max(...fields.map((el) => el.getBoundingClientRect().width));
  for (const el of fields) {
    const w = el.getBoundingClientRect().width;
    if (w >= widest * 0.4) continue;
    const label = (el.querySelector("label")?.innerText || el.querySelector("input")?.getAttribute("name") || "").trim();
    out.push({ kind: "field-collapsed", detail: `"${label.slice(0, 40)}" is ${Math.round(w)}px wide next to ${Math.round(widest)}px fields` });
  }
  return out;
}

const LABELS = {
  "button-label-wrap": "Button label breaks onto several lines",
  "axis-label-overlap": "Chart axis labels overlap",
  "raw-id-text": "Raw id / table name shown as text",
  "dead-primary": "Disabled (dead) primary / Apply / Save action",
  "field-collapsed": "Drawer form field collapsed next to full-width fields",
  "header-primary-height": "Header primary action taller than the template 36px",
  "placeholder-clipped": "Field placeholder cut by the field edge",
};

export default {
  name: "text-fit",
  p0: true,
  async run(page) {
    const found = [...(await page.evaluate(probeTextFit)), ...(await page.evaluate(probeDeadControls, ".minimal__layout__main__content, main"))];
    return found.map((f) => ({ pattern: f.kind, label: LABELS[f.kind], detail: f.detail }));
  },
};
