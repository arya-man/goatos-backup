// Guard: counts-template-only (FIXJ2 J1 P0-1/P0-2/P1-1/P1-2/P1-3). Every /counts/* feature file renders
// MUI + template parts only: no className (legacy mesha-theme / frame / feature CSS classes), no inline
// style props, no lucide-react icons (Iconify, registered set), no native form or table elements (a
// hidden / file input inside a form or the template UploadFile is the one exception), and no feature
// stylesheet under features/counts.
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import test from "node:test";

const dir = new URL("./", import.meta.url);
const sources = readdirSync(dir)
  .filter((name) => name.endsWith(".tsx") && !name.includes(".test.") && !name.includes(".stories."))
  .map((name) => [name, readFileSync(new URL(name, dir), "utf8")]);

const RULES = [
  [/className=/, "a className (legacy class)"],
  [/style=\{\{/, "an inline style prop"],
  [/from "lucide-react"/, "a lucide-react icon"],
  [/<(button|select|textarea|table|thead|tbody|tfoot|tr|td|th)[\s>]/, "a native control / table element"],
  [/<input\b(?![^>]*type="(hidden|file)")/, "a native text-like input"],
];

export function violations(source) {
  return RULES.filter(([re]) => re.test(source)).map(([, why]) => why);
}

test("counts-template-only: self-test", () => {
  assert.deepEqual(violations('<div className="card">'), ["a className (legacy class)"]);
  assert.deepEqual(violations('<button type="button">'), ["a native control / table element"]);
  assert.deepEqual(violations('<input id="x" name="y" />'), ["a native text-like input"]);
  assert.deepEqual(violations('<input type="hidden" name="y" value="1" />'), []);
  assert.deepEqual(violations('<Box sx={{ mb: 2 }} />'), []);
});

test("counts-template-only: every /counts feature file is MUI + template only", () => {
  const bad = sources.flatMap(([name, source]) => violations(source).map((why) => `${name}: ${why}`));
  assert.deepEqual(bad, []);
});

test("counts-template-only: no feature stylesheet under features/counts", () => {
  const css = readdirSync(dir).filter((name) => name.endsWith(".css"));
  assert.deepEqual(css, []);
});
