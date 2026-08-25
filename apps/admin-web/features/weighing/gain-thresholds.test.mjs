import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { sharesOfWhole } from "../../lib/shares.ts";

const source = readFileSync(new URL("./weights.tsx", import.meta.url), "utf8");
const card = readFileSync(new URL("./gain-threshold-card.tsx", import.meta.url), "utf8");
const scope = readFileSync(new URL("./gain-sex-scope.tsx", import.meta.url), "utf8");
const sexFilter = readFileSync(new URL("./gain-sex-filter.tsx", import.meta.url), "utf8");
const contract = readFileSync(
  new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url),
  "utf8",
);
const openapi = readFileSync(
  new URL("../../../../contracts/openapi/app-api.yaml", import.meta.url),
  "utf8",
);

/** Source with block and line comments removed, for scans that must not read documentation. */
const stripComments = (text) => text.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");

test("the four bands arrive banded from the backend, never derived by subtraction", () => {
  // The bands are DISJOINT, so the temptation is the opposite of the old one: deriving
  // a band from two cumulative counts (>180 minus >200) in the page. That reintroduces
  // one fact with two owners and drifts the moment a boundary moves. Every count is
  // printed exactly as the backend banded it.
  assert.match(source, /const gainThresholdRowsFor = \(sex: GainSex\): GainThresholdRow\[\] =>/);
  assert.match(source, /at_or_below_180_g_per_day/);
  assert.match(source, /band_180_to_200_g_per_day/);
  assert.match(source, /band_200_to_250_g_per_day/);
  assert.doesNotMatch(source, /_g_per_day\s*[-+]\s*row\./);
  // The slowest band is the last one and the only red step, so the card reads fastest
  // to slowest down every breed.
  assert.match(source, /gainThresholdSteps = \["hi", "mid", "lo", "under"\] as const/);
});

test("the four shares are apportioned so they add up to exactly 100.0%", () => {
  // The bands partition the breed, so a reader WILL add the column. Rounding each share
  // independently printed 0.0 + 12.8 + 4.3 + 83.0 = 100.1% for Sojat and made a correct
  // partition look broken. Largest remainder hands out the leftover tenths instead.
  //
  // This executes the SHIPPED helper (Node strips the types off lib/shares.ts), so it
  // fails on a real behaviour change rather than on a renamed identifier.
  const sum = (values) => values.reduce((a, b) => a + b, 0);
  assert.equal(sum(sharesOfWhole([0, 6, 2, 39], 47)), 100, "the Sojat row that printed 100.1%");
  assert.equal(sum(sharesOfWhole([26, 22, 15, 74], 137)), 100);
  assert.equal(sum(sharesOfWhole([1, 9, 4, 62], 76)), 100);
  assert.equal(sum(sharesOfWhole([1, 0, 0, 11], 12)), 100);

  // A band that really is empty still reads 0.0%: apportionment may not hand a leftover
  // tenth to a band holding no kids, which would print a share for nobody.
  assert.equal(sharesOfWhole([0, 6, 2, 39], 47)[0], 0);
  assert.equal(sharesOfWhole([0, 1, 0, 21], 22)[0], 0);

  // Every share stays within a tenth of its true value, so reaching 100 can never move a
  // visible amount onto the wrong band.
  for (const [counts, whole] of [
    [[0, 6, 2, 39], 47],
    [[26, 22, 15, 74], 137],
    [[1, 9, 4, 62], 76],
    [[1, 2, 1, 28], 32],
  ]) {
    sharesOfWhole(counts, whole).forEach((pct, index) => {
      const exact = (counts[index] / whole) * 100;
      assert.ok(Math.abs(pct - exact) <= 0.1, `band ${index} moved ${Math.abs(pct - exact)} from ${exact}`);
    });
  }

  // A breed with no kids weighed twice cannot divide by zero.
  assert.deepEqual(sharesOfWhole([0, 0, 0, 0], 0), [0, 0, 0, 0]);
  // The page must use it rather than rounding each share on its own.
  assert.match(source, /sharesOfWhole\(counts, row\.animals\)/);
  assert.doesNotMatch(source, /pct: \(count \/ row\.animals\) \* 100/);
});

