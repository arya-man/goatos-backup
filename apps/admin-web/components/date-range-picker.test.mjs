import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pickerSource = readFileSync(new URL("./date-range-picker.tsx", import.meta.url), "utf8");
const barSource = readFileSync(new URL("./worklist-filters.tsx", import.meta.url), "utf8");

test("the picker owns the calendar and nothing else — hosts write their own URL", () => {
  // Presentational on purpose. The moment it reaches for the router it stops being shareable, and
  // the two hosts (Actions on vd_from/vd_to landing today, Weighing on wt_from/wt_to landing 30
  // days back) would each grow their own calendar — the shared-helper drift OL-7 documents.
  assert.doesNotMatch(pickerSource, /useRouter|useSearchParams/);
  assert.match(pickerSource, /onChange: \(from: string, to: string\) => void/);
});

test("the trigger always names the date, never the word Today", () => {
  // A chip reading "Today" made the reader work out which business day they were looking at, and
  // read identically on a screenshot taken a week earlier. labels.today survives on the footer
  // button, which JUMPS to today — that is a verb, not a value.
  assert.match(pickerSource, /const triggerValue =\s*\n\s*from === to \? formatShort\(from\)/);
  assert.doesNotMatch(pickerSource, /\? labels\.today\b/);
  assert.match(pickerSource, /onClick=\{\(\) => commit\(today, today\)\}/);
});

test("a future day cannot be requested", () => {
  // The reads behind both hosts reject a future business date, so a control able to ask for one is
  // a trap.
  assert.match(pickerSource, /const future = key > today;/);
  assert.match(pickerSource, /disabled=\{future \|\| beforeFloor\}/);
  assert.match(pickerSource, /if \(key > today\) return;/);
});

test("a day before the host's history floor cannot be requested either", () => {
  // Optional per host: Herd Analytics' history starts 2026-08-01, so its calendar disables the
  // days before that the same way every calendar disables the days after today. Hosts that pass
  // no minDate keep every past day selectable.
  assert.match(pickerSource, /const beforeFloor = minDate \? key < minDate : false;/);
  assert.match(pickerSource, /if \(minDate && key < minDate\) return;/);
});

test("hosts can mark domain-specific days without giving the picker routing knowledge", () => {
  assert.match(pickerSource, /markerDates = \[\]/);
  assert.match(pickerSource, /markerFetchPath/);
  assert.match(pickerSource, /const markerDateSet = useMemo\(\(\) => new Set\(visibleMarkerDates\), \[visibleMarkerDates\]\);/);
  assert.match(pickerSource, /const marked = markerDateSet\.has\(key\);/);
  assert.match(pickerSource, /className="top-date-marker"/);
  assert.match(pickerSource, /className="top-date-marker-help"/);
  assert.match(pickerSource, /className="top-date-marker-tip"/);
  assert.match(pickerSource, /labels\.markerHint/);
  assert.match(pickerSource, /const \[fetchedMarkerDates, setFetchedMarkerDates\] = useState<readonly string\[\]>\(\[\]\);/);
  assert.match(pickerSource, /const visibleMarkerDates = markerFetchPath \? \(markerMonthStartsInFuture \? \[\] : fetchedMarkerDates\) : markerDates;/);
  assert.doesNotMatch(pickerSource, /const markerDatesKey = markerDates\.join/);
  assert.doesNotMatch(pickerSource, /setVisibleMarkerDates/);
  assert.doesNotMatch(pickerSource, /\[cursor, markerDates, markerFetchPath, today\]/);
});

test("marker fetches use the visible calendar month, not the selected report span", () => {
  assert.match(pickerSource, /const fromKey = monthStartKey\(cursor\);/);
  assert.match(pickerSource, /const endKey = monthEndKey\(cursor\);/);
  assert.match(pickerSource, /const toKey = endKey > today \? today : endKey;/);
  assert.match(pickerSource, /url\.searchParams\.set\("from", fromKey\);/);
  assert.match(pickerSource, /url\.searchParams\.set\("to", toKey\);/);
  assert.doesNotMatch(pickerSource, /url\.searchParams\.set\("from", from\)/);
  assert.doesNotMatch(pickerSource, /url\.searchParams\.set\("to", to\)/);
});

test("a range is two clicks and stays ordered whichever end is picked first", () => {
  assert.match(pickerSource, /if \(!rangeStart\) \{\s*\n\s*setRangeStart\(key\);/);
  assert.match(pickerSource, /if \(key < rangeStart\) commit\(key, rangeStart\);\s*\n\s*else commit\(rangeStart, key\);/);
});

test("opening the calendar scrolls it into view on a short window", () => {
  // A filter row sits well down the page, so on a 700px-tall window the last weeks and the Today
  // button open below the fold. "nearest" scrolls only as far as needed and is a no-op when it fits.
  assert.match(pickerSource, /onToggle=/);
  assert.match(pickerSource, /scrollIntoView\(\{ block: "nearest"/);
});

test("the board card does not clip the calendar popover", () => {
  const css = readFileSync(new URL("../app/mesha-theme.css", import.meta.url), "utf8");
  // `.card{overflow:hidden}` is declared LATER in the file and is equally specific to `.vr-board`,
  // so a single-class override loses and the calendar gets amputated at the card's bottom edge
  // (~200px cut with an empty board: the last two weeks and the Today button). The two-class
  // selector is what actually wins. Reproduced live on 2026-08-12, both before and after the first
  // one-class attempt.
  assert.match(css, /\.card\.vr-board\{overflow:visible\}/);
  assert.match(css, /\.card\{background:var\(--panel\)/);
});

test("date text is deterministic across server and browser locales", () => {
  // Ambient-locale formatting renders different characters on the server and the hydrated client,
  // which tears the tree down. Same rule the top-bar picker carries.
  assert.doesNotMatch(pickerSource, /new Intl\.DateTimeFormat\(undefined,/);
  assert.match(pickerSource, /const DATE_DISPLAY_LOCALE = "en-GB";/);
  assert.equal(
    pickerSource.match(/new Intl\.DateTimeFormat\(DATE_DISPLAY_LOCALE,/g)?.length,
    4,
    "every date label rendered by the picker must use the same explicit locale",
  );
});

test("a span is written as one filter, and its default window is expressed by absence", () => {
  // Half a window is a guaranteed 400 from a read that takes from/to, so writing the ends in two
  // pushes would send the page through an error state on the way to a valid one — the same
  // reasoning as the compare control.
  assert.match(barSource, /function applyRange\(/);
  assert.match(barSource, /next\.set\(field\.param, from\);\s*\n\s*next\.set\(field\.toParam, to\);/);
  // Landing back on the default CLEARS both, so a shared link keeps meaning "the last 30 days".
  assert.match(
    barSource,
    /if \(from === field\.defaultFrom && to === field\.defaultTo\) \{\s*\n\s*next\.delete\(field\.param\);\s*\n\s*next\.delete\(field\.toParam\);/,
  );
  // Clear all resets BOTH ends; forgetting toParam leaves a half window the backend rejects.
  assert.match(barSource, /if \(field\.kind === "daterange"\) next\.delete\(field\.toParam\);/);
});
