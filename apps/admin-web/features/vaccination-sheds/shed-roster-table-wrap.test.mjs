import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(resolve(here, "../../app/mesha-theme.css"), "utf8");

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

test("shed animal roster linked cells do not split short values into characters", () => {
  const selector = ["table.shed-animal-roster-table", "td .celllink"];

  assert.ok(
    hasDeclaration(selector, "white-space:nowrap"),
    "clickable shed roster cells must be nowrap; otherwise values like female/381d wrap character-by-character",
  );
  assert.ok(
    hasDeclaration(selector, "overflow-wrap:normal"),
    "clickable shed roster cells must override the global .celllink overflow-wrap:anywhere",
  );
  assert.ok(
    hasDeclaration(selector, "word-break:normal"),
    "clickable shed roster cells must override the global .celllink word-break:break-word",
  );
});

test("shed animal roster owns horizontal scroll with stable column widths", () => {
  const tableRule = declarationsFor(["table.shed-animal-roster-table"]).join("");
  const minWidth = tableRule.match(/min-width:(\d+)px/);

  assert.ok(minWidth, "shed roster table needs a min-width so columns cannot collapse to slivers");
  assert.ok(Number(minWidth[1]) >= 1500, "shed roster table must scroll horizontally instead of wrapping dense columns");
});
