#!/usr/bin/env node

// date-format-guard — THE DATE DISPLAY RULE (maintainer decision 2026-09-10).
//
// Every VISIBLE date on every Goat OS surface renders DD/MM/YYYY, with slashes,
// in full. One shape, everywhere: admin-web tables, cards and CHART AXES; Android
// list chips, card subtitles and the timestamp burned into a proof video; and any
// date string the BACKEND composes for a screen (it owns visible copy).
//
// This SUPERSEDES the 2026-08-21 rule, which set DD-MM-YYYY (dashes) for admin-web
// only and allowed a compact dd-mm-yy on chart axes. Two things were wrong with it:
// the separator disagreed with the backend's own FarmDate ("02/01/2006", which
// already shipped in notification copy), and it governed one surface, so Android
// kept rendering "14 Aug" for the same drive the console called "14-08-2026". A
// reader comparing two screens saw two shapes of one fact.
//
// WHAT IS NOT A DATE, and keeps its own pattern deliberately:
//   - a TIME on its own ("HH:mm", "h:mm a") — no day to render;
//   - a MONTH heading ("MMM yyyy", "Jan 2006") — no day component, so DD/MM/YYYY
//     is undefined for it;
//   - a WEEKDAY on its own ("EEE") — a name, not a date;
//   - a WIRE/PARSE format (ISO "yyyy-MM-dd", EXIF "yyyyMMdd'T'HHmmssX", an export
//     filename, a React key, an idempotency key) — machine formats must NOT follow
//     display, and switching one to slashes would corrupt a key or a query param.
//
// FIVE CHECKS:
//   1. WEB CANARY    — apps/admin-web/lib/format.ts fmtDate/fmtDateTime must still
//      compose day/month/year with SLASHES. A refactor that flips the helper back
//      to ISO or to dashes fails here even though no feature file changed.
//   2. WEB AXIS CANARY — components/svg-series.tsx must render axis days as
//      DD/MM/YYYY, not the retired compact dd-mm-yy.
//   3. WEB BARE-DATE SCAN — a JSX TEXT node rendering a date-named field directly
//      (`<td>{row.first_purchase_date}</td>`) ships the wire's ISO string to an
//      operator's eyes. Wrap it in fmtDate()/fmtDateTime().
//   4. ANDROID PATTERN SCAN — any ofPattern(...)/SimpleDateFormat(...) whose pattern
//      carries BOTH a day and a month field is a date, and must render it as
//      "dd/MM/yyyy". This is what catches a new screen hand-writing "d MMM".
//      Plus a canary on GoatOsDates.DATE_PATTERN.
//   5. BACKEND SCAN  — a Go time layout combining a WORD month ("Jan"/"January")
//      with a day is display copy by construction; it must go through
//      biztime.FarmDate/FarmDateFromBusinessDate instead. Plus a canary on
//      biztime.FarmDateFormat.
//
// ESCAPE HATCH: put `date-format-guard:ignore: <reason>` on the line (or the line
// above) for a genuine machine/wire format. It is a reviewer-facing justification,
// not a rubber stamp.
//
// BLIND SPOTS (stated per the guard-honesty rule; review owns these):
//   - a date laundered through an intermediate variable or template literal before
//     it reaches a JSX text node;
//   - fields whose names do not end in _date/_day/_at (e.g. `captured`, `when`);
//   - Go's ISO layout "2006-01-02" is NOT flagged: it is the legitimate wire format
//     in ~200 places (SQL params, event keys, API fields), so flagging it would be
//     pure noise. An ISO date leaking into a Go SENTENCE is caught by review and by
//     the notification-specificity guard, not here;
//   - Kotlin/TS string concatenation that builds a date by hand from parts;
//   - server-rendered copy stored as data (seeded admin_ui_config_entries rows).

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const IGNORE = "date-format-guard:ignore:";

// An ignore marker counts when it sits on the line itself or anywhere in the
// contiguous comment block immediately above it, so a marker with a real
// multi-line reason works the same as a terse trailing one.
function ignored(lines, lineNo) {
  if ((lines[lineNo - 1] ?? "").includes(IGNORE)) return true;
  for (let i = lineNo - 2; i >= 0; i -= 1) {
    const text = (lines[i] ?? "").trim();
    if (!text.startsWith("//") && !text.startsWith("*") && !text.startsWith("/*")) break;
    if (text.includes(IGNORE)) return true;
  }
  return false;
}

