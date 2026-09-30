import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

// guard: not-found-template-view (J2B P2-14). The 404 is the template NotFoundView (bouncing title +
// body, PageNotFoundIllustration, large "Go to home") in the shell, never a small StatePanel card.
test("the root 404 renders the template NotFoundView anatomy", () => {
  const page = read("../../app/not-found.tsx");
  assert.match(page, /<NotFoundView /);
  assert.doesNotMatch(page, /<StatePanel|import \{ StatePanel/);
  const view = read("./not-found-view.tsx");
  assert.match(view, /PageNotFoundIllustration sx=\{\{ my: \{ xs: 5, sm: 10 \} \}\}/);
  assert.match(view, /<Button component=\{Link\} href="\/" size="large" variant="contained">/);
  assert.match(view, /<Typography variant="h3" component="h1" sx=\{\{ mb: 2 \}\}>/);
  assert.match(view, /maxWidth: 448/);
});
