// drawer-template.mjs — right drawers must be the template's temporary Drawer (Ravi R2-4).
//
// Imported by scripts/check-design-system.mjs (`npm run design:guard`, check "drawer-off-template").
// A right drawer is the MUI Minimal template drawer shell (components/app/drawer MinimalDrawer,
// or components/app/detail-drawer DetailDrawer on top of it): portalled temporary MUI Drawer,
// anchor right, a visible backdrop, a template paper width, sticky header (title + close),
// Scrollbar body, footer actions. Wide content scrolls inside its own Scrollbar (DrawerTableScroll).
//
// Findings:
//   raw MUI <Drawer> in feature/app code (anchor right or unspecified) — bypasses the template shell
//   a transparent backdrop (`invisibleBackdrop`, `backdrop: { invisible: true }`) on a drawer
//   a MinimalDrawer / DetailDrawer width that is not a template drawer width
//   a legacy hand-rolled drawer (`<aside className="drawer …">`, `schedule-side-drawer`, an <aside>
//   acting as a dialog)
//
// It also tells the fixed-px-width check which `width={480}`-style literals are template drawer
// widths on a drawer element (allowed) rather than a page element that cannot fit a phone.

// Template right-drawer paper widths (next-ts src): 320 calendar-filters / account-drawer /
// file-manager-file-details / product+job+tour filters, 360 settings-drawer, 420
// notifications-drawer, 480 kanban-details (`{ xs: 1, sm: 480 }`).
export const TEMPLATE_DRAWER_WIDTHS = new Set([320, 360, 420, 480]);

