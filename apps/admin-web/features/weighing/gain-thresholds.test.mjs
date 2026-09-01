import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { sharesOfWhole } from "../../lib/shares.ts";

const source = readFileSync(new URL("./weights.tsx", import.meta.url), "utf8");
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
  assert.match(source, /const gainThresholdRows: GainThresholdRow\[\] =/);
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
    "filter.sex.label",
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
    assert.ok(contract.includes(`"${key}":`), `contract is missing copy key ${key}`);
  }
  // Caption, head-count noun and empty line are picked BY the selected filter, so the page names
  // the key rather than spelling one out. Both halves are still checked: the page resolves them
  // through copy(), and the contract carries every key it can ask for.
  assert.match(source, /sexFilter === "" \? "section\.gain_thresholds\.caption" : `section\.gain_thresholds\.caption_\$\{sexFilter\}`/);
  assert.match(source, /sexFilter === "" \? "value\.gain_thresholds\.kids" : `value\.gain_thresholds\.kids_\$\{sexFilter\}`/);
  assert.match(source, /sexFilter === "" \? "empty\.gain_thresholds\.body" : `empty\.gain_thresholds\.\$\{sexFilter\}`/);
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
  // The caption also has to name the WHOLE population the bands cover. It used to say the bands add
  // up to "the kids weighed twice", which stopped being true once whole-shed pens joined this
  // aggregate -- and a caption that under-describes its own denominator is how the page came to
  // claim daily gain used only scanned kids while most of the number was whole-shed movement.
  assert.match(contract, /Each kid is counted in one band only/);
  assert.match(contract, /sheds weighed as one total/);
  assert.doesNotMatch(contract, /so the columns overlap and do not add up/);
  assert.match(openapi, /The four counts are DISJOINT/);
  // The slowest band is a real column with its own label, not an unlabelled remainder.
  assert.match(contract, /"column\.upto_180": *"180 g\/day or less"/);
});

