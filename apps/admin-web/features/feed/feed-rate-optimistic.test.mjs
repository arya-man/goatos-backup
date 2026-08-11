import assert from "node:assert/strict";
import test from "node:test";

import { rationRateKey, sameRate } from "./feed-rate-optimistic-state.ts";

// The rule that decides when a just-saved rate stops being shown and the server's own value takes
// over. Display only — nothing here decides what is stored.

// THE DEFECT THIS PINS, found in the browser and not by any earlier check (2026-08-11): the compare
// was done on STRINGS. An operator types "2158" and the numeric(12,3) column returns "2158.000", so
// the server's answer never matched what was typed, the entry never cleared, and the cell kept an
// unconfirmed-looking number long after the write had actually landed.
test("the authored decimal and the stored decimal are the same rate", () => {
  assert.equal(sameRate("2158", "2158.000"), true);
  assert.equal(sameRate("4.5", "4.500"), true);
  assert.equal(sameRate("0", "0.000"), true);
});

test("a genuinely different rate has not settled", () => {
  assert.equal(sameRate("2158", "2157.000"), false);
  assert.equal(sameRate("0", "0.001"), false);
});

// An authored 0 means "feed nothing of this item" and is correct for milk-fed kids; an ABSENT rate
// means unconfigured and BLOCKS the shed. The two must never compare equal, here or anywhere.
test("an authored zero is not the same as an absent rate", () => {
  assert.equal(sameRate("0", ""), false);
});

test("a non-numeric value still settles on an exact match", () => {
  assert.equal(sameRate("n/a", "n/a"), true);
  assert.equal(sameRate("n/a", "none"), false);
});

// The key mirrors Postgres feed_config_norm: trim, casefold, collapse whitespace/underscore/hyphen.
test("the key normalizes the way the storage key does", () => {
  assert.equal(
    rationRateKey("p1", " Anantapur Sheep ", "Buck", "Dry Masoor Bhusa"),
    rationRateKey("p1", "anantapur_sheep", "buck", "dry-masoor-bhusa"),
  );
});

// Park is part of the key because the same group/tag/item combination exists in BOTH parks with
// different authored rates. Dropping it would let one park's save paint the other park's cell.
test("the key keeps the same combination in two parks apart", () => {
  assert.notEqual(
    rationRateKey("park-cbe", "Beetal/Sirohi", "Buck", "COFS"),
    rationRateKey("park-cpt", "Beetal/Sirohi", "Buck", "COFS"),
  );
});
