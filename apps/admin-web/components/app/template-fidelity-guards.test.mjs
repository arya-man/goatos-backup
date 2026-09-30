import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { test } from "node:test";

// Template-fidelity guards (AFIX12, audit P0/P1 classes on PR #294). Each test is one rule id; the
// rule text lives in apps/admin-web/AGENTS.md and the design-system / frontend-anti-patterns /
// goatos-code-review skills. They run in `npm test`, so local CI and the PR gate carry them.

const ROOT = new URL("../../", import.meta.url).pathname;
const read = (path) => readFileSync(join(ROOT, path), "utf8");

function walk(dir, out = []) {
  for (const name of readdirSync(join(ROOT, dir))) {
    if (name === "node_modules" || name.startsWith(".")) continue;
    const path = join(dir, name);
    if (statSync(join(ROOT, path)).isDirectory()) walk(path, out);
    else out.push(path);
  }
  return out;
}

const SOURCE_DIRS = ["app", "components", "features", "layouts"];
const tsx = SOURCE_DIRS.flatMap((dir) => walk(dir)).filter((path) => /\.tsx?$/.test(path) && !/\.(test|stories)\./.test(path));
const productTsx = tsx.filter((path) => !path.startsWith("components/minimal/"));
const css = SOURCE_DIRS.flatMap((dir) => walk(dir)).filter((path) => path.endsWith(".css"));
const lineOf = (source, index) => source.slice(0, index).split("\n").length;

// JSX opening tag of a component, tolerant of `=>` and one level of `{...}` in props.
const openingTags = (source, name) => [...source.matchAll(new RegExp(`<${name}\\b((?:[^<>{}]|=>|\\{(?:[^{}]|\\{[^{}]*\\})*\\})*?)/?>`, "gs"))];
// The tag's OWN props: nested `{...}` expressions (icons, render props) blanked out.
const ownProps = (props) => props.replace(/\{(?:[^{}]|\{(?:[^{}]|\{[^{}]*\})*\})*\}/g, "{}");

test("guard: brand-primary-contained -- a contained Button always names its colour", () => {
  // The theme's Button default colour is the template's `inherit` (near-black in light, white in
  // dark). A contained primary action without color= renders off-brand next to green primaries
  // (Sign in, Save password, Add disease, Add city ...: AUDIT1 P0-3). Locked Mesha palette.
  const offenders = [];
  for (const path of productTsx) {
    const source = read(path);
    for (const name of ["Button", "MuiButton"]) {
      for (const match of openingTags(source, name)) {
        const props = ownProps(match[1]);
        if (/variant="contained"/.test(props) && !/\bcolor=/.test(props)) offenders.push(`${path}:${lineOf(source, match.index)}`);
      }
    }
  }
  assert.deepEqual(offenders, [], `contained Buttons need color="primary" (or an explicit error/inherit):\n${offenders.join("\n")}`);
});

test("guard: mesha-logo-mark -- the logo tile is the मे mark and global-error follows the user's theme", () => {
  const logo = read("layouts/app/logo/logo.tsx");
  assert.match(logo, /MESHA_LOGO_TEXT = 'मे'/, "Logo default text is the shell's मे mark (top_bar.logo_text)");
  assert.match(logo, /text = MESHA_LOGO_TEXT/);
  const offenders = productTsx.filter((path) => /logoText="M"|<Logo[^>]*text="M"/.test(read(path)));
  assert.deepEqual(offenders, [], `no "M" logo override: ${offenders.join(", ")}`);
  const globalError = read("app/global-error.tsx");
  assert.doesNotMatch(globalError, /className="dark"|data-theme="dark"/, "global-error never forces dark");
  assert.match(globalError, /THEME_BOOT_SCRIPT/, "global-error applies the stored theme before paint");
  assert.match(globalError, /@\/theme\/fonts\.css/, "global-error loads the app font");
});

test("guard: form-submit-respects-field-guard -- a submit a field already refused is never posted", () => {
  // ThemedDatePicker (required) refuses a submit with preventDefault from its own native listener.
  // A React onSubmit that then preventDefault()s and posts anyway reached the server action without
  // the field and crashed the page (sale drawer Add payment, AUDIT2 P0-2).
  const offenders = [];
  for (const path of productTsx) {
    const source = read(path);
    if (!/<ThemedDatePicker[^>]*\brequired\b/s.test(source)) continue;
    for (const match of source.matchAll(/onSubmit\s*=\s*\(event[^)]*\)\s*=>\s*\{([\s\S]*?)\n\s*\};?/g)) {
      const body = match[1];
      if (/event\.preventDefault\(\)/.test(body) && /formAction|startTransition/.test(body) && !/event\.defaultPrevented/.test(body.split("event.preventDefault()")[0])) {
        offenders.push(`${path}:${lineOf(source, match.index)}`);
      }
    }
  }
  assert.deepEqual(offenders, [], `check event.defaultPrevented before posting:\n${offenders.join("\n")}`);
});

