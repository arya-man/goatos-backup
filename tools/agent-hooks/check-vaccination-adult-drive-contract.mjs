#!/usr/bin/env node
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

function read(relative) {
  return readFileSync(resolve(repo, relative), "utf8");
}

function functionSource(source, marker) {
  const start = source.indexOf(marker);
  if (start < 0) return "";
  const next = source.indexOf("\nfunc ", start + marker.length);
  return source.slice(start, next < 0 ? source.length : next);
}

function findings(sources) {
  const problems = [];
  const dueAt = functionSource(sources.generation, "func dueAt(");
  const campaignOverrides = functionSource(sources.generation, "func campaignDueOverrides(");
  const placement = functionSource(sources.generation, "func hasPhysicalCampaignPlacement(");
  const planner = functionSource(sources.planner, "func (p OperatorDrivePlanner) planOneDay(");
  const configuredCap = functionSource(sources.planner, "func configuredOperatorCap(");
  const parkAdmission = functionSource(sources.parkConsolidation, "func admitParkRouteChunks(");
  const parkGroups = functionSource(sources.parkConsolidation, "func parkRouteGroupsByMovability(");
  const routeRank = functionSource(sources.parkConsolidation, "func vaccinationRouteShedRank(");

  if (
    !dueAt.includes('case "manual_campaign":') ||
    !dueAt.includes("opts.campaignDueByGoat[campaignDueGoatKey(versionID, rule.RuleID, g.GoatID)]") ||
    !dueAt.includes("return campaignDue, true, false")
  ) {
    problems.push("adult blank-history manual_campaign rows must materialize during normal cohort generation");
  }
  if (
    !placement.includes('strings.TrimSpace(g.ParkID) != ""') ||
    !placement.includes('strings.TrimSpace(g.ShedID) != ""') ||
    placement.includes("PartitionLabel")
  ) {
    problems.push("adult campaign eligibility must require physical park/shed placement without requiring partition metadata");
  }
  if (
    !campaignOverrides.includes("isRecurringCampaignRule(rule)") ||
    !campaignOverrides.includes("!latestReady.After(earliestSafe)") ||
    !campaignOverrides.includes("out[member.goatKey] = cohortDate") ||
    !campaignOverrides.includes("campaignDate = cohortDate")
  ) {
    problems.push("overlapping recurring adult repeat windows must place history-backed and blank-history rows on one shared drive date");
  }
  if (campaignOverrides.includes("rule.OffsetDays")) {
    problems.push("blank-history normal-drive clubbing must not introduce a separate offset-based campaign date");
  }
  if (
    !sources.generation.includes("supersededCampaignKey != key") ||
    !sources.generation.includes('"adult_campaign_date_realigned"')
  ) {
    problems.push("adult campaign realignment must cancel the stale split-date obligation");
  }
  if (
    !sources.generation.includes('"vaccine_history_outranks_adult_campaign"') ||
    !sources.generation.includes("stableAdultCampaignObligationKey(tenantID, versionID, rule, g)")
  ) {
    problems.push("accepted history must retire any stable blank-history adult campaign row before repeat generation");
  }
  if (
    !sources.generation.includes("cohortAlignedCampaignByGoat") ||
    !sources.generation.includes("RealignOpenObligationForGeneration(ctx, tenantID, key, due")
  ) {
    problems.push("late-arriving repeat history must realign an existing stable blank-history row onto the normal drive");
  }
  const safeThrough = campaignOverrides.indexOf("safeThrough := due");
  const readinessClamp = campaignOverrides.indexOf("if due.Before(campaignStart)", safeThrough);
  if (safeThrough < 0 || readinessClamp < safeThrough) {
    problems.push("adult campaign readiness clamping must not extend the rule-authored safe window");
  }

  const residual = planner.indexOf("if choice < 0 {");
  const wholeShed = planner.indexOf("total <= configuredOperatorCap", residual);
  const carry = planner.indexOf("unscheduled = append(unscheduled, group.blocks...)", residual);
  const partitionFallback = planner.indexOf("for _, block := range group.blocks", residual);
  if (
    residual < 0 ||
    wholeShed < residual ||
    carry < wholeShed ||
    partitionFallback < carry
  ) {
    problems.push("a sub-cap physical shed must carry whole instead of splitting partitions into residual capacity");
  }
  const actualCapReturn = configuredCap.indexOf("if maxCap > 0");
  const requestFallback = configuredCap.indexOf("if req.ConfiguredOperatorCap > 0");
  if (
    actualCapReturn < 0 ||
    requestFallback < actualCapReturn ||
    !sources.sweeper.includes("configuredCap := int(operator.ConfiguredCap)") ||
    !sources.sweeper.includes("ConfiguredCap: configuredCap")
  ) {
    problems.push("HRMS operator configured caps must take precedence over the request-level fallback");
  }
  if (
    sources.parkConsolidation.includes("expandWholeParkRoutePartitions(") ||
    sources.preflight.includes("expandWholeParkRoutePartitions(")
  ) {
    problems.push("whole-shed packing must never re-add vaccine obligations rejected by the visit shot cap");
  }
  if (
    !parkAdmission.includes("int32(group.targetCount) <= configuredAnimalCap") ||
    !parkGroups.includes("key := physicalShed\n") ||
    parkGroups.includes('physicalShed + "\\x00" + ruleID')
  ) {
    problems.push("park pre-batching must keep a sub-cap physical shed whole across catch-up and repeat rule rows");
  }
  if (
    !routeRank.includes('case "gandhi":\n\t\treturn 10') ||
    !routeRank.includes('case "godel 1":\n\t\treturn 20') ||
    !routeRank.includes('case "godel 2":\n\t\treturn 30') ||
    !routeRank.includes('case "mandela 2":\n\t\treturn 40') ||
    !routeRank.includes('case "old yashoda":\n\t\treturn 50')
  ) {
    problems.push("CPT route order must reproduce the agreed Gandhi-first 193/131 calendar split");
  }

  const administeredAtCloseoutReads = sources.verification.split("SELECT min(vc.administered_at)").length - 1;
  if (
    !sources.verification.includes("completed_at = COALESCE(") ||
    administeredAtCloseoutReads < 2
  ) {
    problems.push("submission and batch closeout must both complete obligations from vaccination_completions.administered_at");
  }
  if (
    !sources.verification.includes("COUNT(*)::int AS completion_count") ||
    !sources.verification.includes("COUNT(DISTINCT vc.goat_id)::int AS total_count") ||
    !sources.verification.includes("WHERE proof_count = completion_count") ||
    !sources.verification.includes("approved_completion_count = completion_count") ||
    !sources.verification.includes("SELECT COUNT(*)::int\nFROM vaccination_completions\nWHERE tenant_id = $1::uuid")
  ) {
    problems.push("vaccination drive display totals must count distinct animals while closeout coverage counts every vaccine completion");
  }
  if (
    !sources.verification.includes("'approved'::text AS status") ||
    !sources.verification.includes("vc.status = 'accepted'") ||
    !sources.verification.includes("AND vc.batch_id = $2::uuid") ||
    !sources.verification.includes("AND status IN ('recorded', 'accepted')") ||
    !sources.verification.includes("AND vc.status IN ('recorded', 'accepted')")
  ) {
    problems.push("closeout must use only active recorded/accepted completions while retaining detached accepted proof");
  }
  if (
    !sources.calendar.includes("obligation_logical_drive_full_membership AS (") ||
    !sources.calendar.includes("obligation_logical_drive_rollup AS (") ||
    !sources.calendar.includes("count(DISTINCT m.animal_id)::int AS drive_total") ||
    !sources.calendar.includes(") = k.logical_window_start") ||
    !sources.calendar.includes(") = k.logical_window_end") ||
    !sources.calendar.includes("'drive_name', obl_summary.drive_name") ||
    !sources.calendar.includes("'drive_total', obl_summary.drive_total") ||
    !sources.calendarUI.includes("summary.drive_name") ||
    !sources.calendarUI.includes("summary.drive_total")
  ) {
    problems.push("Calendar must expose and render one shared logical drive name and cross-day animal total");
  }
  if (
    !sources.booster.includes("repeatDueAfterCompletion(*current, in.AdministeredAt)") ||
    !sources.booster.includes("businessDayStart(in.AdministeredAt).AddDate")
  ) {
    problems.push("booster/repeat scheduling must anchor to operator-submitted administered_at");
  }
  if (
    !sources.skill.includes("automatically join the normal adult drive") ||
    !sources.skill.includes("must never\n  replace the administration date")
  ) {
    problems.push("goatos-build skill must preserve automatic adult drive membership and administered_at anchoring");
  }
  if (
    !sources.rules.includes("no separate manual-campaign trigger or approval") ||
    !sources.rules.includes("carries intact to the next operator-day")
  ) {
    problems.push("vaccination rules must document automatic adult catch-up and whole-shed carryover");
  }
  return problems;
}