/* ------------------------------------------------------------------ web ---- */

const DATE_FIELD = String.raw`[A-Za-z_$][\w$]*(?:\.[\w$]+)*\.(?:[\w$]*_date|[\w$]*_day|[\w$]*_at|feed_day)`;

// NOTE the `\s*` after every `>`: JSX is usually formatted with the interpolation on its OWN
// LINE (`<td className="num">\n  {row.last_weighed_date ?? ...}`), so a pattern demanding `>{`
// adjacency silently misses the common case. That gap shipped a raw ISO date into the Weights
// table's date column and was found by rendering the page, not by reading it.
const BARE_DATE_PATTERNS = [
  new RegExp(String.raw`>\s*\{\s*(${DATE_FIELD})\s*\}\s*<`, "g"),
  new RegExp(String.raw`>\s*\{\s*(${DATE_FIELD})\s*\?\?`, "g"),
  new RegExp(String.raw`>\s*\{\s*` + "`" + String.raw`[^` + "`" + String.raw`]*\$\{\s*${DATE_FIELD}\s*\}[^` + "`" + String.raw`]*` + "`" + String.raw`\s*\}<`, "g"),
  new RegExp(String.raw`>[^<{}]*\{\s*(${DATE_FIELD})\s*\}[^<{}]*<`, "g"),
  new RegExp(String.raw`>\s*\{\s*(${DATE_FIELD})\s*\}\s*[^<{}]+`, "g"),
];

export function scanSource(source) {
  const hits = [];
  const seen = new Set();
  for (const pattern of BARE_DATE_PATTERNS) {
    pattern.lastIndex = 0;
    let match;
    while ((match = pattern.exec(source)) !== null) {
      const line = source.slice(0, match.index).split("\n").length;
      const text = match[0].trim();
      const key = `${line}:${text}`;
      if (seen.has(key)) continue;
      seen.add(key);
      hits.push({ line, text });
    }
  }
  return hits;
}

export function webCanaryFailures(formatSource) {
  const failures = [];
  const fmtDate = formatSource.match(/export function fmtDate\([^]*?\n}/)?.[0] ?? "";
  const fmtDateTime = formatSource.match(/export function fmtDateTime\([^]*?\n}/)?.[0] ?? "";
  if (!fmtDate.includes("${parts.day}/${parts.month}/${parts.year}")) {
    failures.push("apps/admin-web/lib/format.ts fmtDate no longer composes DD/MM/YYYY with slashes");
  }
  if (!fmtDateTime.includes("${parts.day}/${parts.month}/${parts.year} ${parts.hour}:${parts.minute}")) {
    failures.push("apps/admin-web/lib/format.ts fmtDateTime no longer composes DD/MM/YYYY HH:MM with slashes");
  }
  return failures;
}

export function axisCanaryFailures(seriesSource) {
  const body = seriesSource.match(/const fmtDay = [^]*?\n};/)?.[0] ?? "";
  if (!body.includes("${m[3]}/${m[2]}/${m[1]}")) {
    return ["apps/admin-web/components/svg-series.tsx axis days no longer render DD/MM/YYYY"];
  }
  return [];
}

/* -------------------------------------------------------------- android ---- */

// A java.time / SimpleDateFormat pattern is a DATE when it carries both a day
// field and a month field OUTSIDE quoted literals. Quoted runs ('IST', 'T') are
// literal text and must not be read as fields.
export function isDatePattern(pattern) {
  const unquoted = pattern.replace(/'[^']*'/g, "");
  return /d/.test(unquoted) && /M/.test(unquoted);
}

export function androidPatternFailures(source) {
  const failures = [];
  const lines = source.split("\n");
  const re = /(?:ofPattern|SimpleDateFormat)\(\s*"([^"]*)"/g;
  let match;
  while ((match = re.exec(source)) !== null) {
    const pattern = match[1];
    const lineNo = source.slice(0, match.index).split("\n").length;
    if (ignored(lines, lineNo)) continue;
    if (!isDatePattern(pattern)) continue;
    if (pattern.includes("dd/MM/yyyy")) continue;
    failures.push({ line: lineNo, pattern });
  }
  return failures;
}