test("guard: refusal-actions-never-throw -- a server action that returns a refusal does not throw on a blank field", () => {
  // An action typed to return `...Error | undefined` promises the form a refusal in place.
  // requiredString throws, which the page turns into "Something went wrong" (AUDIT2 P0-2). Only
  // hidden identity fields (…_id) and the idempotency key may use it there.
  const offenders = [];
  for (const path of productTsx.filter((file) => /^\s*["']use server["']/.test(read(file)))) {
    const source = read(path);
    for (const match of source.matchAll(/export async function (\w+)\([^)]*\): Promise<[^>]*Error \| undefined>\s*\{([\s\S]*?)\n\}/g)) {
      for (const call of match[2].matchAll(/requiredString\(formData, "([^"]+)"\)/g)) {
        if (!/_id$/.test(call[1])) offenders.push(`${path} ${match[1]}(${call[1]})`);
      }
    }
  }
  assert.deepEqual(offenders, [], `return a refusal for a blank user field instead of requiredString:\n${offenders.join("\n")}`);
});

test("guard: breakpoint-display-in-sx -- a DataTable is never hidden by a stylesheet class", () => {
  // DataTable's emotion styles beat a `.x{display:none}` media rule, so the phone got the cards AND
  // the table (/feed/config ration grid, AUDIT1 P0-6). Switch with sx display breakpoints.
  const classes = new Set();
  for (const path of productTsx) {
    for (const match of openingTags(read(path), "DataTable")) {
      for (const cls of (ownProps(match[1]).match(/className="([^"]+)"/)?.[1] ?? "").split(/\s+/)) if (cls) classes.add(cls);
    }
  }
  const offenders = [];
  for (const path of css) {
    const source = read(path);
    for (const cls of classes) {
      if (new RegExp(`\\.${cls.replace(/[-]/g, "\\-")}\\{display:none`).test(source.replace(/\s+/g, ""))) offenders.push(`${path}: .${cls}`);
    }
  }
  assert.deepEqual(offenders, [], `hide/show a DataTable with sx display breakpoints:\n${offenders.join("\n")}`);
});

test("guard: css-token-defined -- every var(--token) a component reads is defined", () => {
  // P4 deleted --input-h-sm / --lh-caption / --chip-h while load-detail and the work board still
  // read them, silently dropping heights and line heights (AUDIT2 P1-5).
  const defined = new Set();
  for (const path of css) for (const match of read(path).matchAll(/(--[\w-]+)\s*:/g)) defined.add(match[1]);
  // MUI CSS-variables theme (cssVarPrefix '') and template runtime vars.
  const generated = /^--(palette|shape|spacing|shadows|customShadows|font|typography|zIndex|opacity|transitions|mui|layout|nav|kanban|header|scrollbar|toolbar|overlay|Paper|Tooltip|Alert|AppBar|Avatar|Button|Chip|FilledInput|LinearProgress|Skeleton|Slider|SnackbarContent|StepConnector|StepContent|Switch|TableCell)\b/;
  const offenders = new Set();
  for (const path of productTsx) {
    const source = read(path).replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
    for (const match of source.matchAll(/var\((--[\w-]+)\s*(,)?/g)) {
      const [, name, fallback] = match;
      if (fallback || defined.has(name) || generated.test(name)) continue;
      // Locally declared custom properties (sx / style objects) count as defined.
      if (new RegExp(`["'\`]${name}["'\`](?: as \\w+)?\\]?\\s*:`).test(source)) continue;
      offenders.add(`${path}: ${name}`);
    }
  }
  assert.deepEqual([...offenders], [], `undefined CSS token (define it, give a fallback, or use a theme value):\n${[...offenders].join("\n")}`);
});

test("guard: chart-ramp-distinct -- the categorical ramp has no repeated channel and no at-risk red", () => {
  const source = read("components/app/chart-colors.ts");
  const ramp = [...source.match(/const RAMP: ChartColorKey\[\] = \[(.*)\];/)[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);
  assert.ok(ramp.length >= 7);
  assert.equal(ramp.filter((item) => /^error/.test(item)).length, 0, "no error red in the categorical ramp (invariant 1d06f72d0)");
  assert.equal(new Set(ramp).size, ramp.length, `one slot per channel: ${ramp.join(", ")}`);
  // The first four slots are four different hues.
  assert.equal(new Set(ramp.slice(0, 4).map((k) => k.split(".")[0])).size, 4);
});


test("guard: page-header-action-slot -- nothing restyles PageHeader's layout from a stylesheet", () => {
  // `.lt-phead{align-items:flex-start}` shrank the breadcrumbs row so /tasks "New task" hugged the
  // title instead of sitting in the right action slot (AUDIT1 P1-12).
  const classes = new Set();
  for (const path of productTsx) for (const match of openingTags(read(path), "PageHeader")) for (const cls of (ownProps(match[1]).match(/className="([^"]+)"/)?.[1] ?? "").split(/\s+/)) if (cls) classes.add(cls);
  const offenders = [];
  for (const path of css) {
    const source = read(path).replace(/\s+/g, "");
    for (const cls of classes) if (new RegExp(`\\.${cls}\\{[^}]*(align-items|display|flex|grid)`).test(source)) offenders.push(`${path}: .${cls}`);
  }
  assert.deepEqual(offenders, [], `PageHeader layout is the template's; drop the class rules:\n${offenders.join("\n")}`);
});

test("guard: no-card-in-card -- stacked phone rows are divider rows, never bordered cards inside a card", () => {
  // /vaccination/plan and /herd-signals at 390 drew each table row as its own bordered, rounded,
  // tinted box inside the table card (AUDIT1 P1-14).
  const offenders = [];
  for (const path of css) {
    const source = read(path);
    for (const match of source.matchAll(/([^{}]*\btr)\s*\{([^}]*)\}/g)) {
      const body = match[2].replace(/\s+/g, "");
      if (/border:1px/.test(body) && /border-radius:/.test(body)) offenders.push(`${path}:${lineOf(source, match.index)} ${match[1].trim().slice(-60)}`);
    }
  }
  // Pre-existing offenders (ratchet: this list only shrinks; follow-up to convert each like
  // .vplan / .herd-signals-page): verify board, toxin review, vendors, full schedule.
  const KNOWN = ["toxin-review-table", "procurement-vendors-table", "full-vaccine-schedule-table"];
  const fresh = offenders.filter((line) => !KNOWN.some((cls) => line.includes(`table.${cls} tr`)));
  assert.deepEqual(fresh, [], `phone table rows: border-bottom divider only:\n${fresh.join("\n")}`);
  // sx half (R3SP 2026-09-27: /procurement/animal-purchases + /source-entry phone loads drew each
  // row as a bordered rounded card inside the Loads card via procurement-sx.ts phoneLoadCardsSx).
  const sxFiles = ["features/procurement/procurement-sx.ts"];
  for (const path of sxFiles) {
    const source = read(path);
    for (const match of source.matchAll(/\btr`\]:\s*\{([^}]*)\}/g)) {
      assert.ok(!(/border:\s*"1px/.test(match[1]) && /borderRadius/.test(match[1])), `${path}: phone table rows are divider rows, not bordered cards`);
    }
  }
  const board = read("features/preventive-care-vaccination/command-board-view.tsx");
  const kpiAt = board.indexOf("{/* KPI deck");
  assert.ok(kpiAt > 0, "the vaccination KPI deck marker is present");
  const lastCardOpen = board.lastIndexOf("<Card", kpiAt);
  assert.ok(board.lastIndexOf("</Card>", kpiAt) > lastCardOpen, "the vaccination KPI deck sits outside the Command Board card");
});

test("guard: template-filter-toolbar -- filter bars use MUI Chips and one rows-per-page", () => {
  // Care Coverage and the live tracker drew raw .achip pills with a <b>× button, icons dangling
  // outside the selects, and /counts/herd had a second rows-per-page select beside the pager
  // (AUDIT1 P1-9, P1-15, P1-18).
  for (const path of ["features/vaccination-care-coverage/care-coverage-filters.tsx", "features/vaccination-live-tracker/live-tracker-filters.tsx", "features/counts/herd-filters-modal-client.tsx"]) {
    const source = read(path);
    assert.doesNotMatch(source, /className="(achip|lt-fbar|lt-fsel|tsearch|btn)[" ]/, `${path}: template toolbar only`);
    assert.doesNotMatch(source, /rows_per_page/, `${path}: rows per page lives in the table pager`);
  }
  assert.doesNotMatch(read("features/vaccination-live-tracker/live-poller.tsx"), /lt-intervalpick/, "refresh interval is a ToggleButtonGroup");
});

test("guard: routine-drawer-template -- the routine drawer renders only MUI form parts", () => {
  const drawer = read("features/pen-routines/routine-drawer.tsx");
  assert.doesNotMatch(drawer, /<textarea|<details|className="(fld|note)"|className=\{?["`]prt-/, "no raw textarea/details or legacy .fld/.note/.prt-* markup");
  assert.doesNotMatch(read("app/mesha-theme.css"), /\.prt-(step|tile|pill|question|foot|row2|pens)\b/, "legacy .prt-* drawer rules stay deleted");
});

test("guard: dark-alert-tint -- dark standard Alerts are a tint, not a filled block", () => {
  const alert = read("theme/core/components/alert.tsx");
  const standard = alert.slice(alert.indexOf("const standardVariants"), alert.indexOf("const filledVariants"));
  const dark = standard.slice(standard.indexOf("applyStyles('dark'"));
  assert.doesNotMatch(dark, /backgroundColor: theme\.vars\.palette\[colorKey\]\.darker/, "the locked dark palette's darker steps are mid tones");
  assert.match(dark, /varAlpha\(theme\.vars\.palette\[colorKey\]\.mainChannel, 0\.16\)/);
});

test("guard: kanban-card-raised -- work-board cards stay raised in dark with the amber attention border", () => {
  const board = read("features/work-board/work-board-board.tsx");
  assert.match(board, /CARD_ROOT_SX[\s\S]*bgcolor: "background\.paper"[\s\S]*customShadows\.card/);
  assert.match(board, /HOT_CARD_SX[\s\S]*borderColor: t\.vars\.palette\.warning\.main/);
  assert.match(board, /<KanbanItemRoot sx=\{hot \? HOT_CARD_SX : CARD_ROOT_SX\}/);
});

test("guard: labelled-filter-fields -- every worklist filter field shows its own label", () => {
  // /weighing/analytics Pens: the compare value box was a bare 96px input beside a 120px select whose
  // label was cut mid-word ("Average weigh…"), AUDIT1 P1-15. Template toolbar fields carry labels.
  const source = read("components/worklist-filters.tsx");
  const compare = source.slice(source.indexOf("const staged = opDraft !== field.op"), source.indexOf("The toolbar's search box"));
  // Each field's own props: from its `<TextField` to its first child/next field (handlers nest deeply).
  const fields = compare.split("<TextField").slice(1).map((chunk) => chunk.split(/<MenuItem|<TextField|\n\s*\/>/)[0]);
  assert.ok(fields.length >= 2, "operator select + value field");
  for (const props of fields) assert.match(props, /\blabel=/, "each compare TextField has a visible label");
  assert.doesNotMatch(compare, /width: 96\b|sm: 120\b/, "no fixed widths that cut the label");
});

test("guard: legacy-card-css-deleted -- the retired chart-card / KPI-grid / dialog caret CSS stays deleted", () => {
  // R3CNT 2026-09-27: no page renders `.card.wchart` / `.card.wtable` (guard page-template-legacy-card),
  // the `.g2`..`.g6` KPI grids (template Grid + KpiGrid) or the work-board dialog `.it .car` caret any
  // more, so their rules in the legacy stylesheets were dead weight that a stray class could revive.
  const DEAD = /(?:^|[\s,{}>+~(])\.(?:wchart|wtable|g[2-6])(?![\w-])|\.wb-dialog\s+\.it(?:\.open)?\s+\.car(?![\w-])/;
  for (const path of ["app/frame.css", "app/mesha-theme.css", "app/minimal-theme.css"]) {
    const css = read(path).replace(/\/\*[\s\S]*?\*\//g, "");
    const hits = css.split("\n").map((line, i) => [i + 1, line]).filter(([, line]) => DEAD.test(line));
    assert.deepEqual(hits, [], `${path}: retired selector is back:\n${hits.map(([n, l]) => `${n}: ${l.trim()}`).join("\n")}`);
  }
  for (const path of productTsx) {
    assert.doesNotMatch(read(path), /className=\{?["'`][^"'`]*(?<![\w-])(?:wchart|wtable|g[2-6])(?![\w-])/, `${path}: renders a retired legacy class`);
  }
});

test("guard: kpi-deck-not-in-card -- a KPI row (KpiGrid) is a page row, never inside another Card", () => {
  // R3CNT 2026-09-27: /weighing/weights Growth Director put six bordered KPI widgets inside a
  // "Road to sale weight" card (a card grid in a card), then repeated the same title on the bands card.
  const offenders = [];
  for (const path of productTsx) {
    const source = read(path);
    for (const m of source.matchAll(/<KpiGrid\b/g)) {
      const before = source.slice(0, m.index);
      const open = (before.match(/<Card[\s>]/g) ?? []).length - (before.match(/<\/Card>/g) ?? []).length;
      if (open > 0) offenders.push(path);
    }
  }
  // Shrink-only: /weighing/analytics feed-by-weight-band card still nests its deck (follow-up).
  const KNOWN = ["features/weighing/feed-weight-band-card.tsx"];
  assert.deepEqual([...new Set(offenders)].filter((path) => !KNOWN.includes(path)), [], "KPI deck inside a Card");
});
