import assert from "node:assert/strict";
import test from "node:test";
import {
  WRITE_MARKER_WINDOW_MS,
  isBackendWrite,
  readBypassesShortCache,
  writeMarkerValue,
} from "./write-marker.ts";

test("a read right after the caller's own write bypasses the short cache, on any instance", () => {
  const writtenAt = 1_000_000;
  const marker = writeMarkerValue(writtenAt);
  assert.equal(readBypassesShortCache(marker, writtenAt + 1), true);
  assert.equal(readBypassesShortCache(marker, writtenAt + WRITE_MARKER_WINDOW_MS - 1), true);
});

test("the marker stops bypassing once the short-cache TTL window has passed", () => {
  const writtenAt = 1_000_000;
  assert.equal(readBypassesShortCache(writeMarkerValue(writtenAt), writtenAt + WRITE_MARKER_WINDOW_MS + 1), false);
});

test("no marker or a garbage marker never bypasses", () => {
  assert.equal(readBypassesShortCache(undefined, 5), false);
  assert.equal(readBypassesShortCache("", 5), false);
  assert.equal(readBypassesShortCache("nope", 5), false);
});

test("a marker from the future (clock skew) still bypasses, bounded to the window", () => {
  assert.equal(readBypassesShortCache(writeMarkerValue(2_000), 1_000), true);
  assert.equal(readBypassesShortCache(writeMarkerValue(1_000 + WRITE_MARKER_WINDOW_MS * 3), 1_000), false);
});

test("a dry-run preview POST is not a write; every other non-GET is", () => {
  // Stamping the marker from a server action re-renders the route and closes the overlay drawer
  // the preview was called from ("Tag animals to sale" closed on its own Review step).
  assert.equal(isBackendWrite("POST", "/admin/goats/sale-allocations/preview"), false);
  assert.equal(isBackendWrite("GET", "/sales/deals"), false);
  assert.equal(isBackendWrite("POST", "/admin/goats/sale-allocations/confirm"), true);
  assert.equal(isBackendWrite("PUT", "/admin/goats/sale-allocations/preview"), true);
  assert.equal(isBackendWrite("post", "/sales/deals"), true);
  assert.equal(isBackendWrite("DELETE", "/sales/deals/x/payments/y"), true);
});