// A Compose state field whose name ends in "Label" is BY NAME something a person
// reads, so it must not be assigned a wire/ISO value. This is the trap that put a
// raw "2026-07-29" under the Feed Packing title: the field was called
// `targetDateLabel` but held `todayIso()`, and it was ALSO the comparison key
// against today and the date picker's value, so nobody reading the ViewModel saw
// a display bug. Name the field for what it holds (`targetDateIso`) and format it
// at the point it becomes visible.
const WIRE_VALUED = /=\s*(?:todayIso\(\)|[\w.]*[iI]so\([^)]*\)|[\w.]*\.(?:targetDate|businessDate|feedDay|dueDate)\b)/;

export function androidLabelFieldFailures(source) {
  const failures = [];
  const lines = source.split("\n");
  lines.forEach((line, i) => {
    if (line.includes(IGNORE)) return;
    const match = /(\w+Label)\s*(=[^=].*)$/.exec(line);
    if (!match) return;
    if (!WIRE_VALUED.test(match[2])) return;
    failures.push({ line: i + 1, field: match[1] });
  });
  return failures;
}

export function androidCanaryFailures(datesSource) {
  const failures = [];
  if (!/const val DATE_PATTERN: String = "dd\/MM\/yyyy"/.test(datesSource)) {
    failures.push("GoatOsDates.DATE_PATTERN is no longer \"dd/MM/yyyy\"");
  }
  if (!/const val WIRE_DATE_PATTERN: String = "yyyy-MM-dd"/.test(datesSource)) {
    failures.push("GoatOsDates.WIRE_DATE_PATTERN must stay ISO \"yyyy-MM-dd\" — wire formats never follow display");
  }
  return failures;
}

/* -------------------------------------------------------------- backend ---- */

// A Go layout that spells the month as a WORD next to a day is display copy by
// construction — no wire format uses "Jan 2". ISO "2006-01-02" is deliberately
// not flagged (see BLIND SPOTS).
const GO_WORD_MONTH_DATE = /"(?=[^"]*\b(?:Jan|January)\b)(?=[^"]*\b0?2\b)[^"]*"/;

export function goDisplayFailures(source) {
  const failures = [];
  const lines = source.split("\n");
  const re = /\.Format\(\s*("(?:[^"\\]|\\.)*")\s*\)/g;
  let match;
  while ((match = re.exec(source)) !== null) {
    const layout = match[1];
    const lineNo = source.slice(0, match.index).split("\n").length;
    if (ignored(lines, lineNo)) continue;
    if (!GO_WORD_MONTH_DATE.test(layout)) continue;
    failures.push({ line: lineNo, layout });
  }
  return failures;
}

// A date VALUE written into a copy string -- "starts on 03 Aug 2026", "from 11-08-2026",
// "e.g. ... on 12 Jun". These are not time LAYOUTS, so the .Format() scan above cannot see
// them; they were found by fetching a real /admin-web/bootstrap and scanning the payload.
//
// The discriminator is the YEAR. Every Go time layout is built from the reference instant
// (2006-01-02 15:04:05 MST), so a literal carrying any other number beside a month name --
// or a dd-mm-yyyy whose year is not 2006 -- is DATA a person reads, not a format.
const MONTH = String.raw`(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)`;
const LAYOUT_NUMBERS = new Set(["1", "2", "3", "4", "5", "6", "15", "01", "02", "03", "04", "05", "06", "07", "00", "0700", "2006", "-0700"]);

// Go layouts write the DAY as "2" or "02" and nothing else, so any other 1-2 digit number
// beside a month name is a real day. A 4-digit number beside a month is a YEAR, which makes
// the string a MONTH HEADING ("Aug 2026") -- allowed, because it has no day component.
const LAYOUT_DAYS = new Set(["2", "02"]);

// Comments are blanked first: a doc comment quoting an example ("10 Sep") is documentation,
// not copy the operator sees, and flagging it buries the real findings in noise.
function stripGoComments(source) {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "))
    .replace(/(^|[^:])\/\/[^\n]*/g, (m, p1) => p1 + " ".repeat(m.length - p1.length));
}

export function goCopyDateFailures(rawSource) {
  const failures = [];
  const source = stripGoComments(rawSource);
  // Literals are scanned on the STRIPPED source, but ignore markers are read from the RAW
  // lines -- stripping blanks the very comment the marker lives in. Line numbers match
  // because stripGoComments replaces comment characters with spaces, never with nothing.
  const lines = rawSource.split("\n");
  const literal = /"((?:[^"\\\n]|\\.)*)"/g;
  let match;
  while ((match = literal.exec(source)) !== null) {
    const text = match[1];
    const lineNo = source.slice(0, match.index).split("\n").length;
    if (ignored(lines, lineNo)) continue;

    // a dashed day-month-year date that is not the Go reference year
    const dashed = new RegExp(String.raw`\b\d{2}-\d{2}-(\d{4})\b`).exec(text);
    if (dashed && dashed[1] !== "2006") {
      failures.push({ line: lineNo, text: dashed[0] });
      continue;
    }
    // A month name beside a number that no Go layout uses. The two patterns are collected
    // INDEPENDENTLY: scanning them as one alternation consumes the month while testing the
    // leading day, so the trailing YEAR of "03 Aug 2026" is never examined -- and the year is
    // exactly what separates a real date from the 2006 reference layout.
    const before = [...text.matchAll(new RegExp(String.raw`\b(\d{1,4})\s+${MONTH}\b`, "g"))].map((m) => m[1]);
    const after = [...text.matchAll(new RegExp(String.raw`\b${MONTH}\s+(\d{1,4})\b`, "g"))].map((m) => m[1]);
    const dayLike = [...before, ...after].filter((num) => num.length <= 2);
    if (dayLike.some((num) => !LAYOUT_DAYS.has(num))) {
      failures.push({ line: lineNo, text: text.slice(0, 80) });
    }
  }
  return failures;
}

