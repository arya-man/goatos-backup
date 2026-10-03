// The Sold page's weight-band tiles are a RENDERER, and these are the three ways that stops
// being true. They read the page source as text on purpose: the assertions are about what the
// component is allowed to KNOW, which no rendered output can show.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./sales-sold.tsx", import.meta.url), "utf8");

test("the band tiles come from the backend's own list, not a hardcoded four", () => {
  assert.match(
    source,
    /overview\.sold_weight_bands\.bands\.map\(/,
    "the tiles must map the contract's bands array so the backend owns which bands exist and their order",
  );
  // The retired flat fields: reading one again would re-fix the band set in the page.
  for (const retired of ["at_or_above_40_kg", "from_35_to_40_kg", "from_20_to_35_kg", "under_20_kg"]) {
    assert.ok(!source.includes(retired), `page still reads the retired flat field ${retired}`);
  }
});

test("every visible word is a copy key, never a literal", () => {
  assert.match(
    source,
    /copy\(pageContract, `sold_weight\.band\.\$\{band\.band\}`\)/,
    "the tile label must be looked up by the band's own key",
  );
  for (const key of [
    "sold_weight.source.measured",
    "sold_weight.source.load_average",
    "sold_weight.source.estimated",
  ]) {
    assert.ok(source.includes(key), `missing backend-owned copy key ${key}`);
  }
  // Words a page must never compose for itself. "estimated" reaches the screen only through
  // sold_weight.source.estimated above.
  for (const literal of ["weighed<", "at load average", ">Estimated", "kg and above"]) {
    assert.ok(!source.includes(literal), `page hardcodes the visible words ${literal}`);
  }
});

test("a provenance is named only where it exists, and the estimate note is gone", () => {
  // Each of the three parts is guarded on its own count, so a band whose animals were all
  // weighed the same way does not print "0 at load average" beside the number.
  for (const guard of ["band.measured > 0", "band.load_average > 0", "band.estimated > 0"]) {
    assert.ok(source.includes(guard), `missing the ${guard} guard`);
  }
  // The estimate note under the tiles was removed (maintainer, 2026-10-03).
  assert.ok(!source.includes("sold_weight.estimated.note"), "the estimate note is gone");
});