test("the Sex filter is a PAGE filter: every read carries it, and the page never re-derives it", () => {
  // The maintainer's rule (2026-08-26): picking Male re-reads the WHOLE page at that half. A page
  // where the shed table counts every kid while the breed card counts the male half has no true
  // number on it, so the filter travels with every read rather than being applied to one card.
  assert.match(source, /const SEX_PARAM = "sex"/);
  // MALE is the default and an absent parameter means it (maintainer request 2026-09-01); every
  // kid is the explicit `sex=all`, which the reads still see as "" -- the value the backend
  // resolver reads as "no filter", so the unfiltered page runs the query it always ran.
  assert.match(source, /rawSex === "female" \? "female" : rawSex === "all" \? "" : "male"/);
  assert.match(source, /landingWindow\(\s*\n\s*params,\s*\n\s*today,\s*\n\s*parkFilter,\s*\n\s*sexFilter,\s*\n\s*originFilter,\s*\n\s*weighingCategoryFilter,\s*\n\s*\)/);
  assert.match(source, /getShedWeights\(\{ \.\.\.scope, \.\.\.window \}\)/);
  for (const read of ["getShedWeights", "getWeighingGrowth", "getWeightDemographics", "getGrowthDirector"]) {
    assert.match(
      source,
      new RegExp(`${read}\\(\\{ \\.\\.\\.scope, \\.\\.\\.window \\}\\)`),
      `${read} must carry the sex filter`,
    );
  }
  // The rows arrive ALREADY filtered, so the card renders what it was sent and never re-selects
  // by sex — a second implementation of one rule is a second thing to keep in step.
  assert.doesNotMatch(source, /gain_thresholds_by_breed[\s\S]{0,400}row\.sex/);
  assert.doesNotMatch(stripComments(source), /gain_thresholds_by_breed[\s\S]{0,600}\breduce\(/);
  // The share stays row-local: a male share is taken against that row's own denominator.
  assert.match(source, /sharesOfWhole\(counts, row\.animals\)/);
});

test("Sex sits in the filter bar beside Weighing, defaults to Male, and carries its own All", () => {
  const bar = source.slice(source.indexOf("const filterFields"), source.indexOf("const shedColumns"));
  const weighing = bar.indexOf('param: "weighing"');
  const sex = bar.indexOf("param: SEX_PARAM");
  assert.ok(weighing > 0 && sex > weighing, "Sex must follow the Weighing filter in the bar");
  // allowAll:false and an explicit All option, the same shape the Weighing filter uses. The bar's
  // generic BLANK option would be a second spelling of "every kid" — and an absent `sex` now means
  // MALE, so that blank would quietly switch the page back to the default while claiming to show
  // everything. One All, and it is a real value.
  const sexField = bar.slice(sex);
  assert.match(sexField, /allowAll: false/);
  assert.match(sexField, /\{ value: "all", label: copy\(pageContract, "filter\.all_option"\) \}/);
  assert.match(sexField, /copy\(pageContract, "view\.sex\.male"\)/);
  assert.match(sexField, /copy\(pageContract, "view\.sex\.female"\)/);
  // The control shows "all" where the reads see "": the blank would read back as the male default
  // on the next request, so the All choice must survive the round trip as a value of its own.
  assert.match(sexField, /value: sexChoice/);
  assert.match(source, /const sexChoice = sexFilter === "" \? "all" : sexFilter/);
});

test("the backend narrows BOTH kinds of weigh, from one resolver", () => {
  const scopeFile = readFileSync(
    new URL("../../../../backend/internal/weighing/adapters/postgres/sex_scope.go", import.meta.url),
    "utf8",
  );
  // An individual weigh is claimed through its scanned tag...
  assert.match(scopeFile, /WHERE lower\(btrim\(g\.sex\)\) = \$5::text/);
  // ...and a whole-shed weigh only when its cohort is provably ONE sex. A mixed shed is claimed by
  // neither side rather than split, because one shed average cannot be divided between cohorts.
  assert.match(scopeFile, /HAVING count\(DISTINCT lower\(btrim\(g\.sex\)\)\) = 1/);
  assert.match(scopeFile, /AND min\(lower\(btrim\(g\.sex\)\)\) = \$5::text/);
  // An empty sex is "no filter", NOT "nothing matched" — the unfiltered page must run the query it
  // ran before this file existed, and must not read a goat row at all.
  assert.match(scopeFile, /if normalized == "" \|\| len\(parkIDs\) == 0 \{\n\t\treturn out, nil/);
  // An unknown value is rejected rather than silently widening the filter.
  assert.match(scopeFile, /unsupported sex filter/);

  // The other weighing reads apply the filter WITHOUT naming a herd table: they are handed an
  // opaque tag list and bucket list, which is what keeps the isolation lock intact.
  for (const file of ["shed_weights.go", "growth.go", "load_weights.go"]) {
    const read = readFileSync(
      new URL(`../../../../backend/internal/weighing/adapters/postgres/${file}`, import.meta.url),
      "utf8",
    );
    // Comments stripped first: growth.go's own header NAMES goat_identifiers to say it must never
    // join it, and failing on that documentation would teach the next author to delete the warning.
    assert.doesNotMatch(stripComments(read), /\bgoat_identifiers\b|\bFROM goats\b|\bJOIN goats\b/, `${file} must not join the herd register`);
    assert.match(read, /NOT \$\d+::bool OR/, `${file} must apply the scope it was handed`);
  }
  // And the exemption is RECORDED, by name, in the guard.
  const guard = readFileSync(new URL("../../../../tools/agent-hooks/check-weighing-free-flow-guard.mjs", import.meta.url), "utf8");
  assert.match(guard, /adapters\/postgres\/sex_scope\.go/);
});

test("the card counts whole-shed kids too, not just the ones scanned one by one", () => {
  // THE REGRESSION THIS PINS. A rewrite dropped the lump-sum arm of this aggregate and the card
  // silently lost every kid weighed as part of a whole shed: Anantapur Sheep fell from 380 to 117
  // while the page still called it "kids weighed twice". Only the individually scanned kids
  // survived, and nothing on screen said so.
  const repo = readFileSync(
    new URL("../../../../backend/internal/weighing/adapters/postgres/weight_demographics.go", import.meta.url),
    "utf8",
  );
  const gt = repo.slice(repo.indexOf("How many animals of each breed fell into each daily-gain band"), repo.indexOf(") gt),"));
  // Both arms: one row per animal with a computable gain, UNIONed with one row per
  // homogeneous-breed whole-shed weigh whose animals all land in the band its own change falls in.
  assert.match(gt, /FROM resolved_gain/);
  assert.match(gt, /UNION ALL/);
  assert.match(gt, /FROM lump_span ls JOIN shed_cohort sc/);
  assert.match(gt, /sum\(ls\.animals\) FILTER \(WHERE ls\.g_per_day > 250\)/);
  // The lump arm honours the Sex filter on the shed's own cohort, because a whole-shed weigh
  // carries no tag and can only be claimed when its residents are all one sex.
  assert.match(gt, /sc\.sexes = 1 AND lower\(btrim\(sc\.sex\)\) = \$5::text/);
  // A mixed-breed shed still joins no breed row: one shed average cannot be split across breeds.
  assert.match(gt, /WHERE sc\.breeds = 1/);
});

test("the sex filter reaches the demographics read on both arms", () => {
  const repo = readFileSync(
    new URL("../../../../backend/internal/weighing/adapters/postgres/weight_demographics.go", import.meta.url),
    "utf8",
  );
  // The per-animal arm is narrowed upstream, where the tag is already resolved to its animal...
  assert.match(repo, /WHERE \$5::text = '' OR lower\(btrim\(gt\.sex\)\) = \$5::text/);
  // ...and an unknown value is refused rather than silently widening the filter.
  assert.match(repo, /sexFilter, sexErr := normalizeSexFilter\(sex\)/);
  // An empty filter keeps every row INCLUDING tags that resolve to no animal, which is what the
  // unfiltered page has always counted.
  assert.match(repo, /\$5::text = '' OR/);
  assert.match(openapi, /required: \[label, animals,/);
});
