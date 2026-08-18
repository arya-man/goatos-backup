import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const component = readFileSync(new URL("./weight-bars.tsx", import.meta.url), "utf8");
const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");

test("bar mode chips are not inside the clamped label text", () => {
  const textIndex = component.indexOf('className="wbl-text"');
  const modeIndex = component.indexOf('className="wbar-mode"');

  assert.notEqual(textIndex, -1);
  assert.notEqual(modeIndex, -1);
  assert.ok(textIndex < modeIndex);
  assert.match(css, /\.wbar \.wbl\{[^}]*display:block/);
  assert.match(css, /\.wbar \.wbl-text\{[^}]*-webkit-line-clamp:2/);
  assert.match(css, /\.wcols \.wbar \.wbl-text\{[^}]*-webkit-line-clamp:3/);
});
