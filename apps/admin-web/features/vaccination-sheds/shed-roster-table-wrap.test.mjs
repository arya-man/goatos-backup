import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { copy } from "../../lib/admin-ui-contract.ts";

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(resolve(here, "../../app/mesha-theme.css"), "utf8");
const pageSource = readFileSync(resolve(here, "shed-detail.tsx"), "utf8");

function declarationsFor(selectorNeedles) {
  const matches = [];
  const rulePattern = /([^{}]+)\{([^{}]+)\}/g;
  let match;
  while ((match = rulePattern.exec(css)) !== null) {
    const selector = match[1].replace(/\s+/g, " ").trim();
    if (selectorNeedles.every((needle) => selector.includes(needle))) {
      matches.push(match[2].replace(/\s+/g, ""));
    }
  }
  return matches;
}

function hasDeclaration(selectorNeedles, declaration) {
  return declarationsFor(selectorNeedles).some((body) => body.includes(declaration));
}

test("shed animal roster row content does not split short values into characters", () => {
  const selector = ["table.shed-animal-roster-table", ".shed-animal-roster-cell-content"];

  assert.ok(
    hasDeclaration(selector, "white-space:nowrap"),
    "shed roster cells must be nowrap; otherwise values like female/381d wrap character-by-character",
  );
  assert.ok(
    hasDeclaration(selector, "overflow-wrap:normal"),
    "shed roster cells must override free-text overflow wrapping",
  );
  assert.ok(
    hasDeclaration(selector, "word-break:normal"),
    "shed roster cells must override free-text word breaking",
  );
});

test("shed animal roster owns horizontal scroll with stable column widths", () => {
  const tableRule = declarationsFor(["table.shed-animal-roster-table"]).join("");
  const minWidth = tableRule.match(/min-width:(\d+)px/);

  assert.ok(minWidth, "shed roster table needs a min-width so columns cannot collapse to slivers");
  assert.ok(Number(minWidth[1]) >= 1500, "shed roster table must scroll horizontally instead of wrapping dense columns");
});

test("each shed animal roster record has one keyboard-accessible link covering the whole row", () => {
  const rosterStart = pageSource.indexOf("function AnimalRosterCard");
  const rosterEnd = pageSource.indexOf("// Shed-wise vaccination detail", rosterStart);
  const rosterSource = pageSource.slice(rosterStart, rosterEnd);

  assert.match(rosterSource, /<tr key=\{a\.goatId\} className="shed-animal-roster-row">/);
  assert.match(
    rosterSource,
    /<LocalOverlayLink[\s\S]*?className="shed-animal-roster-row-link"[\s\S]*?aria-label=/,
    "the row target must remain a real LocalOverlayLink so keyboard, deep-link, and no-JS behavior survive",
  );
  assert.equal(
    (rosterSource.match(/<LocalOverlayLink\b/g) ?? []).length,
    1,
    "one animal record must expose one row link, not eleven text-sized cell links",
  );

  assert.ok(
    hasDeclaration(["table.shed-animal-roster-table", ".shed-animal-roster-row"], "position:relative"),
    "the animal row must establish the containing block for its full-row link",
  );
  const linkRule = declarationsFor(["table.shed-animal-roster-table", ".shed-animal-roster-row-link"]).join("");
  assert.ok(linkRule.includes("position:absolute"), "the row link must stretch independently of cell text width");
  assert.ok(linkRule.includes("inset:0"), "the row link must cover blank space across the entire animal row");

  assert.ok(
    hasDeclaration(["table.shed-animal-roster-table", ".shed-animal-roster-cell-content", ":is(a,button"], "pointer-events:auto"),
    "a real control added to a row must remain independently clickable instead of being swallowed by the row link",
  );
});

test("shed animal row action has a contract-safe fallback before live backend hydration", () => {
  assert.equal(
    copy({ route_id: "shed-execution", copy: {} }, "action.open_passport"),
    "Open Animal Passport",
  );
});