export function goCanaryFailures(biztimeSource) {
  if (!/const FarmDateFormat = "02\/01\/2006"/.test(biztimeSource)) {
    return ["backend/internal/platform/biztime FarmDateFormat is no longer \"02/01/2006\""];
  }
  return [];
}

/* ------------------------------------------------------------- self-test ---- */

function assert(condition, message) {
  if (!condition) {
    console.error(`self-test FAIL: ${message}`);
    process.exit(1);
  }
}

function selfTest() {
  // 1. web bare-date scan
  const badJsx = [
    "<td>{row.first_purchase_date}</td>",
    "<td>{d.feed_day}</td>",
    "<td>{row.last_weighed_date ?? <span>never</span>}</td>",
    "<p>Recorded {trace.created_at}</p>",
    "<span>{`${coverage.start_date} to ${coverage.end_date}`}</span>",
  ].join("\n");
  const goodJsx = [
    "<td>{fmtDate(row.first_purchase_date)}</td>",
    "<td key={d.feed_day}>{fmtDate(d.feed_day)}</td>",
    "<td>{row.last_weighed_date ? fmtDate(row.last_weighed_date) : <span>never</span>}</td>",
    "<p>Recorded {dateTime(trace.created_at)}</p>",
    "<span>{coverage.start_date && coverage.end_date ? `${fmtDate(coverage.start_date)} to ${fmtDate(coverage.end_date)}` : '—'}</span>",
  ].join("\n");
  assert(scanSource(badJsx).length === 5, "bare date text nodes not flagged");
  // the real-world formatting that slipped past the original pattern: interpolation on its own line
  assert(scanSource('<td className="num">\n  {row.last_weighed_date ?? (<span>never</span>)}\n</td>').length === 1,
    "newline-separated bare date not flagged (the Weights table defect)");
  assert(scanSource('<td className="num">\n  {fmtDate(row.last_weighed_date)}\n</td>').length === 0,
    "newline-separated WRAPPED date wrongly flagged");
  assert(scanSource(goodJsx).length === 0, "wrapped/attribute dates wrongly flagged");

  // 2. web canary — ISO and the RETIRED DASH form must both fail
  const slash = 'export function fmtDate(iso) {\nreturn `${parts.day}/${parts.month}/${parts.year}`;\n}\nexport function fmtDateTime(iso) {\nreturn `${parts.day}/${parts.month}/${parts.year} ${parts.hour}:${parts.minute}`;\n}';
  const dash = 'export function fmtDate(iso) {\nreturn `${parts.day}-${parts.month}-${parts.year}`;\n}\nexport function fmtDateTime(iso) {\nreturn `${parts.day}-${parts.month}-${parts.year} ${parts.hour}:${parts.minute}`;\n}';
  const iso = 'export function fmtDate(iso) {\nreturn `${parts.year}-${parts.month}-${parts.day}`;\n}\nexport function fmtDateTime(iso) {\nreturn `${parts.day}/${parts.month}/${parts.year} ${parts.hour}:${parts.minute}`;\n}';
  assert(webCanaryFailures(slash).length === 0, "slash fmtDate wrongly flagged");
  assert(webCanaryFailures(dash).length === 2, "retired DD-MM-YYYY dash form not caught");
  assert(webCanaryFailures(iso).length === 1, "ISO fmtDate canary not caught");

  // 3. axis canary — the retired compact dd-mm-yy must fail
  assert(axisCanaryFailures("const fmtDay = (label) => {\nreturn m ? `${m[3]}/${m[2]}/${m[1]}` : label;\n};").length === 0, "slash axis wrongly flagged");
  assert(axisCanaryFailures("const fmtDay = (label) => {\nreturn m ? `${m[3]}-${m[2]}-${m[1]}` : label;\n};").length === 1, "retired compact dd-mm-yy axis not caught");

  // 4. android — every retired pattern fails, every non-date passes
  for (const p of ["d MMM", "EEE d MMM", "MMM d, yyyy h:mm:ss a", "d MMM yyyy, h:mm a", "d MMM · HH:mm", "dd-MM-yyyy"]) {
    assert(androidPatternFailures(`DateTimeFormatter.ofPattern("${p}")`).length === 1, `android date pattern "${p}" not flagged`);
  }
  for (const p of ["HH:mm", "h:mm a", "HH:mm:ss", "MMM yyyy", "dd/MM/yyyy", "dd/MM/yyyy, h:mm a", "EEE dd/MM/yyyy", "dd/MM/yyyy h:mm:ss a"]) {
    assert(androidPatternFailures(`DateTimeFormatter.ofPattern("${p}")`).length === 0, `android non-date/compliant pattern "${p}" wrongly flagged`);
  }
  // a quoted literal must not be read as a field: "h:mm a 'IST'" has no month
  assert(androidPatternFailures(`ofPattern("h:mm a 'IST'")`).length === 0, "quoted literal read as a month field");
  // ADVERSARIAL: the escape hatch works, and ONLY with a reason on the right line
  assert(androidPatternFailures(`ofPattern("yyyyMMdd'T'HHmmssX") // ${IGNORE} EXIF parse format`).length === 0, "ignore marker not honoured");
  assert(androidPatternFailures(`ofPattern("yyyyMMdd'T'HHmmssX")`).length === 1, "machine pattern passes without a stated reason");
  assert(
    androidPatternFailures(`// ${IGNORE} parses a GCS signed URL,\n// a wire format fixed by Google.\nofPattern("yyyyMMdd'T'HHmmssX")`).length === 0,
    "ignore marker in a multi-line comment block not honoured",
  );
  assert(
    androidPatternFailures(`// unrelated comment\n// another unrelated comment\nofPattern("d MMM")`).length === 1,
    "unmarked comment block wrongly treated as an exemption",
  );
  assert(androidCanaryFailures('const val DATE_PATTERN: String = "dd/MM/yyyy"\nconst val WIRE_DATE_PATTERN: String = "yyyy-MM-dd"').length === 0, "good GoatOsDates canary flagged");
  assert(androidCanaryFailures('const val DATE_PATTERN: String = "dd-MM-yyyy"\nconst val WIRE_DATE_PATTERN: String = "yyyy-MM-dd"').length === 1, "dashed DATE_PATTERN not caught");
  assert(androidCanaryFailures('const val DATE_PATTERN: String = "dd/MM/yyyy"\nconst val WIRE_DATE_PATTERN: String = "dd/MM/yyyy"').length === 1, "wire pattern switched to display not caught");

  // 4b. a *Label field holding a wire value — the raw "2026-07-29" defect
  assert(androidLabelFieldFailures("targetDateLabel = todayIso(),").length === 1, "Label field assigned todayIso() not flagged");
  assert(androidLabelFieldFailures("feedForDateLabel = feedDayIso(selection.targetDate),").length === 1, "Label field assigned an Iso() helper not flagged");
  assert(androidLabelFieldFailures("targetDateLabel = selection.targetDate,").length === 1, "Label field assigned a wire date field not flagged");
  assert(androidLabelFieldFailures("targetDateIso = todayIso(),").length === 0, "honestly-named Iso field wrongly flagged");
  assert(androidLabelFieldFailures("dateLabel = GoatOsDates.fromWireDate(day),").length === 0, "formatted Label field wrongly flagged");
  assert(androidLabelFieldFailures("if (targetDateLabel == todayIso()) return").length === 0, "a comparison wrongly read as an assignment");

  // 5. backend — word-month display layouts fail; ISO wire passes
  assert(goDisplayFailures('t.Format("Jan 2")').length === 1, "Go \"Jan 2\" not flagged");
  assert(goDisplayFailures('t.Format("January 2, 2006")').length === 1, "Go \"January 2, 2006\" not flagged");
  assert(goDisplayFailures('t.Format("02 Jan 2006 · 15:04")').length === 1, "Go \"02 Jan 2006\" not flagged");
  assert(goDisplayFailures('t.Format("2006-01-02")').length === 0, "Go ISO wire layout wrongly flagged");
  assert(goDisplayFailures('t.Format("Jan 2006")').length === 0, "Go month heading wrongly flagged");
  assert(goDisplayFailures('t.Format("02/01/2006")').length === 0, "Go FarmDate layout wrongly flagged");
  assert(goDisplayFailures(`t.Format("Jan 2") // ${IGNORE} legacy export header`).length === 0, "Go ignore marker not honoured");
  // 5b. a date VALUE in copy — the class the live-payload E2E found
  assert(goCopyDateFailures('x := "the default period starts on 03 Aug 2026, where"').length === 1, "copy date \"03 Aug 2026\" not flagged");
  assert(goCopyDateFailures('x := "\u20b9 per day \u00b7 from 11-08-2026"').length === 1, "copy date \"11-08-2026\" not flagged");
  assert(goCopyDateFailures('x := "e.g. ultrasound-confirmed pregnant on 12 Jun"').length === 1, "copy date \"12 Jun\" not flagged");
  assert(goCopyDateFailures('x := "starts on 03/08/2026"').length === 0, "compliant copy date wrongly flagged");
  // Go time LAYOUTS must stay clean — they are built from the 2006-01-02 reference instant
  assert(goCopyDateFailures('t.Format("02/01/2006")').length === 0, "FarmDate layout wrongly flagged as copy");
  assert(goCopyDateFailures('t.Format("2006-01-02")').length === 0, "ISO layout wrongly flagged as copy");
  assert(goCopyDateFailures('t.Format("2 Jan 2006")').length === 0, "a time layout wrongly flagged as copy (the .Format scan owns it)");
  assert(goCopyDateFailures('t.Format("Jan 2006")').length === 0, "month-heading layout wrongly flagged as copy");
  assert(goCopyDateFailures(`x := "from 11-08-2026" // ${IGNORE} historical incident date`).length === 0, "copy ignore marker not honoured");
  // a MONTH HEADING has no day component and is explicitly allowed by the rule
  assert(goCopyDateFailures('x := "Aug 2026"').length === 0, "month heading wrongly flagged");
  assert(goCopyDateFailures('m.Label = t.Format("Jan 2006")').length === 0, "month-heading layout wrongly flagged");
  // a doc comment quoting an example is documentation, not copy
  assert(goCopyDateFailures('// DateLabel is the short form ("10 Sep"), not copy\nvar x = 1').length === 0, "doc comment wrongly flagged as copy");
  assert(goCopyDateFailures('/* block: e.g. "12 Jun" */\nvar x = 1').length === 0, "block comment wrongly flagged as copy");
  // ...but a real copy literal on a line with a trailing comment still fails
  assert(goCopyDateFailures('x := "starts on 03 Aug 2026" // caption').length === 1, "copy date beside a comment not flagged");

  assert(goCanaryFailures('const FarmDateFormat = "02/01/2006"').length === 0, "good FarmDateFormat flagged");
  assert(goCanaryFailures('const FarmDateFormat = "02-01-2006"').length === 1, "dashed FarmDateFormat not caught");

  console.log("date-format-guard self-test: PASS");
}