test("the share is taken against the row's own backend denominator", () => {
  // `row.animals` is kids of THIS breed weighed twice — the exact key set the backend
  // filtered. Dividing by the page's animal total (which counts kids weighed once)
  // would understate every breed while still rendering a plausible percentage.
  assert.match(source, /sharesOfWhole\(counts, row\.animals\)/);
  assert.doesNotMatch(source, /\/ summary\.animals_weighed/);
});

test("the card opens on the chart and the toggle only changes the view", () => {
  // Chart is the default because the card exists to answer "is this breed growing", and
  // an ABSENT param means chart — so a link shared from the default view keeps meaning
  // chart rather than freezing whichever view it was copied in.
  assert.match(source, /one\(params, GAIN_VIEW_PARAM\) === "table" \? "table" : "chart"/);
  assert.match(source, /GAIN_VIEW_PARAM\]: null/);
  // Both views render the SAME derived rows. A second source for one fact is how two
  // views of one card start disagreeing.
  assert.equal(source.match(/gainThresholdRows/g).length >= 4, true);
  assert.doesNotMatch(source, /gain_thresholds_by_breed[\s\S]{0,400}gain_thresholds_by_breed/);
});

test("every bar states its share, its count, and the animals behind it on hover", () => {
  const bars = readFileSync(new URL("./gain-threshold-bars.tsx", import.meta.url), "utf8");
  // Share then count in the row's own cell — the percentage is what the bar encodes, the
  // count is the evidence under it, so a share standing on two kids can never read as a
  // share standing on a hundred.
  assert.match(bars, /<span className="gmv">\{mark\.pct\.toFixed\(1\)\}%<\/span>/);
  assert.match(bars, /<span className="gmn">\(\{mark\.count\.toLocaleString\("en-IN"\)\}\)<\/span>/);
  // Head count under the breed name.
  assert.match(bars, /className="gml-sub"[\s\S]{0,120}row\.animals\.toLocaleString/);
  // Count on hover, against the breed's own denominator — never a page-wide total.
  assert.match(bars, /title=\{`\$\{mark\.count\.toLocaleString\("en-IN"\)\} \$\{ofLabel\} \$\{row\.animals/);
  // Disjoint bands could legitimately be stacked, but a stack cannot be compared band
  // for band across breeds — which is the whole point of the card. One bar per band.
  assert.doesNotMatch(bars, /reduce\(/);
  assert.doesNotMatch(bars, /cumulativeWidth|stackOffset/);
});

test("the chart renders no copy of its own", () => {
  // Comments are stripped first: a doc comment may NAME a contract label as an example
  // ("e.g. \"Above 250 g/day\"") without that label being shipped copy. Scanning the raw
  // file would fail on documentation and teach the next author to delete the docs.
  const bars = stripComments(readFileSync(new URL("./gain-threshold-bars.tsx", import.meta.url), "utf8"));
  // Every string arrives resolved from the page contract; a literal here would be a label
  // the backend cannot govern.
  assert.doesNotMatch(bars, /"Above \d+ g\/day"/);
  assert.doesNotMatch(bars, /"kids"/);
  // Colours are theme tokens, so both themes stay correct by construction.
  assert.doesNotMatch(bars, /#[0-9a-fA-F]{3,8}\b/);
});

test("every visible string on the table is backend-contract copy", () => {
  for (const key of [
    "section.gain_thresholds.title",
    "view.chart",
    "view.table",
    "section.gain_thresholds.aria",
    "filter.gain_sex.label",
    "view.sex.all",
    "view.sex.male",
    "view.sex.female",
  ]) {
    assert.match(source, new RegExp(`copy\\(pageContract, "${key.replace(/\./g, "\\.")}"\\)`));
    assert.ok(contract.includes(`"${key}":`), `contract is missing copy key ${key}`);
  }
  // The three grains each carry their OWN caption, head-count noun and empty line, because each
  // names the kids that grain counted: a combined caption under Male would say the bands add up to
  // the kids weighed twice when they add up to the MALE kids weighed twice. All nine are resolved
  // in the page and passed down, so the backend still owns every word.
  for (const key of [
    "section.gain_thresholds.caption",
    "section.gain_thresholds.caption_male",
    "section.gain_thresholds.caption_female",
    "value.gain_thresholds.kids",
    "value.gain_thresholds.kids_male",
    "value.gain_thresholds.kids_female",
    "empty.gain_thresholds.body",
    "empty.gain_thresholds.male",
    "empty.gain_thresholds.female",
  ]) {
    assert.match(source, new RegExp(`copy\\(pageContract, "${key.replace(/\./g, "\\.")}"\\)`));
    assert.ok(contract.includes(`"${key}":`), `contract is missing copy key ${key}`);
  }
  // Neither client half renders copy of its own.
  for (const clientSource of [card, sexFilter]) {
    assert.doesNotMatch(stripComments(clientSource), /"(All kids|Male|Female|Sex|Counted from|No male|No female|kids)"/);
  }
  // Column headers come from the table contract, not literals in the page.
  assert.match(source, /tableLabels\(pageContract, "gain-thresholds"\)/);
  assert.match(contract, /tableP\("gain-thresholds"/);
  assert.doesNotMatch(source, /Above 250 g\/day/);
});

test("the table sits directly above the daily gain by load chart", () => {
  const table = source.indexOf('aria-label={copy(pageContract, "section.gain_thresholds.aria")}');
  const loadChart = source.indexOf('copy(pageContract, "chart.load.title")');
  assert.ok(table > 0 && loadChart > 0);
  assert.ok(table < loadChart, "the gain-mark table must render before the load chart");
});

test("the contract states each kid is counted once, in both the caption and the schema", () => {
  // The previous marks OVERLAPPED and the caption said so. These bands do not, and the
  // caption has to say THAT instead — a stale overlap warning would tell a reader the
  // columns cannot be added when now they must add to the denominator.
  assert.match(contract, /Each kid is counted in one band only, so the bands add up to the kids weighed twice/);
  assert.doesNotMatch(contract, /so the columns overlap and do not add up/);
  assert.match(openapi, /The four counts are DISJOINT/);
  // The slowest band is a real column with its own label, not an unlabelled remainder.
  assert.match(contract, /"column\.upto_180": *"180 g\/day or less"/);
});

test("the card counts every kid until a sex is picked, and never adds the two grains", () => {
  // Combined is the DEFAULT (maintainer, 2026-08-25): the breed comparison the card exists for is
  // the whole breed's, and an absent parameter has to keep meaning what the card showed before the
  // control existed — otherwise every shared link silently changes grain.
  assert.match(source, /const GAIN_SEX_PARAM = "gain_sex"/);
  assert.match(source, /rawGainSex === "male" \|\| rawGainSex === "female" \? rawGainSex : "all"/);
  assert.match(scope, /if \(next === "all"\) url\.searchParams\.delete\(searchParamName\)/);

  // The backend emits each breed combined AND per sex, and the grains OVERLAP. Exactly one is
  // rendered; the page must never sum a breed's male and female rows, or add a per-sex row to the
  // combined one, which would count every kid twice.
  assert.match(source, /\.filter\(\(row\) => \(row\.sex \?\? ""\) === \(sex === "all" \? "" : sex\)\)/);
  assert.doesNotMatch(stripComments(source), /gain_thresholds_by_breed[\s\S]{0,600}\breduce\(/);
  // The share stays row-local: a male share is taken against that row's own denominator, which is
  // the male kids weighed twice, never against the breed's combined total.
  assert.match(source, /sharesOfWhole\(counts, row\.animals\)/);
  // The figures view reads the SAME grain, so switching to the table cannot hand back every kid.
  assert.match(card, /view === "chart" \?[\s\S]{0,400}rows=\{grain\.rows\}/);
  assert.match(card, /grain\.rows\.map\(\(row\) => \(/);
});

test("picking a sex costs no page load, because all three grains are already here", () => {
  // Park, Period and Weighing change what the SERVER must fetch, so they belong in the URL and
  // cost a render. This one does not: the backend sends all three grains in ONE response, and
  // routing it through the URL re-rendered the whole page — every card, every read — to show rows
  // the reader already had (1.4-5.7s per switch on the local stack).
  for (const grain of ['rows: gainThresholdRowsFor("all")', 'rows: gainThresholdRowsFor("male")', 'rows: gainThresholdRowsFor("female")']) {
    assert.ok(source.includes(grain), `the page must prepare the ${grain} grain up front`);
  }
  // No navigation anywhere in the control or the state it writes to: a router push, an anchor or a
  // Link is the round trip being removed. The URL is still kept in step so a link carries the grain.
  for (const clientSource of [scope, sexFilter, card]) {
    assert.doesNotMatch(clientSource, /useRouter|router\.(push|replace)|next\/link|<a\b/);
  }
  assert.match(scope, /window\.history\.replaceState/);
  assert.match(scope, /useState<GainSex>\(initialSex\)/);
  // The bar renders the control raw and neither reads nor writes it, so no filter-bar state can
  // start racing the grain.
  const bar = readFileSync(new URL("../../components/worklist-filters.tsx", import.meta.url), "utf8");
  assert.match(bar, /inlineTrailing\?: ReactNode;/);
  assert.match(bar, /\{inlineTrailing\}/);
});

test("the Sex control sits beside the filters and looks like one", () => {
  // Beside Weighing, in line with the fields — not pinned to the far end, which is the download
  // opener's slot, and not on a line of its own.
  const inline = source.indexOf("inlineTrailing={");
  const trailing = source.indexOf("trailing={");
  assert.ok(inline > 0 && trailing > inline, "the Sex control renders before the trailing slot");
  assert.match(source, /<GainSexFilter/);
  // Same markup as the bar's own selects, so a reader cannot tell which control costs a page load.
  assert.match(sexFilter, /className="tsize"/);
  assert.match(sexFilter, /<span className="muted">\{label\}<\/span>/);
  // The control is at the top of the page and the card ~2,600px below it, so the grain they share
  // is held by a provider around both rather than by either one.
  assert.match(source, /<GainSexProvider initialSex=\{gainSex\} searchParamName=\{GAIN_SEX_PARAM\}>/);
  assert.match(source, /<\/GainSexProvider>/);
});

test("the backend reports both grains and the wire can tell them apart", () => {
  const repo = readFileSync(
    new URL("../../../../backend/internal/weighing/adapters/postgres/weight_demographics.go", import.meta.url),
    "utf8",
  );
  // Two grouping sets, so a per-sex row and its breed's combined row come from one pass over one
  // population and cannot disagree about who was counted.
  assert.match(repo, /GROUP BY GROUPING SETS \(\(breed\), \(breed, sex\)\)/);
  // GROUPING(sex) keeps the combined row distinguishable from a kid whose register carries no sex:
  // the combined row is empty, that kid's row says so in words.
  assert.match(repo, /CASE WHEN GROUPING\(sex\) = 1 THEN ''/);
  assert.match(repo, /ELSE COALESCE\(NULLIF\(sex, ''\), 'unknown sex'\) END AS sex/);
  // The contract declares sex required, so a client cannot read a combined row as a male one by
  // finding the field absent.
  assert.match(openapi, /required: \[label, sex, animals,/);
});
