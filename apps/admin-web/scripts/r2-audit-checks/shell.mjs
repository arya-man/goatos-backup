// r2-audit-checks/shell.mjs — the SHELL on every route, measured against the MUI Minimal template
// (INTEGRATOR, 2026-09-27). Plugged into scripts/r2-visual-audit.mjs (every scan job, all profiles),
// so the visual gate fails when a push breaks the chrome Ravi sees on every page.
//
// Checks (all P0):
//   shell|stylesheet-failed      a <link rel=stylesheet> did not load (404 / swapped build). This is
//                                what a `next build` into a live server's .next looks like: UA
//                                defaults everywhere (40px list indent, grey `buttonface` buttons,
//                                blue underlined links) while every component is "unchanged".
//   shell|nav-item-left          sidebar root items / subheaders not flush in the template column:
//                                NavSectionVertical px:2 (16px) + item/subheader padding-left 12px,
//                                so every root row starts at nav.left + 16 and its text/icon at +28,
//                                the logo column.
//   shell|nav-item-padding       root item / subheader padding-left != 12px (template --nav-item-pl).
//   shell|nav-active-opaque      active root item painted with an opaque block instead of the
//                                template's translucent primary tint (alpha <= 0.24).
//   shell|header-button-bg       a header icon button has a resting background or border (template
//                                header IconButtons are transparent; hover only).
//   shell|tabs-overlap           a filter control / its floating label overlaps a Tabs strip
//                                (the template list toolbar sits below the tabs with its own padding).
//   shell|controls-overlap       two filter controls in the content column overlap each other.
//   shell|sort-header-link       a table sort header renders as a link (underline or link blue):
//                                template headers are TableSortLabel in the header text colour.
//
// Pure helpers are exported for scripts/r2-audit-checks/shell.test.mjs.

export const TEMPLATE_SHELL = Object.freeze({
  navPadX: 16, // NavSectionVertical sx={{ px: 2 }}
  itemPadLeft: 12, // --nav-item-pl
  subheaderPadLeft: 12, // NavSubheader padding theme.spacing(2, 1, 1, 1.5)
  activeMaxAlpha: 0.24, // varAlpha(primary.mainChannel, 0.08) (+ hover 0.16)
  tolerance: 1.5,
});

const px = (v) => (v == null ? NaN : parseFloat(v));
const round = (n) => Math.round(n * 10) / 10;

export function parseRgba(s) {
  const m = /^rgba?\(([^)]+)\)$/.exec(String(s || "").trim());
  if (!m) return null;
  const p = m[1].split(/[\s,/]+/).filter(Boolean).map(Number);
  return [p[0], p[1], p[2], p.length > 3 ? p[3] : 1];
}

export function isLinkColour(rgb) {
  const c = parseRgba(rgb);
  if (!c) return false;
  const [r, g, b] = c;
  // UA link blue (#0000EE light, #9E9EFF dark) and visited purple; Mesha info (cyan) and all
  // neutrals stay out (their blue does not dominate green by 50+).
  return b - r >= 40 && b - g >= 50;
}

export function intersects(a, b, min = 2) {
  const w = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x);
  const h = Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y);
  return w > min && h > min ? { w: round(w), h: round(h) } : null;
}

