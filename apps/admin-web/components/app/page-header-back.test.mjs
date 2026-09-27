// guard: page-header-back-link. The template CustomBreadcrumbs / BackLink stay verbatim; PageHeader
// renders the template BackLink itself as the heading (class minimal__breadcrumbs__back keeps the
// 44px phone tap rule and the gutter-arrow fix; the in-app trail makes it a history back).
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

test("PageHeader renders the template BackLink with the tap/gutter class and trail back", () => {
  const src = read("./page-header.tsx");
  assert.match(src, /import \{ BackLink \} from "@\/components\/minimal\/custom-breadcrumbs\/back-link";/);
  assert.match(src, /<BackLink[\s\S]*?className="minimal__breadcrumbs__back"[\s\S]*?trail\.back\(\)/);
  assert.doesNotMatch(src, /backHref=\{back\}/, "the template backHref path cannot carry the trail click");
});

test("the phone tap rule still targets the back link class", () => {
  assert.match(read("./phone-tap-styles.tsx"), /a\.minimal__breadcrumbs__back/);
});