/* ------------------------------------------------------------------ main ---- */

function tracked(...paths) {
  return execFileSync("git", ["ls-files", ...paths], { cwd: repo, encoding: "utf8" })
    .split("\n")
    .filter(Boolean);
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }
  const failures = [];

  // 1 + 2. web canaries
  failures.push(...webCanaryFailures(readFileSync(resolve(repo, "apps/admin-web/lib/format.ts"), "utf8")));
  failures.push(...axisCanaryFailures(readFileSync(resolve(repo, "apps/admin-web/components/svg-series.tsx"), "utf8")));

  // 3. web bare-date scan
  for (const file of tracked("apps/admin-web/features", "apps/admin-web/components", "apps/admin-web/app")) {
    if (!file.endsWith(".tsx") || file.includes(".test.")) continue;
    for (const hit of scanSource(readFileSync(resolve(repo, file), "utf8"))) {
      failures.push(`${file}:${hit.line}: bare ISO date in JSX text — wrap in fmtDate()/fmtDateTime(): ${hit.text}`);
    }
  }

  // 4. android
  const datesPath = "apps/goatos-android/core/core-common/src/main/kotlin/sg/mesha/goatos/core/common/datetime/GoatOsDates.kt";
  failures.push(...androidCanaryFailures(readFileSync(resolve(repo, datesPath), "utf8")));
  for (const file of tracked("apps/goatos-android")) {
    if (!file.endsWith(".kt") || file.includes("/test/") || file.includes("/androidTest/")) continue;
    const androidSource = readFileSync(resolve(repo, file), "utf8");
    for (const hit of androidPatternFailures(androidSource)) {
      failures.push(`${file}:${hit.line}: date pattern "${hit.pattern}" — render DD/MM/YYYY via GoatOsDates (or mark ${IGNORE} <reason> for a wire format)`);
    }
    for (const hit of androidLabelFieldFailures(androidSource)) {
      failures.push(`${file}:${hit.line}: ${hit.field} is named for a reader but holds a wire/ISO value — rename it (…Iso) and format at the point it becomes visible`);
    }
  }

  // 5. backend
  failures.push(...goCanaryFailures(readFileSync(resolve(repo, "backend/internal/platform/biztime/biztime.go"), "utf8")));
  for (const file of tracked("backend")) {
    if (!file.endsWith(".go") || file.endsWith("_test.go")) continue;
    const goSource = readFileSync(resolve(repo, file), "utf8");
    for (const hit of goDisplayFailures(goSource)) {
      failures.push(`${file}:${hit.line}: display layout ${hit.layout} — use biztime.FarmDate/FarmDateFromBusinessDate (or mark ${IGNORE} <reason>)`);
    }
    for (const hit of goCopyDateFailures(goSource)) {
      failures.push(`${file}:${hit.line}: a date written into copy is not DD/MM/YYYY: "${hit.text}"`);
    }
  }

  if (failures.length > 0) {
    console.error("date-format-guard: FAIL");
    for (const failure of failures) console.error(`  - ${failure}`);
    console.error("");
    console.error("  THE RULE: every visible date renders DD/MM/YYYY (maintainer decision 2026-09-10).");
    console.error("  Web: lib/format.ts fmtDate/fmtDateTime. App: GoatOsDates. Backend copy: biztime.FarmDate.");
    console.error("  Wire formats (ISO, keys, filenames) stay as they are.");
    process.exit(1);
  }
  console.log("date-format-guard: PASS");
}

if (process.argv[1] && import.meta.url.endsWith(process.argv[1].split("/").pop())) main();
