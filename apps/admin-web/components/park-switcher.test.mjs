import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// Header park switcher (template-derived WorkspacesPopover, 240px list).
const popover = readFileSync(new URL("../layouts/app/components/workspaces-popover.tsx", import.meta.url), "utf8");
const shell = readFileSync(new URL("./mesha-shell.tsx", import.meta.url), "utf8");

test("O37: long park names wrap and the park code shows", () => {
  const nameCell = popover.match(/<Typography\b[\s\S]*?>\s*\{option\.name\}/)?.[0] ?? "";
  assert.ok(nameCell, "option name Typography found");
  assert.doesNotMatch(nameCell, /\bnoWrap\b/, "option name must wrap, never ellipsis-cut");
  assert.match(nameCell, /whiteSpace: 'normal'/, "MenuItem sets nowrap; the name must reset it");
  assert.doesNotMatch(popover, /sx=\{\{ height: 48 \}\}/, "a fixed 48px row would clip a wrapped name");
  assert.match(popover, /minHeight: \{ xs: 48 \}, height: 'auto'/, "rows keep the 48px floor and grow with a wrapped name");
  assert.match(popover, /\{option\.code \?[\s\S]*?\{option\.code\}/, "option shows the park code");
  // O39: one long wrapped park (~210px row at 240) plus All parks must show whole, with no inner scroll.
  assert.match(popover, /width: 280 \} \}/, "list paper is 280 wide");
  assert.match(popover, /<Scrollbar sx=\{\{ maxHeight: 360 \}\}>/, "list scroll cap is 360");
  assert.match(shell, /code: p\.code && p\.code !== p\.name \? p\.code : null,/, "shell passes the park code to the switcher");
});

test("O38: unknown ?park reads the selected-park copy and selects no option", () => {
  assert.match(popover, /pickScopeOption\(data, value, fallback\)/, "trigger/selection resolve through pickScopeOption");
  assert.match(popover, /selected=\{option\.id === selectedId\}/, "selection follows the matched id only");
  assert.doesNotMatch(popover, /\?\? data\[0\]/, "never falls back to the first option (All parks) for a set value");
  assert.match(shell, /fallback=\{\{ name: shellCopy\(contract, "scope\.selected_park"\)/, "shell passes the selected-park copy");
});

// guard: top-bar-park-rule (TR1-#12). The header park switcher shows on every park-scoped route; it is
// hidden only where the page has its own park filter (own-filter) or reads no park at all (no-park).
// The rule lives in one place (mesha-shell.tsx) and every hidden route states which case it is.
{
  const { readFileSync: read } = await import("node:fs");
  const shellSrc = read(new URL("./mesha-shell.tsx", import.meta.url), "utf8");
  assert.match(shellSrc, /TOP-BAR PARK RULE \(the one place/);
  for (const name of ["PAGES_WITH_LOCAL_OR_NO_PARK_SCOPE", "ROUTE_FAMILIES_WITH_LOCAL_OR_NO_PARK_SCOPE", "ROUTE_PATTERNS_WITH_LOCAL_OR_NO_PARK_SCOPE"]) {
    const block = shellSrc.match(new RegExp(`const ${name} = \\[([\\s\\S]*?)\\n  \\];`));
    assert.ok(block, `${name} exists`);
    const entries = block[1].split("\n").filter((l) => /^\s*("|\/\^|\.\.\.)/.test(l));
    assert.ok(entries.length > 0, `${name} has entries`);
    for (const line of entries) assert.match(line, /\/\/ (own-filter|no-park)\b/, `${name}: "${line.trim()}" must say own-filter or no-park`);
  }
}

// guard: park-rule-follows-pending-route (TR-2 P1-1). The shell paints the TARGET route's skeleton in the
// click frame, so the header's park rule must read the pending target too: /vaccination -> a pen showed
// the switcher in the skeleton and dropped it when the page committed.
test("the top-bar park rule follows the pending route while its skeleton is painted", () => {
  assert.match(shell, /const lockTopBarParkSelector = routeOwnsOrIgnoresTopBarPark\(\s*pendingHref \? pendingHref\.split\(\/\[\?#\]\/\)\[0\] : pathname,/);
  assert.ok(shell.indexOf("const [pendingHref, setPendingHref]") < shell.indexOf("const lockTopBarParkSelector"), "pendingHref is declared before the rule reads it");
});
