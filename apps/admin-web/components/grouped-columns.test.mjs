import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

test("grouped column missing series slots are hidden from mobile webview paint", () => {
  const source = readFileSync(new URL("./grouped-columns.tsx", import.meta.url), "utf8");
  const css = readFileSync(new URL("../app/mesha-theme.css", import.meta.url), "utf8");

  assert.match(source, /className="gcb gcempty" aria-hidden="true"/);
  assert.match(source, /value === null \|\| value === 0/);
  assert.match(css, /\.gcb\.gcempty\{visibility:hidden\}/);
  assert.doesNotMatch(source, /className="gcb">\s*<span className="gcbar none"/);
});
