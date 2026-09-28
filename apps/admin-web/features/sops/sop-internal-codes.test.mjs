import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: sop-no-internal-codes (FJ3 P1-13, OCI smoke #19/#42). The SOP editors printed each
// capture/question key ("return_to_pen", "feed_transport_video", "purchase_date") and each choice's
// stored value beside the human label, and the SOP detail dialog printed "Code counts.reconcile"
// and "type: animal_id_scan". Keys stay in the data (the phones stamp uploads with them); a person
// reads the title, the choice label and a worded field type.
const read = (file) => readFileSync(new URL(file, import.meta.url), "utf8");

test("SOP editors do not print internal keys or choice values", () => {
  for (const file of ["./feed-editor.tsx", "./shifting-editor.tsx", "./weighing-editor.tsx", "./inspection-editor.tsx", "./pc-care-editor.tsx", "./capture-editor.tsx"]) {
    const src = read(file);
    assert.doesNotMatch(src, /<code[^>]*>\{(?:slot|p|q|row)\.key\}<\/code>/, `${file} prints a key`);
    assert.doesNotMatch(src, /<code[^>]*>\{o\.value\}<\/code>/, `${file} prints a choice value`);
  }
});

test("SOP detail dialog shows no SOP code and words the field type; full screen on a phone", () => {
  const lib = read("./sop-library.tsx");
  assert.doesNotMatch(lib, /\{view\.code\}<\/div>/, "the detail dialog prints the SOP code");
  assert.doesNotMatch(lib, /label\.type"\)\}: \{f\.type\}/, "the detail dialog prints a raw field type");
  assert.match(lib, /<Dialog fullWidth fullScreen=\{fullScreen\} maxWidth="md"/);
});

// guard: sop-editor-template-fields (FJ3 P1-16). Editor fields are MUI outlined TextFields with
// their own label (no label-above `.numlbl` wrapper beside a floating-label select), and the legacy
// `.qcard input` paint (border, padding, background) must not reach the MUI input inside them —
// it drew a second box inside every outlined field.
test("SOP editor text fields carry their own MUI label and escape the legacy .qcard input paint", () => {
  for (const file of ["./feed-editor.tsx", "./followup-editor.tsx", "./shifting-editor.tsx"]) {
    assert.doesNotMatch(read(file), /<label className="numlbl">\s*\{copy\([^}]*\)\}\s*<MuiTextField/, `${file}: label-above wrapper around a TextField`);
  }
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  const rule = css.match(/\.qcard input[^{]*\{/);
  assert.ok(rule && /:not\(\.MuiInputBase-input\)/.test(rule[0]), ".qcard input rule must exclude MUI inputs");
});

// guard: sop-flow-phone-fit (FJ3 P1-17): on a phone the flow opened at ~41% hugging the right edge.
test("SOP flow canvas keeps a 70% floor on a phone and centres the scaled flow", () => {
  const canvas = read("./flow-canvas.tsx");
  assert.match(canvas, /el\.clientWidth < 600 \? 0\.7 : 0\.35/);
  assert.match(canvas, /width: layout\.width \* zoom, height: layout\.height \* zoom, margin: "0 auto"/);
});

// guard: sop-editor-template-fields (FJ3 P1-16, second half). No label-above field anatomy left in the
// SOP editors: every text/number/time field is an outlined MUI TextField with its own label (multiline
// for instructions and hints), choices are MUI selects, and group headings are template
// Typography subtitle2 (product new-edit form) instead of the legacy .qcfg-title / .numlbl spans.
test("SOP editors carry no label-above wrappers, native textareas/selects or legacy group titles", () => {
  for (const file of ["./weighing-editor.tsx", "./inspection-editor.tsx", "./shifting-editor.tsx", "./pc-care-editor.tsx", "./feed-editor.tsx", "./toxin-editor.tsx"]) {
    const src = read(file);
    assert.doesNotMatch(src, /className="numlbl"|className="numfield"/, `${file}: label-above .numlbl/.numfield wrapper`);
    assert.doesNotMatch(src, /<textarea\s/, `${file}: native <textarea> (use MuiTextField multiline with a label)`);
    assert.doesNotMatch(src, /<select\s/, `${file}: native <select> (use MuiTextField select / InlineSelect)`);
    assert.doesNotMatch(src, /className="qcfg-title"/, `${file}: legacy .qcfg-title (use Typography variant="subtitle2")`);
  }
});

// guard: sop-select-option-title (REVIEW-4 O8). Moving the toxin step-kind native <select> onto
// InlineSelect dropped each option's backend description (tsop_step_kinds title). Select options
// carry an optional `title` rendered on the MenuItem, and the toxin kind select passes it.
test("SOP select options keep their backend description as the menu item title", () => {
  const chrome = read("./editor-chrome.tsx");
  assert.match(chrome, /type SelectOption = \{ value: string; label: string; title\?: string \}/);
  assert.equal((chrome.match(/<MenuItem key=\{option\.value\} value=\{option\.value\} title=\{option\.title\}>/g) ?? []).length, 2);
  assert.match(read("./toxin-editor.tsx"), /options=\{kinds\.map\(\(k\) => \(\{ value: k\.key, label: k\.label, title: k\.title \}\)\)\}/);
});

// guard: sop-library-template-job-list (TR1-#23/#33, replaces sop-library-skeleton-kpi-row). The template
// job list has a search field plus filters and one ⋮ popover over the card grid: no KPI row, no filter
// card, no Columns / Export text buttons. Cards take the JobItem logo slot (rounded 48px Avatar with
// the letter fallback), not a green icon tile. The loading twin draws the same blocks.
test("SOP library is the template job list and its skeleton mirrors it", () => {
  const src = read("./sop-library.tsx");
  assert.doesNotMatch(src, /<KpiGrid>|KpiWidget/);
  assert.match(src, /<FilterBar\s+bare\b/);
  assert.doesNotMatch(src, /<Button[^>]*startIcon=\{<(Columns3|Download)\b/, "Columns / Export belong in the ⋮ menu");
  // Cards are the template-derived JobItem / JobList through their slots (as /procurement/animal-purchases),
  // never a hand-built grid of MuiCards; an icon tile fills the logo slot (TR2-P2-10, sop-card-logo-tile).
  assert.match(src, /import \{ JobItem, type JobItemFact \} from "@\/components\/app\/sections\/job\/job-item";/);
  assert.match(src, /import \{ JobList \} from "@\/components\/app\/sections\/job\/job-list";/);
  assert.match(src, /<JobList pagination=\{/);
  assert.match(src, /<JobItem\b[\s\S]{0,400}avatar=\{<Iconify /);
  assert.doesNotMatch(src, /from "@mui\/material\/(Card|Pagination)"|gridTemplateColumns: \{ xs: "repeat\(1, 1fr\)"/);
  // No legacy wrapper classes or inline styles on the page (the banner is a template Alert with its action slot).
  assert.doesNotMatch(src, /kit-enter|sop-kit|sop-published-banner|<Alert[^>]*style=\{/);
  const skel = read("./sop-route-skeleton.tsx");
  assert.doesNotMatch(skel, /KpiRowSkeleton|StatStripSkeleton/);
  assert.match(skel, /<FilterCardSkeleton bare /);
  // TR1-#1: the toolbar twin folds like the bar (selects md+, Filters button below md), ⋮ is 36 (44 below md).
  // TR3-P1-1: search + Filters + ⋮ on one phone row (the fold search basis shared with FilterBar).
  assert.match(skel, /<FilterCardSkeleton bare fold fields=\{\[160, 160, "search"\]\} actionWidths=\{\[36\]\} searchBasis=\{\{ \.\.\.FILTER_SEARCH_FOLD_BASIS \}\} \/>/);
  // TR3-P0-3: one grid row of placeholder cards at every width; the header layout is shared with the page.
  assert.match(skel, /<CardGridSkeleton count=\{SOP_SKELETON_CARDS\} oneRow \/>/);
  assert.match(skel, /<PageHeaderSkeleton layout=\{SOP_HEADER_LAYOUT\}/);
  assert.match(src, /<PageHeader\s+layout=\{SOP_HEADER_LAYOUT\}/);
  assert.match(src, /<Box sx=\{\{ display: "flex", flexDirection: "column", gap: 3 \}\}>/);
  assert.match(skel, /<PageSkeleton gap=\{3\}>/);
});

// guard: job-item-menu-label (REVIEW-38 O54). The template JobItem ⋮ is an icon-only button, so it
// carries a declared accessible name (allowProps IconButton:aria-label) that the type makes required
// whenever there are menu items; SOP cards name it "More: <SOP name>" as the old RowMenu did.
test("JobItem ⋮ has an accessible name and SOP cards set it", () => {
  const item = read("../../components/app/sections/job/job-item.tsx");
  assert.match(item, /<IconButton onClick=\{menuActionsPopover\.onOpen\} aria-label=\{menuLabel\}/);
  assert.match(item, /\| \{ menuActions: JobItemMenuAction\[\]; menuLabel: string \}/, "menuLabel is required with menuActions");
  const derived = JSON.parse(read("../../../../docs/design/template-derived.json"));
  const entry = Object.entries(derived).flatMap(([, v]) => (v && typeof v === "object" ? Object.entries(v) : [])).find(([k]) => k === "components/app/sections/job/job-item.tsx");
  assert.ok(entry, "job-item.tsx is declared template-derived");
  assert.ok(entry[1].allowProps.includes("IconButton:aria-label"), "the aria-label is a declared override");
  const src = read("./sop-library.tsx");
  assert.match(src, /menuLabel=\{`\$\{copy\(pageContract, "action\.more"\)\}: \$\{view\.name\}`\}/);
  // Every JobItem with menu items in the app passes a label (TypeScript enforces it too).
  for (const file of ["./sop-library.tsx", "../procurement/animal-purchases.tsx"]) {
    for (const m of read(file).matchAll(/<JobItem\b[\s\S]*?\/?>/g)) {
      if (/menuActions=/.test(m[0])) assert.match(m[0], /menuLabel=/, `${file}: JobItem with menuActions needs menuLabel`);
    }
  }
});

// guard: sop-paging-client-only (REVIEW-38 O55). The SOP list is whole on the client, so a page change
// rewrites the URL `page` with history.replaceState (useSearchParams follows, deep links still open the
// page) and never costs a server navigation: no router.replace/push for the page, and the JobList pager
// items call onSelect on a plain click instead of following a Next Link.
test("SOP card paging is client-only with a deep-linkable page param", () => {
  const src = read("./sop-library.tsx");
  const setter = src.match(/const setRequestedPage = \(n: number\) => \{([\s\S]*?)\n  \};/);
  assert.ok(setter, "setRequestedPage exists");
  assert.match(setter[1], /window\.history\.replaceState\(window\.history\.state, "", pageHref\(n\)\)/);
  assert.doesNotMatch(setter[1], /router\./);
  assert.doesNotMatch(src, /router\.replace\(/);
  assert.match(src, /searchParams\.get\(PAGE_PARAM\)/, "a deep link's page param still selects the page");
  assert.match(src, /<JobList pagination=\{\{.*onSelect: setRequestedPage \}\}>/);
  const list = read("../../components/app/sections/job/job-list.tsx");
  assert.match(list, /pagination\.onSelect \? \(\s*<PaginationItem\s+component="a"/);
  assert.match(list, /event\.preventDefault\(\);\s*if \(page\) pagination\.onSelect\(page\);/);
  assert.match(list, /onClick=\{selectPage\(item\.page\)\}/);
});
