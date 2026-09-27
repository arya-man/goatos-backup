import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// TR1-#27 (guard: order-history-body-block). The template OrderDetailsHistory puts the time caption
// (an inline span) on its own line because the block Typography title sits above it. Our added
// `body` line is a caller ReactNode: a bare string there flowed inline, so the /workflows chain rail
// read "…field tasksNEXT". The derived section wraps the body in its own block, whatever a caller
// passes, and the time caption stays a span after it.
test("OrderDetailsHistory renders the body in its own block before the time caption", () => {
  const src = readFileSync(new URL("./sections/order/order-details-history.tsx", import.meta.url), "utf8");
  const content = src.slice(src.indexOf("<TimelineContent>"), src.indexOf("</TimelineContent>"));
  assert.match(content, /\{item\.body \? <Box sx=\{\{[^}]*\}\}>\{item\.body\}<\/Box> : null\}\s*<Box component="span"[^>]*>\s*\{item\.time\}/);
  assert.doesNotMatch(content, /^\s*\{item\.body\}\s*$/m, "a bare {item.body} flows inline into the time caption");
});
