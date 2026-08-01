#!/usr/bin/env node

// check-notification-specificity.mjs
//
// Maintainer decision, 2026-08-02: every user-facing notification must be MEANINGFUL, never
// abstract. A notification Title/Body must let the reader act without opening the app: it must
// carry a park/shed/location reference, a human vaccine/work-item name, a count, and a
// farm-readable due date -- not a bare count/generic-noun sentence like
// "Vaccination(s) due soon · 3". Leadership escalations must NAME which sheds are outstanding,
// not just report a count. Applies to every notification type (vaccination, weighing, feed,
// counts), not just vaccination. See docs/decisions/2026-08-02-meaningful-notification-copy.md.
//
// This guard composes with, and does NOT duplicate, check-ui-vaccine-labels.mjs:
//   - check-ui-vaccine-labels.mjs catches raw config/protocol tokens (et_tt_adult_w2, ...)
//     leaking into ANY admin-web/Android UI string (a HUMANIZATION defect).
//   - This guard scans backend/internal/notificationbridge/** Title/Body construction for the
//     opposite defect: a string that is grammatically fine and free of raw tokens, but still
//     abstract -- no location/shed, no vaccine/work-item name, and/or no date reference (a
//     SPECIFICITY defect). A Title/Body can pass check-ui-vaccine-labels and still fail this
//     guard, and vice versa.
//
// Modes:
//   (default)     diff-scoped: scan files changed vs $NOTIFICATION_GUARD_BASE (or origin/main)
//                 that touch backend/internal/notificationbridge/**, plus always scan that
//                 directory's *.go (non-test) files as the fixed target. No relevant diff and a
//                 clean fixed-target scan -> PASS. A dirty fixed target always reports (this
//                 directory is the canonical home of notification copy, so it is not allowed to
//                 go stale even on unrelated commits).
//   --self-test   run the built-in compliant + defective fixtures and exit.
//
// Escape hatch: append `// notification-copy:ignore: <reason>` on the line.
//
// Honest blind spots (what this guard CANNOT see):
//   - Runtime string interpolation: if a Title/Body is built from a variable
//     (`title := buildTitle(...)`) whose contents are assembled elsewhere, this guard can only
//     inspect the literal it can see; it does not trace arbitrary helper functions or execute
//     the program. A helper that itself embeds park/shed/date/vaccine tokens correctly will
//     produce a source line this guard cannot verify at all (it does not fail closed on that --
//     it simply cannot see it).
//   - It cannot verify IST timezone conversion correctness -- only that a date/time field or
//     format verb (e.g. `%s` fed by a date, `businessDate`, `.Format(`) is referenced near the
//     string. A wrong timezone conversion is invisible to this guard.
//   - It only scans Go source under backend/internal/notificationbridge/**. It cannot see the
//     actual FCM/push payload rendered on-device (Android Kotlin notification channel/compat
//     builder code, or any client-side re-templating of the pushed title/body) and cannot see
//     web/toast rendering of a delivered notification.
//   - It only inspects lines that are themselves a `Title:`/`Body:` field literal (or a
//     `fmt.Sprintf(...)` assigned directly to one), then looks a few lines around THAT line for
//     park/shed/vaccine/date markers. It deliberately does not treat every nearby quoted string
//     in the same struct literal as user-facing copy (structured metadata map keys like
//     `"campaign_id": ...` sit right next to `Title:`/`Body:` in this codebase and must carry
//     raw identifiers, not human labels). Specificity assembled through deeply nested
//     conditionals or a helper function called several lines away from the field literal can
//     still be miscounted as abstract (false positive), or a rare token collision a few lines
//     away can be miscounted as present (false negative).
//   - It cannot verify that a park/shed NAME in the string is real/current data versus a stale
//     placeholder -- only that some token of the right shape appears.
//   - Detects only the Go field-literal/Sprintf shape used in this codebase today; a Title/Body
//     built through a different construction (e.g. a template file, a JSON config-driven copy
//     table) is invisible to it.
//   - It cannot detect notifications that reference a farm entity through a variable (e.g.,
//     `title := msg + parkName`) when the marker is in the variable name but not the literal;
//     the detection relies on direct string content inspection (false negative).

import { execSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const TARGET_DIR = "backend/internal/notificationbridge";

const isNotificationGo = (rel) =>
  rel.startsWith(`${TARGET_DIR}/`) && rel.endsWith(".go") && !rel.endsWith("_test.go");

// Raw config-token detection is check-ui-vaccine-labels.mjs's job; this guard does not
// duplicate the vaccine-token regex. It only checks for GENERIC snake_case-looking identifiers
// leaking as the entire user-visible noun (defensive, narrow -- the ui-vaccine-labels guard is
// the primary authority for vaccine tokens specifically).
const RAW_TOKEN_IN_STRING = /["'`][^"'`]*\b[a-z]+(?:_[a-z]+){2,}\b[^"'`]*["'`]/;

// Tokens/markers that indicate the string (or the surrounding construction) carries real
// specificity: a location reference, a vaccine/work-item name reference, a count, or a date.
const LOCATION_MARKERS = /\b(?:park|shed|partition|location|Park|Shed|Partition|Location)\b|parkName|shedName|shedLabel|partitionLabel/;
const NAME_MARKERS = /vaccineLabel|vaccine_labels|driveName|workNoun|profile\.\w*[Nn]oun|itemName|VACCINE_LABEL/;
const DATE_MARKERS = /businessDate|dueAt|due_at|\.Format\(|asOf|as_of|IST\b|dueDate|scheduledFor/;
const COUNT_MARKERS = /len\(|%d|count\b|Count\b/;

// Strings/constructions that ARE the defect: a bare count + generic noun, no location/date/name.
// e.g. "%d sheds is now live", "Vaccination(s) due soon", "N items pending".
const GENERIC_NOUN_ABSTRACT = /\b(?:due soon|is now live|are (?:now )?live|items? pending|task(?:s)? pending|record(?:s)? pending|\d+ (?:sheds?|animals?|goats?|tasks?|items?|records?) (?:is|are))\b/i;

function isProseLine(text) {
  const trimmed = text.trim();
  return trimmed.startsWith("//") || trimmed.startsWith("*") || trimmed.startsWith("/*");
}

/**
 * Given the full source of a notificationbridge Go file, find Title:/Body: field literals and
 * fmt.Sprintf(...) calls feeding them, and flag any user-facing string that reads as abstract:
 * matches the generic-noun-only pattern AND has none of location/name/date markers nearby
 * (same line plus 3 lines above, to catch a Sprintf feeding a variable used a few lines later).
 */
export function findingsForSource(source) {
  const findings = [];
  const lines = source.split("\n");

  lines.forEach((text, i) => {
    if (/notification-copy:ignore/.test(text) || isProseLine(text)) return;
    // Only inspect the Title:/Body: field-literal line itself (or a fmt.Sprintf(...) call that
    // is the direct RHS of one). Metadata map keys like "campaign_id": ... sit in the same
    // struct literal a few lines away from Title:/Body: and must NOT be treated as user-facing
    // copy just because they are nearby -- that would false-positive on structured context
    // fields, which are exactly what should carry raw identifiers.
    const isTitleOrBodyField = /\b(?:Title|Body)\s*:\s*.*["'`]|\b(?:Title|Body)\s*:\s*fmt\.Sprintf\(/.test(text);
    if (!isTitleOrBodyField) return;
    if (!/["'`]/.test(text)) return;

    const windowStart = Math.max(0, i - 3);
    const windowEnd = Math.min(lines.length, i + 2);
    const window = lines.slice(windowStart, windowEnd).join("\n");

    const hasLocation = LOCATION_MARKERS.test(window);
    const hasName = NAME_MARKERS.test(window);
    const hasDate = DATE_MARKERS.test(window);
    const hasCount = COUNT_MARKERS.test(window);
    const looksAbstract = GENERIC_NOUN_ABSTRACT.test(text);
    // Check for entity-free notifications (no farm entity markers at all)
    const hasNoFarmEntity = !hasLocation && !hasName && !hasCount && !hasDate;

    if (looksAbstract && !(hasLocation && hasDate && (hasName || hasCount))) {
      findings.push({
        line: i + 1,
        rule: "abstract-notification-copy",
        message:
          "notification Title/Body reads as an abstract count/generic-noun sentence with no park/shed/date reference nearby; name the park, shed/partition, vaccine/work-item, and a farm-readable IST due date",
      });
    }

    // Flag notifications that reference NO farm entity at all (e.g., "The proof is ready for
    // operational closure" with no park/shed/vaccine/animal/operator/date). This catches generic
    // prose that slipped past the count-only pattern. Escape hatch: use `notification-copy:ignore`
    // for genuinely entity-free system messages (health check, etc.).
    if (hasNoFarmEntity) {
      findings.push({
        line: i + 1,
        rule: "entity-free-notification-copy",
        message:
          "notification Title/Body references NO farm entity (no park, shed, vaccine, animal, operator, or date); name at least one farm context element so the operator can act without opening the app",
      });
    }

    if (RAW_TOKEN_IN_STRING.test(text) && !/vaccine_code|doseCode|dose_code|vaccineCode/.test(text)) {
      // Narrow, defensive check for a raw snake_case-looking token as the ENTIRE user-visible
      // noun in a Title/Body literal. The primary authority for vaccine tokens specifically is
      // check-ui-vaccine-labels.mjs; this only widens coverage to non-vaccine config tokens
      // inside notificationbridge Title/Body strings.
      findings.push({
        line: i + 1,
        rule: "raw-config-token-in-notification-copy",
        message: "raw snake_case config token appears to leak into a notification Title/Body; render a human label instead",
      });
    }
  });

  return findings;
}

function walkNotificationDir() {
  const dir = join(repo, TARGET_DIR);
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (!entry.isFile()) continue;
    const rel = relative(repo, join(dir, entry.name));
    if (isNotificationGo(rel)) out.push(rel);
  }
  return out;
}

function changedNotificationFiles() {
  const base = process.env.NOTIFICATION_GUARD_BASE || "origin/main";
  const ranges = [`${base}...HEAD`, "HEAD~1...HEAD"];
  for (const range of ranges) {
    try {
      const refOk = range.split("...")[0];
      execSync(`git rev-parse --verify --quiet ${refOk}^{commit}`, { cwd: repo, stdio: "ignore" });
      const out = execSync(`git diff --name-only --diff-filter=d ${range}`, { cwd: repo, encoding: "utf8" });
      return out.split("\n").map((s) => s.trim()).filter(Boolean).filter(isNotificationGo);
    } catch {
      /* try next range */
    }
  }
  return null;
}

function selfTest() {
  // Test 1: abstract count/generic-noun (existing pattern)
  const badCount = `
func x() {
	push := notify.Message{
		Title: "Vaccination(s) due soon",
		Body:  fmt.Sprintf("%d sheds is now live", len(payload.Buckets)),
	}
}
`;
  // Test 2: entity-free generic prose (new pattern)
  const badEntityFree = `
func x() {
	push := notify.Message{
		Title: "Vaccination proof verified",
		Body:  "The proof is ready for operational closure.",
	}
}
`;
  // Test 3: compliant with location + vaccine + count
  const good = `
func x() {
	push := notify.Message{
		Title: parkName + " · " + shedLabel + " · " + vaccineLabel + " due",
		Body:  fmt.Sprintf("%d goats in %s (%s) need %s by %s IST", count, parkName, shedLabel, vaccineLabel, businessDate),
	}
}
`;
  // Test 4: compliant with park context (uses parkLabel variable)
  const goodWithPark = `
func x() {
	push := notify.Message{
		Title: fmt.Sprintf("%s: vaccination approval pending", parkLabel),
		Body:  fmt.Sprintf("%d animals in %s need approval by %s", count, parkLabel, businessDate),
	}
}
`;

  const badCountFindings = findingsForSource(badCount);
  if (!badCountFindings.some((f) => f.rule === "abstract-notification-copy")) {
    throw new Error(`self-test: expected abstract-notification-copy on count-only fixture, got: ${JSON.stringify(badCountFindings)}`);
  }

  const badEntityFreeFindings = findingsForSource(badEntityFree);
  if (!badEntityFreeFindings.some((f) => f.rule === "entity-free-notification-copy")) {
    throw new Error(`self-test: expected entity-free-notification-copy on generic prose fixture, got: ${JSON.stringify(badEntityFreeFindings)}`);
  }

  const goodFindings = findingsForSource(good);
  if (goodFindings.length) {
    throw new Error(`self-test: false positive on compliant fixture: ${JSON.stringify(goodFindings)}`);
  }

  const goodWithParkFindings = findingsForSource(goodWithPark);
  if (goodWithParkFindings.length) {
    throw new Error(`self-test: false positive on park-scoped fixture: ${JSON.stringify(goodWithParkFindings)}`);
  }

  console.log("check-notification-specificity self-test: PASS");
  console.log(`  count-only defective fixture -> ${badCountFindings.length} finding(s): ${badCountFindings.map((f) => f.rule).join(", ")}`);
  console.log(`  entity-free defective fixture -> ${badEntityFreeFindings.length} finding(s): ${badEntityFreeFindings.map((f) => f.rule).join(", ")}`);
  console.log(`  compliant fixture -> ${goodFindings.length} finding(s)`);
  console.log(`  park-scoped compliant fixture -> ${goodWithParkFindings.length} finding(s)`);
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

// Always scan the fixed target directory (it must never go stale), plus report which files were
// touched by the diff for context.
const diffFiles = changedNotificationFiles();
const files = walkNotificationDir();

const findings = [];
for (const rel of files) {
  const abs = join(repo, rel);
  let source;
  try {
    source = readFileSync(abs, "utf8");
  } catch {
    continue;
  }
  for (const f of findingsForSource(source)) findings.push({ ...f, rel });
}

if (findings.length) {
  console.error(`notification-specificity: ${findings.length} abstract/defective notification string(s) in ${TARGET_DIR}`);
  for (const f of findings) console.error(`- ${f.rule} ${f.rel}:${f.line}: ${f.message}`);
  console.error("If a case is genuinely fine (e.g. a system/health-check notification with no farm entity), append `notification-copy:ignore: <reason>` on the line.");
  console.error(`(diff-detected changed files this run: ${diffFiles === null ? "no diff base resolvable" : diffFiles.length})`);
  process.exit(1);
}
console.log(`notification-specificity: ok (${files.length} file(s) scanned in ${TARGET_DIR}; no abstract count-only notification copy found)`);