// The template drawer shells themselves (and their stories) may render the raw MUI Drawer. The kanban
// details shell (template sections/kanban/details anatomy, moved out of components/minimal because it
// is adapted) is one; it keeps the visible backdrop like every other right drawer.
const SHELL_OWNERS = [/^components\/minimal\//, /^layouts\/template\//, /^stories\//, /^components\/app\/kanban\/kanban-details\.tsx$/, /^components\/app\/drawer\//];
const isShellOwner = (rel) => SHELL_OWNERS.some((re) => re.test(rel));

const DRAWER_OPEN = /<(MinimalDrawer|DetailDrawer|Drawer|SwipeableDrawer)\b/g;

/** [{ tag, start, end, text, startLine, endLine }] for every drawer opening tag in `text`. */
export function drawerOpeningTags(text) {
  const tags = [];
  for (const m of text.matchAll(DRAWER_OPEN)) {
    let depth = 0;
    let quote = "";
    let i = m.index + m[0].length;
    for (; i < text.length; i += 1) {
      const ch = text[i];
      if (quote) {
        if (ch === quote && text[i - 1] !== "\\") quote = "";
        continue;
      }
      // Comments inside prop expressions (`{/* the sale's … */}`) are not code: skip them whole.
      if (ch === "/" && text[i + 1] === "*") {
        const endC = text.indexOf("*/", i + 2);
        i = endC < 0 ? text.length : endC + 1;
        continue;
      }
      if (ch === "/" && text[i + 1] === "/" && depth > 0) {
        const endL = text.indexOf("\n", i);
        i = endL < 0 ? text.length : endL;
        continue;
      }
      // Inside an expression an apostrophe is usually JSX text ("don't"), not a string opener.
      if (ch === '"' || ch === "`" || (ch === "'" && depth === 0)) quote = ch;
      else if (ch === "{") depth += 1;
      else if (ch === "}") depth -= 1;
      else if (ch === ">" && depth === 0 && text[i - 1] !== "=") break;
    }
    const lineOf = (pos) => text.slice(0, pos).split("\n").length;
    tags.push({ tag: m[1], start: m.index, end: i, text: text.slice(m.index, i + 1), startLine: lineOf(m.index), endLine: lineOf(i) });
  }
  return tags;
}

/** Line numbers (1-based) that sit inside a drawer opening tag. */
export function drawerTagLines(text) {
  const lines = new Set();
  for (const t of drawerOpeningTags(text)) for (let l = t.startLine; l <= t.endLine; l += 1) lines.add(l);
  return lines;
}

/** True when every px width literal on `code` is a template drawer width. */
export function onlyTemplateDrawerWidths(code) {
  const nums = [...code.matchAll(/\b(?:width|minWidth|min-width)\s*[:=]\s*["'{]?\s*(\d{3,4})(?:px)?/g)].map((m) => Number(m[1]));
  return nums.length > 0 && nums.every((n) => TEMPLATE_DRAWER_WIDTHS.has(n));
}

function constValue(text, name) {
  const m = new RegExp(`\\bconst\\s+${name}\\s*=\\s*(\\d+)\\b`).exec(text);
  return m ? Number(m[1]) : undefined;
}

export function drawerTemplateFindings(text, rel) {
  if (isShellOwner(rel) || !/\.tsx?$/.test(rel)) return [];
  const out = [];
  const lines = text.split("\n");
  const push = (line, why) => out.push({ line, snippet: (lines[line - 1] ?? "").trim(), why });

  for (const t of drawerOpeningTags(text)) {
    if (t.tag === "Drawer" || t.tag === "SwipeableDrawer") {
      const anchor = /\banchor=["'{]\s*["']?(left|right|top|bottom)/.exec(t.text)?.[1];
      // A left nav drawer is the template nav-mobile pattern, not a right detail/filter drawer.
      if (anchor !== "left") push(t.startLine, "raw MUI Drawer; render the template MinimalDrawer / DetailDrawer");
    }
    if (/\binvisibleBackdrop\b|invisible\s*:\s*true/.test(t.text)) push(t.startLine, "drawer without a visible backdrop");
    if (t.tag === "MinimalDrawer") {
      const w = /\bwidth=\{\s*([A-Za-z_]\w*|\d+)\s*\}/.exec(t.text)?.[1];
      if (w !== undefined) {
        const n = /^\d+$/.test(w) ? Number(w) : constValue(text, w);
        if (n !== undefined && !TEMPLATE_DRAWER_WIDTHS.has(n)) push(t.startLine, `drawer width ${n} is not a template drawer width (320/360/420/480)`);
      }
    }
    if (t.tag === "DetailDrawer") {
      const size = /\bsize=["']([a-z]+)["']/.exec(t.text)?.[1];
      if (size && size !== "sm" && size !== "md") push(t.startLine, `DetailDrawer size "${size}" is not a template drawer width`);
    }
  }
  // `invisibleBackdrop` passed through a spread or a variable still ends up on the drawer.
  lines.forEach((line, i) => {
    if (/^\s*(?:\/\/|\*)/.test(line)) return;
    if (/\binvisibleBackdrop\s*(?:=|:)\s*(?:\{?\s*true|true)/.test(line)) push(i + 1, "drawer without a visible backdrop");
  });
  // Hand-rolled drawer shells: an <aside> (opening tag may span lines) carrying the legacy
  // `drawer` / `schedule-side-drawer` class or acting as a dialog.
  for (const m of text.matchAll(/<aside\b(?:[^>{]|\{[^}]*\})*>/g)) {
    const tag = m[0];
    const cls = /className=\{?["'`]([^"'`]*)/.exec(tag)?.[1] ?? "";
    const line = text.slice(0, m.index).split("\n").length;
    if (/(?:^|\s)(?:drawer|schedule-side-drawer)(?:\s|\$|$)/.test(cls)) push(line, "legacy hand-rolled drawer (<aside class=drawer>); render the template MinimalDrawer / DetailDrawer");
    else if (/\brole=["'](?:dialog|alertdialog)["']|\baria-modal=/.test(tag)) push(line, "legacy hand-rolled drawer (<aside role=dialog>); render the template MinimalDrawer / DetailDrawer");
  }
  // One finding per line.
  const seen = new Set();
  return out.filter((f) => (seen.has(f.line) ? false : (seen.add(f.line), true)));
}