function loadSources() {
  return {
    generation: read("backend/internal/vaccination/app/generation.go"),
    planner: read("backend/internal/vaccinationexecution/app/operator_drive_planner.go"),
    sweeper: read("backend/internal/obligation/app/sweeper.go"),
    parkConsolidation: read("backend/internal/obligation/app/park_consolidation.go"),
    preflight: read("backend/internal/obligation/app/preflight.go"),
    verification: read("backend/internal/verification/adapters/postgres/repository.go"),
    calendar: read("backend/internal/calendar/adapters/postgres/canonical_read.go"),
    calendarUI: read("apps/admin-web/features/calendar/calendar-drive-card.tsx"),
    booster: read("backend/internal/vaccination/app/booster.go"),
    skill: read(".agents/skills/goatos-build/SKILL.md"),
    rules: read("docs/preventive-care-vaccination/vaccination-rules.md"),
  };
}

if (process.argv.includes("--self-test")) {
  const good = loadSources();
  if (findings(good).length) {
    console.error("vaccination adult-drive contract guard self-test requires the canonical good fixture");
    process.exit(1);
  }
  const adversarial = [
    { ...good, generation: good.generation.replace("return campaignDue, true, false", "return time.Time{}, false, false") },
    { ...good, generation: good.generation.replace('strings.TrimSpace(g.ShedID) != ""', 'strings.TrimSpace(g.ShedID) != "" && strings.TrimSpace(g.PartitionLabel) != ""') },
    { ...good, generation: good.generation.replace("out[member.goatKey] = cohortDate", "delete(out, member.goatKey)") },
    { ...good, generation: good.generation.replace("isRecurringCampaignRule(rule)", "true") },
    { ...good, generation: good.generation.replace("campaignDate := campaignStart", "campaignDate := campaignStart.AddDate(0, 0, int(rule.OffsetDays))") },
    { ...good, generation: good.generation.replace('"adult_campaign_date_realigned"', '"left_stale"') },
    { ...good, generation: good.generation.replace('"vaccine_history_outranks_adult_campaign"', '"history_left_duplicate"') },
    { ...good, generation: good.generation.replace("RealignOpenObligationForGeneration(ctx, tenantID, key, due", "ReopenDeferredObligationForGeneration(ctx, tenantID, key, due") },
    { ...good, generation: good.generation.replace("safeThrough := due", "safeThrough := campaignStart") },
    { ...good, planner: good.planner.replace("total <= configuredOperatorCap", "total > configuredOperatorCap") },
    { ...good, planner: good.planner.replace("if maxCap > 0 {\n\t\treturn maxCap\n\t}\n\tif req.ConfiguredOperatorCap > 0", "if req.ConfiguredOperatorCap > 0 {\n\t\treturn req.ConfiguredOperatorCap\n\t}\n\tif maxCap > 0") },
    { ...good, preflight: `${good.preflight}\nfunc unsafe() { expandWholeParkRoutePartitions() }\n` },
    { ...good, parkConsolidation: good.parkConsolidation.replace("int32(group.targetCount) <= configuredAnimalCap", "int32(group.targetCount) > configuredAnimalCap") },
    { ...good, parkConsolidation: good.parkConsolidation.replaceAll("key := physicalShed", 'key := physicalShed + "\\x00" + strings.TrimSpace(row.RuleID)') },
    { ...good, parkConsolidation: good.parkConsolidation.replace('case "gandhi":\n\t\treturn 10', 'case "gandhi":\n\t\treturn 50') },
    { ...good, verification: good.verification.replace("SELECT min(vc.administered_at)", "SELECT min(vi.verified_at)") },
    { ...good, verification: good.verification.replace("COUNT(DISTINCT vc.goat_id)::int AS total_count", "COUNT(*)::int AS total_count") },
    { ...good, verification: good.verification.replaceAll("'approved'::text AS status", "'pending'::text AS status") },
    { ...good, verification: good.verification.replaceAll("AND status IN ('recorded', 'accepted')", "AND status = 'recorded'") },
    { ...good, calendar: good.calendar.replace("count(DISTINCT m.animal_id)", "count(*)") },
    { ...good, calendarUI: good.calendarUI.replace("summary.drive_total", "summary.total_animals") },
    { ...good, booster: good.booster.replaceAll("repeatDueAfterCompletion(*current, in.AdministeredAt)", "repeatDueAfterCompletion(*current, time.Now())") },
  ];
  for (const [index, fixture] of adversarial.entries()) {
    if (findings(fixture).length === 0) {
      console.error(`vaccination adult-drive contract guard missed adversarial fixture ${index + 1}`);
      process.exit(1);
    }
  }
  console.log("vaccination adult-drive contract guard: adversarial self-test passed");
  process.exit(0);
}

const problems = findings(loadSources());
if (problems.length) {
  console.error("vaccination adult-drive contract guard failed:");
  for (const problem of problems) console.error(`- ${problem}`);
  process.exit(1);
}
console.log("vaccination adult-drive contract guard: automatic 324-cohort, whole-shed, and administered_at contracts passed");
