// guard: fixj2-area-template-only (FIXJ2 J1 P0-1 / P0-2 / P1-1 / P1-2 / P1-3). Every /herd-signals
// feature file renders MUI / template parts on sx only: no className (so no class token from the
// legacy stylesheets), no inline style={{…}}, no native control / table markup, no lucide-react and
// no hand-drawn <svg> icon (template Iconify instead). The page-scoped `.herd-signals-page` CSS the
// old markup relied on stays deleted, and the responsive table behaviour it carried now lives in
// herd-signals-sx.ts (phone field grid labelled by data-l, desktop column floors, sticky first column).
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import test from "node:test";
import { legacyCss } from "../../scripts/lib/legacy-css.mjs";

const dir = new URL("./", import.meta.url);
const read = (path) => readFileSync(new URL(path, dir), "utf8");
const files = readdirSync(dir).filter((name) => name.endsWith(".tsx"));

const BANNED = [
  ["className", /\bclassName=/],
  ["inline style", /\bstyle=\{\{/],
  ["native control", /<(?:button|input|select|textarea|table|thead|tbody|tr|td|th)[\s>]/],
  ["lucide", /from ["']lucide-react["']/],
  ["hand svg", /<svg[\s>]/],
  ["native datetime", /type=["']datetime-local["']/],
];

export function findings(src) {
  const out = [];
  src.split("\n").forEach((line, index) => {
    for (const [kind, pattern] of BANNED) if (pattern.test(line)) out.push(`${kind} at line ${index + 1}: ${line.trim()}`);
  });
  return out;
}

test("self-test: the scan catches every banned shape and passes template parts", () => {
  for (const bad of ['<div className="card">', '<span style={{ flex: 1 }} />', "<button type=\"button\">", "<tr>", '<th className="num">', 'import { Radio } from "lucide-react";', '<svg className="ic" viewBox="0 0 24 24">', '<input type="datetime-local" />']) {
    assert.ok(findings(bad).length > 0, `must flag: ${bad}`);
  }
  for (const good of ['<TableCell data-l="Pen">', '<Box sx={{ flex: 1 }} />', '<Iconify icon="solar:link-bold" />', "<TableRow sx={selectableRowSx()}>"]) {
    assert.deepEqual(findings(good), [], `must pass: ${good}`);
  }
});

for (const name of files) {
  test(`${name} is template/MUI parts on sx only`, () => {
    assert.deepEqual(findings(read(name)), []);
  });
}

test("no feature stylesheet and no page-scoped legacy CSS for /herd-signals", () => {
  assert.deepEqual(readdirSync(dir).filter((name) => name.endsWith(".css")), []);
  for (const css of ["mesha-theme", "frame", "minimal-theme"]) {
    assert.doesNotMatch(legacyCss(css), /\.herd-signals-(?:page|table|pager|fbar|kpis)\b|\.hs-(?:selectable|watch|flash|mapping-tab|btn)\b/, `${css} still styles /herd-signals`);
  }
});

test("the responsive tables keep their phone field grid and desktop column floors in sx", () => {
  const sx = read("herd-signals-sx.ts");
  assert.match(sx, /content: "attr\(data-l\)"/, "phone rows label each cell from data-l");
  assert.match(sx, /"& thead": \{ display: "none" \}/, "phone rows hide the header row");
  assert.match(sx, /gridTemplateColumns: "1fr 1fr"/, "phone rows are a two-column field grid");
  assert.match(sx, /borderBottom: `1px solid \$\{theme\.vars\.palette\.divider\}`/, "phone rows are divider rows, never cards in the card");
  assert.match(sx, /whiteSpace: "normal"/, "phone cell values wrap instead of widening the page");
  assert.match(sx, /position: "sticky", left: 0/, "desktop keeps the identity column in view");
  const table = read("herd-signals-table.tsx");
  assert.match(table, /const LIVE_COLUMN_WIDTHS = \[(?:\d+, ){20}\d+\]/, "21 live-table column floors");
  assert.match(table, /const LIVE_TABLE_MIN_WIDTH = 2860/, "the live table scrolls inside its card instead of compressing 21 columns");
  assert.match(table, /respTableSx\(\{ minWidth: LIVE_TABLE_MIN_WIDTH, columns: LIVE_COLUMN_WIDTHS \}\)/);
});

test("the full-screen history range uses the template date-time picker, not native inputs", () => {
  const fs = read("herd-signals-history-fullscreen.tsx");
  assert.match(fs, /from "@mui\/x-date-pickers\/DateTimePicker"/);
  assert.match(fs, /format="DD\/MM\/YYYY HH:mm"/, "visible timestamps are DD/MM/YYYY HH:MM");
  assert.match(fs, /<ToggleButtonGroup[\s\S]{0,400}aria-label="History range"/, "range presets are a template toggle group");
});