/** Turn a page measurement (see measureShell) into findings. */
export function evaluateShell(m, t = TEMPLATE_SHELL) {
  const out = [];
  const add = (pattern, label, detail) => out.push({ pattern, label, detail, p0: true });
  for (const s of m.failedSheets || []) add("stylesheet-failed", "Shell: stylesheet failed to load (UA defaults on screen)", s);
  if (m.nav) {
    const col = m.nav.left + t.navPadX;
    for (const it of m.nav.items) {
      if (Math.abs(it.x - col) > t.tolerance) add("nav-item-left", `Shell: sidebar ${it.kind} not flush in the template column (nav.left + ${t.navPadX}px)`, `"${it.text}" ${it.kind} at x=${it.x}, template x=${col} (${it.x > col ? "indented" : "outdented"} ${round(Math.abs(it.x - col))}px)`);
      const want = it.kind === "subheader" ? t.subheaderPadLeft : t.itemPadLeft;
      if (Math.abs(it.pl - want) > t.tolerance) add("nav-item-padding", `Shell: sidebar ${it.kind} padding-left != template ${want}px`, `"${it.text}" padding-left ${it.pl}px`);
      if (it.active && it.bgAlpha > t.activeMaxAlpha) add("nav-active-opaque", "Shell: active sidebar item is an opaque block (template: translucent primary tint)", `"${it.text}" background ${it.bg}`);
    }
  }
  for (const b of m.headerButtons || []) {
    if (b.bgAlpha > 0.02 || b.border > 0) add("header-button-bg", "Shell: header icon button has a background/border (template IconButton is transparent)", `${b.label || b.sig}: background ${b.bg}${b.border > 0 ? `, border ${b.border}px` : ""}`);
  }
  for (const o of m.tabsOverlaps || []) add("tabs-overlap", "Shell: filter control overlaps the Tabs strip", `${o.what} overlaps tabs by ${o.h}px (${o.detail})`);
  for (const o of m.controlOverlaps || []) add("controls-overlap", "Shell: filter controls overlap each other", `${o.a} x ${o.b}: ${o.w}x${o.h}px`);
  for (const s of m.sortHeaders || []) {
    if (s.underline || isLinkColour(s.color)) add("sort-header-link", "Shell: table sort header renders as a link (template TableSortLabel)", `"${s.text}" colour ${s.color}${s.underline ? ", underlined" : ""} (${s.sig})`);
  }
  return out;
}

