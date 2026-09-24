import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./load-comparison-tab.tsx", import.meta.url), "utf8");
const contract = readFileSync(
  new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url),
  "utf8",
);

test("each load on the comparison chart names the pens its weighed animals sit in", () => {
  // A bar saying a supplier's stock grew 1.4x, with no pen named, cannot be walked from: the
  // reader has no way to reach the animals it describes.
  //
  // The pens ride the group's own HEADING, in a bracket beside the load (maintainer request
  // 2026-09-22, replacing the sub-line this tab carried from 2026-09-21) -- the shape every load
  // chart on the dashboard now uses. They are still VISIBLE rather than hidden behind a hover
  // panel, which was the point of the sub-line and survives the move.
  assert.match(source, /chartHeading: withLoadPens\(heading, pensFromPlacements\(bucket\?\.placements\)\)/);
  assert.match(source, /heading: row\.chartHeading,/);
  // The TABLE keeps the fuller line, head counts and all, because a cell has room the axis does
  // not. Its head count comes from the same placement row the load's own average is weighted by,
  // so the line and the chart cannot disagree about how many animals were weighed where.
  assert.match(source, /pens: penList\(bucket\)/);
  assert.match(source, /\$\{where\} · \$\{p\.animals\.toLocaleString\("en-IN"\)\}/);
});

test("the pen line takes its names from the backend, and disambiguates parks only when it must", () => {
  // The display string is BACKEND-composed. This file must never join a shed name to a partition
  // label itself -- that is the hand-rolled composition the operational-location rule bans.
  assert.match(source, /p\.operational_location_display/);
  assert.doesNotMatch(source, /partition_label/);
  // The park is ALWAYS named (maintainer, 2026-09-05): a shed name repeats across parks, and this
  // tab is normally read unfiltered, so a bare "Castro 1" leaves the reader guessing.
  assert.match(source, /const where = p\.park_name \? `\$\{p\.park_name\} \$\{p\.operational_location_display\}` : p\.operational_location_display;/);
  assert.doesNotMatch(source, /multiPark/);
  // No pens is an ABSENT sub-line, never a dangling separator beside the multiple.
  assert.match(source, /if \(placements\.length === 0\) return "";/);
});

test("the caption says the pens are there", () => {
  assert.match(contract, /followed by the pens the load's animals are in today/);
});

test("the value chart names the same pens as the weight chart beside it", () => {
  // Maintainer request 2026-09-21: name the pen on EVERY graph, not only one. Two charts of the
  // same loads, side by side, labelling them differently is the cross-surface disagreement this
  // page's rules exist to stop -- and a bar saying a load is worth a sum of money names no pen, so
  // a reader cannot walk from it to the animals any more than they could from the weight bars.
  //
  // Both charts read the SAME row.chartHeading, so they cannot drift into two spellings of one
  // load's pens.
  assert.equal((source.match(/row\.chartHeading/g) ?? []).length, 2);
  // A SOLD-OUT load is named WITHOUT its pens. Its animals are gone, so naming the pens they used
  // to sit in would point the reader at a pen that no longer holds them. That rule predates the
  // bracket and survives it -- this is the ONE place the two charts label a load differently, and
  // it is deliberate.
  assert.match(source, /heading: stockAnimals === 0 \? row\.heading : row\.chartHeading,/);
  assert.match(contract, /no stock to value, and names no pen/);
});

test("the loads ledger names the pens too, and the card is inset like the Weights tables", () => {
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  // Maintainer request 2026-09-21: the same pens in the table under the two charts. The table is
  // the ONE place row.pens is rendered now -- the charts carry the shorter bracket -- and both are
  // composed from the same placements, so the surfaces cannot name one load's pens two ways.
  assert.match(source, /<th className="pens">\{copy\(pageContract, "table\.loads\.pens"\)\}<\/th>/);
  assert.match(source, /<td className="pens">\{row\.pens \|\| none\}<\/td>/);
  assert.ok(contract.includes('"table.loads.pens"'), "the column label is backend copy");

  // Vertical padding on the CARD, horizontal on its children — `.twrap` excluded beside
  // `.tablewrap`, because insetting the wrapper would pull the table away from its own header rule.
  assert.match(source, /className="card wtable" style=\{\{ marginTop: 12 \}\}/);
  assert.match(css, /\.wtable > :not\(\.tablewrap\):not\(\.twrap\)\{padding-left:16px;padding-right:16px\}/);
  assert.match(css, /\.wtable table\.loadwise-table th:first-child,\s*\n\.wtable table\.loadwise-table td:first-child\{padding-left:16px\}/);
  // The pens cell is the ONE column allowed to wrap; every other cell in this table is nowrap so a
  // load number or a park code can never break character-by-character.
  assert.match(css, /table\.loadwise-table td\.pens\{white-space:normal/);
});
