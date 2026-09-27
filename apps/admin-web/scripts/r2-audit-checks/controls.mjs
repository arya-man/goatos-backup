// r2-visual-audit plugin: list toolbar + pager integrity (R3OPS-3, /tasks FJ findings).
// - label-doubled: a field says its label twice (an external caption over a TextField that also has
//   `label`, or InputLabel + a visible legend): "Sort Sort" on /tasks.
// - control-overlap: two form controls in one toolbar overlap (the doubled Sort label ran into the
//   search field).
// - pager-clipped: a TablePagination / Pagination arrow falls outside its card or the pager scrolls
//   sideways (the /tasks arrows were cut at the card edge on phones).

/** In-page probe (serialisable). Returns [{ kind, detail }]. */
export function probeControls() {
  const out = [];
  const shown = (el) => {
    if (!el || !el.getClientRects().length) return false;
    const s = getComputedStyle(el);
    return s.visibility !== "hidden" && s.display !== "none" && parseFloat(s.opacity || "1") > 0.05;
  };
  const name = (el) => `${el.tagName.toLowerCase()}.${String(el.className).split(" ").slice(0, 2).join(".")}`;
  const layer = (el) => el.closest(".MuiModal-root, .MuiPopover-root, .MuiPopper-root, [role=dialog]");
  const text = (el) => (el.textContent || "").replace(/[\s*]+/g, " ").trim().toLowerCase();
  const controls = [...document.querySelectorAll(".MuiFormControl-root")].filter(shown).filter((c) => !c.parentElement?.closest(".MuiFormControl-root"));
  for (const control of controls) {
    const label = [...control.querySelectorAll(":scope > label, :scope > .MuiInputLabel-root")].find(shown);
    if (!label || !text(label)) continue;
    const legend = control.querySelector("legend span");
    const legendShown = legend && shown(legend) && text(legend) === text(label);
    let prev = control.previousElementSibling;
    while (prev && !shown(prev)) prev = prev.previousElementSibling;
    const caption = prev && !prev.querySelector("input, select, textarea, button") && text(prev) === text(label) ? prev : null;
    if (legendShown || caption) out.push({ kind: "label-doubled", detail: `"${text(label)}" twice at ${name(control)}` });
  }
  for (let i = 0; i < controls.length; i += 1) {
    const a = controls[i].getBoundingClientRect();
    for (let j = i + 1; j < controls.length; j += 1) {
      if (controls[i].contains(controls[j]) || controls[j].contains(controls[i])) continue;
      // A drawer / dialog / popover field over a page field is layering, not a toolbar collision.
      if (layer(controls[i]) !== layer(controls[j])) continue;
      const b = controls[j].getBoundingClientRect();
      const w = Math.min(a.right, b.right) - Math.max(a.left, b.left);
      const h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
      if (w > 2 && h > 2) out.push({ kind: "control-overlap", detail: `${name(controls[i])} overlaps ${name(controls[j])} by ${Math.round(w)}x${Math.round(h)}px` });
    }
  }
  for (const pager of [...document.querySelectorAll(".MuiTablePagination-root, .MuiPagination-root")].filter(shown)) {
    if (pager.scrollWidth > pager.clientWidth + 1 && getComputedStyle(pager).overflowX !== "visible") {
      out.push({ kind: "pager-clipped", detail: `${name(pager)} scrolls sideways (${pager.scrollWidth} > ${pager.clientWidth})` });
      continue;
    }
    const box = (pager.closest(".MuiCard-root, .MuiPaper-root") || pager).getBoundingClientRect();
    for (const btn of pager.querySelectorAll("button, a")) {
      if (!shown(btn)) continue;
      const r = btn.getBoundingClientRect();
      if (r.left < box.left - 1 || r.right > box.right + 1 || r.width < 20) {
        out.push({ kind: "pager-clipped", detail: `${btn.getAttribute("aria-label") || text(btn) || name(btn)} at ${Math.round(r.left)}-${Math.round(r.right)} outside ${Math.round(box.left)}-${Math.round(box.right)}` });
        break;
      }
    }
  }
  return out.slice(0, 6);
}

const LABELS = {
  "label-doubled": "Field label rendered twice",
  "control-overlap": "Toolbar controls overlap",
  "pager-clipped": "Pager arrows clipped or pager scrolls sideways",
};

export default {
  name: "controls",
  p0: true,
  async run(page) {
    const found = await page.evaluate(probeControls);
    return found.map((f) => ({ pattern: f.kind, label: LABELS[f.kind], detail: f.detail }));
  },
};
