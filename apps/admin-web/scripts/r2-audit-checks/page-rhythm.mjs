// r2-visual-audit plugin: page rhythm + raw codes (R3OPS).
// - header-gap: the page header (PageHeader = template CustomBreadcrumbs, [data-page-header]) must
//   have the page gap (>= 16px) before the first block under it. A page whose root is a fragment
//   instead of the `screen on` grid glued its summary card to the breadcrumbs (/verify).
// - raw-code-label: a template Label / Chip must never show a snake_case backend code
//   ("Not_started", "waiting_for_accepted_completion"); run it through humanizeEnum / optionLabel.

/** In-page probe (serialisable). Returns [{ kind, detail }]. */
export function probePageRhythm() {
  const out = [];
  const visible = (el) => {
    const r = el.getBoundingClientRect();
    const s = getComputedStyle(el);
    return r.width > 0 && r.height > 0 && s.visibility !== "hidden" && s.display !== "none" && s.position !== "fixed" && s.position !== "absolute";
  };
  const header = document.querySelector("[data-page-header]");
  if (header && visible(header)) {
    // The header may sit alone in a wrapper (<div><PageHeader/></div> in a flex column): then the
    // first block is the WRAPPER's next sibling. Walk up while the header is its wrapper's only
    // visible content (the /configuration/work-instructions SOP library read 0px and slipped past).
    let anchor = header;
    let next = null;
    for (let depth = 0; anchor && depth < 4; depth += 1) {
      next = anchor.nextElementSibling;
      while (next && !visible(next)) next = next.nextElementSibling;
      if (next || !anchor.parentElement || anchor.parentElement.matches("main, body, .screen")) break;
      anchor = anchor.parentElement;
    }
    if (next) {
      const gap = Math.round(next.getBoundingClientRect().top - header.getBoundingClientRect().bottom);
      if (gap < 16) out.push({ kind: "header-gap", detail: `${gap}px between the page header and ${next.tagName.toLowerCase()}.${String(next.className).split(" ").slice(0, 2).join(".")}` });
    }
  }
  const seen = new Set();
  for (const el of document.querySelectorAll(".minimal__label__root, .MuiChip-label")) {
    const text = (el.textContent || "").trim();
    if (!visible(el) || seen.has(text)) continue;
    if (/^[A-Za-z]+(_[A-Za-z0-9]+)+$/.test(text)) {
      seen.add(text);
      out.push({ kind: "raw-code-label", detail: `"${text}"` });
    }
  }
  return out;
}

export default {
  name: "rhythm",
  p0: true,
  async run(page) {
    const found = await page.evaluate(probePageRhythm);
    return found.map((f) =>
      f.kind === "header-gap"
        ? { pattern: "header-gap", label: "Page header touches the first block (no page gap)", detail: f.detail }
        : { pattern: "raw-code-label", label: "Label shows a raw snake_case backend code", detail: f.detail },
    );
  },
};