/** Runs in the page. Self-contained. */
export function measureShell() {
  const rgba = (s) => { const m = /^rgba?\(([^)]+)\)$/.exec(s || ""); if (!m) return 0; const p = m[1].split(/[\s,/]+/).filter(Boolean).map(Number); return p.length > 3 ? p[3] : 1; };
  const vis = (el) => { const r = el.getBoundingClientRect(); if (r.width < 1 || r.height < 1) return false; try { return el.checkVisibility({ opacityProperty: true, visibilityProperty: true }); } catch { return true; } };
  const rect = (el) => { const r = el.getBoundingClientRect(); return { x: Math.round(r.left * 10) / 10, y: Math.round(r.top * 10) / 10, w: Math.round(r.width * 10) / 10, h: Math.round(r.height * 10) / 10 }; };
  const txt = (el) => (el.innerText || el.getAttribute("aria-label") || "").trim().replace(/\s+/g, " ").slice(0, 40);
  const sig = (el) => el.tagName.toLowerCase() + [...el.classList].filter((c) => !/^css-/.test(c)).slice(0, 3).map((c) => "." + c).join("");
  const out = { failedSheets: [], nav: null, headerButtons: [], tabsOverlaps: [], controlOverlaps: [], sortHeaders: [] };

  for (const l of document.querySelectorAll("link[rel=stylesheet]")) {
    let ok = !!l.sheet;
    if (ok) { try { void l.sheet.cssRules.length; } catch { /* cross-origin sheet: loaded */ } }
    if (!ok) out.failedSheets.push((l.getAttribute("href") || "").replace(/^.*\/_next\//, "_next/"));
  }
  // a 404 stylesheet still gets an (empty) CSSStyleSheet; the resource timing entry has the status
  for (const e of performance.getEntriesByType("resource")) {
    if (/\.css(\?|$)/.test(e.name) && e.responseStatus >= 400) {
      const n = e.name.replace(/^.*\/_next\//, "_next/");
      if (!out.failedSheets.includes(n)) out.failedSheets.push(`${n} (HTTP ${e.responseStatus})`);
    }
  }

  const navRoot = [...document.querySelectorAll(".minimal__nav__section__vertical")].find((n) => vis(n) && n.getBoundingClientRect().width > 200 && !n.closest(".MuiDrawer-root"));
  if (navRoot) {
    const nr = navRoot.getBoundingClientRect();
    const items = [];
    for (const sh of navRoot.querySelectorAll(".minimal__nav__subheader")) {
      if (!vis(sh)) continue;
      const cs = getComputedStyle(sh);
      items.push({ kind: "subheader", text: txt(sh), x: rect(sh).x, pl: parseFloat(cs.paddingLeft), active: false, bgAlpha: 0 });
    }
    for (const it of navRoot.querySelectorAll(".minimal__nav__item__root")) {
      if (!vis(it)) continue;
      // sub items live in the Collapse that follows their parent item; group Collapses follow a subheader
      const coll = it.closest(".MuiCollapse-root");
      if (coll && coll.previousElementSibling && coll.previousElementSibling.matches(".minimal__nav__item__root")) continue;
      const cs = getComputedStyle(it);
      items.push({ kind: "item", text: txt(it), x: rect(it).x, pl: parseFloat(cs.paddingLeft), active: /--active/.test(it.className) || it.getAttribute("aria-current") === "page", bg: cs.backgroundColor, bgAlpha: rgba(cs.backgroundColor) });
    }
    out.nav = { left: Math.round(nr.left * 10) / 10, items };
  }

  const header = document.querySelector("header.minimal__layout__header__root, .minimal__layout__header__root, header.MuiAppBar-root");
  if (header) {
    for (const b of header.querySelectorAll("button, a.MuiIconButton-root, [role=button]")) {
      // the account avatar, contained/outlined actions and the Ask Mesha brand launcher (goat mark in
      // #topbar-ai-slot) are deliberately filled; every other header control is a template IconButton
      if (!vis(b) || b.querySelector(".MuiAvatar-root, img") || b.closest(".MuiAvatar-root, #topbar-ai-slot, .topbar-ai-slot, .ceo-ai") || b.matches(".MuiButton-contained, .MuiButton-outlined, .MuiButton-soft, .MuiFab-root")) continue;
      if (b.parentElement && b.parentElement.closest("button, [role=button]")) continue;
      const cs = getComputedStyle(b);
      const border = ["Top", "Right", "Bottom", "Left"].reduce((m2, s) => Math.max(m2, cs[`border${s}Style`] !== "none" && cs[`border${s}Style`] !== "hidden" ? parseFloat(cs[`border${s}Width`]) || 0 : 0), 0);
      out.headerButtons.push({ label: txt(b), sig: sig(b), bg: cs.backgroundColor, bgAlpha: rgba(cs.backgroundColor), border });
    }
  }

  const root = document.querySelector(".minimal__layout__main__content, main") || document.body;
  const tabStrips = [...root.querySelectorAll(".MuiTabs-root, [role=tablist]")].filter((t) => vis(t) && !t.parentElement.closest(".MuiTabs-root") && !t.closest("[role=dialog], .MuiDrawer-root, .MuiPopover-root"));
  const CONTROL = ".MuiFormControl-root, .MuiInputLabel-root, .MuiFormLabel-root, .MuiChip-root, .MuiButton-root, .MuiToggleButtonGroup-root, input:not([type=hidden]), select";
  const controls = [...root.querySelectorAll(CONTROL)].filter((c) => vis(c) && !c.closest(".MuiTabs-root, [role=tablist], table, [role=dialog], .MuiDrawer-root, .MuiPopover-root, .MuiDialog-root, nav"));
  for (const t of tabStrips) {
    const tr = rect(t);
    for (const c of controls) {
      const cr = rect(c);
      const w = Math.min(tr.x + tr.w, cr.x + cr.w) - Math.max(tr.x, cr.x);
      const h = Math.min(tr.y + tr.h, cr.y + cr.h) - Math.max(tr.y, cr.y);
      if (w > 2 && h > 2) out.tabsOverlaps.push({ what: `${sig(c)} "${txt(c)}"`, h: Math.round(h * 10) / 10, detail: `control ${cr.w}x${cr.h}@${cr.x},${cr.y} vs tabs ${tr.w}x${tr.h}@${tr.x},${tr.y}` });
    }
  }
  const boxes = controls.filter((c) => c.matches(".MuiFormControl-root") && !c.parentElement.closest(".MuiFormControl-root"));
  for (let i = 0; i < boxes.length; i++) for (let j = i + 1; j < boxes.length; j++) {
    const a = rect(boxes[i]), b = rect(boxes[j]);
    const w = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x);
    const h = Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y);
    if (w > 2 && h > 2) out.controlOverlaps.push({ a: `${sig(boxes[i])} "${txt(boxes[i])}"`, b: `${sig(boxes[j])} "${txt(boxes[j])}"`, w: Math.round(w), h: Math.round(h) });
  }

  for (const s of root.querySelectorAll("thead .MuiTableSortLabel-root, thead th a, [role=columnheader] a, thead th [role=button]")) {
    if (!vis(s)) continue;
    const cs = getComputedStyle(s);
    out.sortHeaders.push({ text: txt(s), color: cs.color, underline: /underline/.test(cs.textDecorationLine), sig: sig(s) });
  }
  return out;
}

export default {
  name: "shell",
  p0: true,
  async run(page) {
    const m = await page.evaluate(measureShell);
    return evaluateShell(m);
  },
};
