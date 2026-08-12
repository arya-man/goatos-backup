import type { AdminWebPageContract } from "@/lib/api/server";

export type AdminUiPageContract = AdminWebPageContract;
export type AdminUiTableContract = AdminWebPageContract["tables"][number];
export type AdminUiOption = AdminWebPageContract["option_groups"][number]["options"][number];
export type AdminUiControl = AdminWebPageContract["controls"][number];

const COPY_FALLBACKS: Record<string, Record<string, string>> = {
  "weighing-weights": {
    "growth_director.section.title": "Growth Director",
    "growth_director.section.aria": "Growth Director widgets",
    "growth_director.error.title": "Growth Director could not be loaded",
    "growth_director.error.body": "The rest of the page still works. Try again in a moment.",
    "growth_director.period.note": "Weighing weeks that overlap the period are counted in full; feed uses the exact days.",
    "growth_director.road.title": "Road to sale weight",
    "growth_director.road.caption": "Where every kid sits on the way to 30 kg, counted from each kid's latest weigh.",
    "growth_director.road.identities.sub": "tag identities weighed in this period",
    "growth_director.road.matched.sub": "matched to the herd register",
    "growth_director.road.moved_up": "moved up a band since their last weigh",
    "growth_director.road.held": "held their band",
    "growth_director.road.moved_down": "slipped back",
    "growth_director.road.sale_marker": "sale",
    "growth_director.road.note.pairs": "A kid has to be weighed twice before it can move a band.",
    "growth_director.road.note.unmatched": "Tags that match nothing in the herd register still count — a scale reading is a scale reading — and are shown as unmatched.",
    "growth_director.fair_fight.title": "Fair fight — same breed, same sex",
    "growth_director.fair_fight.caption": "Same breed, same sex, different sheds — a fairer comparison that points at shed-level causes. Feed, age, starting weight and health can still differ; it narrows the question, it does not close it.",
    "growth_director.fair_fight.note": "A cohort shows once the same kind of kid, weighed twice, lives in two sheds. Sex comes from the herd register, never from the shed name.",
    "growth_director.fair_fight.empty": "No cohort yet — it takes two sheds each holding three kids of the same breed and sex with a second weigh.",
    "growth_director.fair_fight.pair_noun": "kids",
    "growth_director.slow.title": "Slow-growth watchlist",
    "growth_director.slow.caption": "Groups of kids that are not gaining — worth a walk to the shed. Same breed and sex grouped together, so it points at a shed problem, not one sick kid.",
    "growth_director.slow.note": "Target ~200 g/day is the ops rule of thumb, not a contract. Changes within 3% of body weight count as gut fill; losses over 0.30 kg/day are treated as bad scans, not slow growth. Small groups stay hidden until 3 kids have a second weigh.",
    "growth_director.slow.col.shed": "Shed",
    "growth_director.slow.col.breed": "Breed",
    "growth_director.slow.col.sex": "Sex",
    "growth_director.slow.col.pairs": "Kids weighed twice",
    "growth_director.slow.col.median": "Median daily gain",
    "growth_director.slow.col.wow": "vs last week",
    "growth_director.slow.col.status": "Status",
    "growth_director.slow.status.on_track": "On track",
    "growth_director.slow.status.below_target": "Below target",
    "growth_director.slow.status.losing": "Losing",
    "growth_director.slow.wow.none": "needs two weeks of weighing",
    "growth_director.slow.empty": "No group has three kids with a second weigh yet.",
    "growth_director.feed_growth.title": "Feed given vs growth",
    "growth_director.feed_growth.caption": "Feed as directed on the sheet, not as eaten — leftovers are not measured yet. Read this as an estimate.",
    "growth_director.feed_growth.estimate": "estimate",
    "growth_director.feed_growth.col.shed": "Shed",
    "growth_director.feed_growth.col.feed": "Feed directed",
    "growth_director.feed_growth.col.gain": "Daily gain",
    "growth_director.feed_growth.col.ratio": "Feed kg / kg gained",
    "growth_director.feed_growth.feed_unit": "g per head per day",
    "growth_director.feed_growth.basis.per_animal": "per kid, own weighs",
    "growth_director.feed_growth.basis.whole_shed": "shed average movement",
    "growth_director.feed_growth.experiment": "trial",
    "growth_director.feed_growth.experiment.note": "Runs the experiment sheet (absolute kg, no per-head rate), so its ratio is not comparable.",
    "growth_director.feed_growth.no_gain": "needs a second weigh",
    "growth_director.feed_growth.note": "High feed with low gain is a ration, waste, or health question for that shed this week. Growth is counted by each kid's herd-register shed — the shed the feed sheet was written for — so kids whose tag matches nothing are left out here and counted in the trust panel.",
    "growth_director.feed_growth.empty": "No feed sheet covered these sheds in this period.",
    "growth_director.feed_problems.title": "Feed sheet problems",
    "growth_director.feed_problems.caption": "Lines the feed sheet could not fill in — the shed may have been fed by guesswork.",
    "growth_director.feed_problems.col.shed": "Shed",
    "growth_director.feed_problems.col.item": "Feed item",
    "growth_director.feed_problems.col.days": "Days blocked",
    "growth_director.feed_problems.col.reason": "Reason",
    "growth_director.feed_problems.latest_day": "on the latest feed day",
    "growth_director.feed_problems.history": "in this period",
    "growth_director.feed_problems.note.zero": "Blocked is not zero — blocked means nobody authored a ration. Zero-kg lines exist on purpose (milk-fed kids) and are not shown here.",
    "growth_director.feed_problems.empty": "Every line on the feed sheet was filled in for this period.",
    "growth_director.trust.title": "Can we trust these numbers?",
    "growth_director.trust.caption": "Every gain number on this page stands on these counts. Fixing them is the cheapest way to make the whole page better.",
    "growth_director.trust.scans_matched": "Scans matched to a kid",
    "growth_director.trust.scans_matched.sub": "a scan that matches nothing needs a re-scan",
    "growth_director.trust.pairs": "Tags weighed twice",
    "growth_director.trust.pairs.sub": "growth can only be worked out for these — a tag is only a named kid once it matches the herd register",
    "growth_director.trust.once_only": "Tags weighed once only",
    "growth_director.trust.once_only.sub": "a second weigh unlocks their gain",
    "growth_director.trust.whole_shed": "Whole-shed weighings",
    "growth_director.trust.whole_shed.sub": "no per-kid, breed or sex view",
    "growth_director.trust.pending": "Awaiting verification",
    "growth_director.trust.pending.sub": "still counted — a weigh is a weigh until a verifier bounces it",
    "growth_director.trust.rework": "Bounced by the verifier",
    "growth_director.trust.rework.sub": "left out of every gain number on this page",
  },
  "control-tower": {
    "label.scope": "Scope",
    "label.detail": "Detail",
    "label.evidence": "Evidence",
    "drawer.alert.aria": "Control Tower alert",
    "drawer.alert.close_label": "Close Control Tower alert",
    "drawer.alert.guidance": "Use the linked work surfaces to resolve the underlying process gap.",
  },
  "action-center": {
    "label.drive_over_cap_required": "capacity shortfall",
    "label.drive_medical_defer": "medical defer",
    "label.drive_terminal_closed": "terminal closed",
    "label.evidence": "Evidence",
    "label.video_proof": "video proof",
    "label.image_proof": "image proof",
    "label.open_proof": "open proof",
    "label.proof_media_unavailable": "proof media unavailable",
    "tooltip.drive_over_cap_required": "{animals} animals assigned against {slots} planned operator slots; leadership action needed before latest-safe date",
    "tooltip.drive_medical_defer": "Hard medical defer blocks vaccination until cleared",
    "tooltip.drive_terminal_closed": "Terminal animal state closed this vaccination work",
  },
  calendar: {
    "calendar.picker.previous_month": "Previous month",
    "calendar.picker.next_month": "Next month",
    "calendar.picker.previous_year": "Previous year",
    "calendar.picker.next_year": "Next year",
    "calendar.picker.drive_hint": "drive day",
    "calendar.picker.due_hint": "due work",
    "calendar.picker.deferred_hint": "deferred work",
    "calendar.picker.other_hint": "other due work",
    "calendar.picker.history_hint": "completed history",
    "calendar.drive.display_id_header": "Display ID",
    "calendar.drive.shed_header": "Shed",
    "calendar.drive.tag_1_header": "Tag 1",
    "calendar.drive.tag_2_header": "Tag 2",
    "calendar.drive.stage_header": "Stage",
    "calendar.drive.lifecycle_header": "Lifecycle",
    "calendar.drive.health_header": "Health",
    "calendar.drive.reason_header": "Reason",
    "calendar.drive.status_header": "Status",
    "calendar.drive.search_placeholder": "Search animal, tag, shed, status...",
    "calendar.drive.search_action": "Search",
    "calendar.drive.clear_search": "Clear",
    "calendar.drive.previous_page": "Previous",
    "calendar.drive.next_page": "Next",
    "calendar.drive.page_label": "Page",
    "calendar.drive.rows_label": "rows",
  },
  vaccination: {
    "command_board.kpi.targets": "Animals",
    "command_board.kpi.targets_dl": "Distinct animals in program",
    "command_board.cohort_matrix.meta": "Per farm · per dose: pending / submitted / verified, with the actual operator vaccination date or date range · red when the operator still owes work",
    "command_board.cohort_matrix.empty": "No cohort obligations in this scope",
    "command_board.cohort_matrix.no_farm": "Farm not set",
    "command_board.cohort_matrix.pending_word": "pending",
    "command_board.cohort_matrix.submitted_word": "submitted",
    "command_board.cohort_matrix.verified_word": "verified",
    "command_board.cohort_matrix.date_unavailable": "Date unavailable",
    "command_board.filter.vaccine": "Vaccine",
    "command_board.filter.all_vaccines": "All vaccines",
    "command_board.filter.drive": "Drive",
    "command_board.filter.operator_day": "Operator day (optional)",
    "command_board.filter.all_common_drives": "All common drives",
    "command_board.filter.completed_history": "Completed history",
    "command_board.filter.no_drives": "No drives planned in this park scope yet",
    "command_board.future_drives.title": "Future vaccination drives",
    "command_board.future_drives.count_suffix": "campaigns",
    "command_board.future_drives.lines_suffix": "operator days",
    "command_board.future_drives.column.campaign": "Campaign",
    "command_board.future_drives.column.drive": "Drive",
    "command_board.future_drives.column.dates": "Dates",
    "command_board.future_drives.column.sheds": "Sheds",
    "command_board.future_drives.column.animals": "Animals",
    "command_board.future_drives.column.doses": "Doses",
    "command_board.future_drives.campaign_animals": "animals",
    "command_board.future_drives.campaign_doses": "doses",
    "command_board.filter.drives_truncated": "More drives exist than this list can show — narrow the farm scope to see the rest",
    "command_board.kpi.missed": "Missed",
    "command_board.kpi.missed_dl": "Dose window closed unvaccinated",
    "command_board.shed_vaccine.title": "Shed × Vaccine",
    "command_board.shed_vaccine.meta": "Red = goats not vaccinated yet, past their due date. All doses of that vaccine counted together. Click a red box to see which goats.",
    "command_board.shed_vaccine.column.shed": "Shed",
    "command_board.shed_vaccine.state.behind": "Goats not done",
    "command_board.shed_vaccine.cell.behind_unit": "goats",
    "command_board.shed_vaccine.state.ok": "All done",
    "command_board.shed_vaccine.state.not_planned": "Not given in this shed",
    "command_board.shed_vaccine.summary_behind": "sheds have goats pending",
    "command_board.shed_vaccine.summary_clean": "Every shed is up to date on every vaccine",
    "command_board.shed_vaccine.drawer.behind_of": "behind, of",
    "command_board.shed_vaccine.drawer.column.due": "Was due",
    "command_board.shed_vaccine.cell.verifying_unit": "pending",
    "command_board.shed_vaccine.state.verifying": "Video check pending",
    "command_board.shed_vaccine.drawer.verifying_of": "given and waiting for video check, of",
    "command_board.shed_vaccine.drawer.no_video": "No video uploaded",
    "command_board.shed_vaccine.drawer.shed_videos": "Shed video",
    "command_board.shed_vaccine.drawer.clip": "Clip",
    "command_board.shed_vaccine.drawer.truncated": "Showing the longest-waiting animals only — the count above is the full figure.",
    "command_board.kpi.closed_without_dose": "Closed, no dose",
    "command_board.kpi.closed_without_dose_dl": "In the roster, but every obligation closed with no dose given",
    "command_board.shed_matrix.waiting_suffix": "d waiting",
    "label.done": "done",
    "label.overdue": "overdue",
    "status.scheduled_drive": "Drive scheduled",
    "status.no_work_due": "No work due",
    "section.full_schedule.title": "Full vaccine schedule",
    "section.full_schedule.note": "Planned vaccination drives for the selected month.",
    "section.full_schedule.operator_title": "Operator drive schedule",
    "section.full_schedule.operator_note": "Planned vaccination drives split by operator capacity and grouped by physical shed totals.",
    "section.full_schedule.loading_operator_note": "Loading planned operator assignments.",
    "section.full_schedule.assignment_unavailable_title": "Drive schedule unavailable",
    "section.full_schedule.no_assignments_title": "No operator drive rows",
    "section.full_schedule.no_assignments_body": "No persisted operator assignments exist for",
    "section.full_schedule.empty_title": "No schedule rows for this month",
    "section.full_schedule.empty_body": "Rows appear once due work is clubbed into vaccination drives for the selected month.",
    "section.full_schedule.unavailable_title": "Full vaccine schedule is unavailable",
    "section.full_schedule.unavailable_body": "The vaccination operations service did not return data. Resolve the error above, then reload.",
    "section.full_schedule.stale_title": "Schedule rebuild in progress",
    "section.full_schedule.stale_body": "Showing the last good materialized month while new vaccination changes are being rebuilt.",
    "section.full_schedule.next_rows": "Next rows",
    "schedule.legend.aria": "Full vaccine schedule status legend",
    "schedule.kpi.parks": "Parks covered",
    "schedule.kpi.sheds": "Sheds in drives",
    "schedule.kpi.animals": "Animals in drives",
    "schedule.kpi.animals_assigned": "Animals assigned",
    "schedule.kpi.overdue_drives": "Overdue drives",
    "schedule.kpi.drive_rows": "Operator days",
    "schedule.column.date": "Date",
    "schedule.column.operator": "Operator",
    "schedule.column.park": "Park",
    "schedule.column.sheds": "Sheds",
    "schedule.column.partition": "Partition",
    "schedule.column.animals": "Animals",
    "schedule.column.workload": "Workload",
    "schedule.column.vaccines": "Vaccines",
    "schedule.column.total_doses": "Total doses",
    "schedule.column.capacity": "Capacity",
    "schedule.column.postpone": "Move date",
    "schedule.partition.prefix": "Part",
    "schedule.partition.whole_shed": "Whole shed",
    "schedule.postpone.vaccine": "Vaccine",
    "schedule.postpone.new_date": "New date",
    "schedule.postpone.reason": "Reason",
    "schedule.postpone.action": "Move",
    "schedule.postpone.reason_default": "Admin drive-date override",
    "schedule.column.next_due": "Next due",
    "schedule.column.status": "Status",
    "schedule.unit.shed": "shed",
    "schedule.unit.sheds": "sheds",
    "schedule.unit.animal": "animal",
    "schedule.unit.animals": "animals",
    "schedule.unit.dose": "dose",
    "schedule.unit.doses": "doses",
    "schedule.unit.drives": "drive batches",
    "schedule.unit.deferred": "deferred",
    "schedule.unit.scheduled": "scheduled",
    "schedule.load.single_drive": "single drive",
    "schedule.load.scheduled_label": "scheduled",
    "schedule.load.todo_label": "to do",
    "schedule.load.doses_label": "doses",
    "schedule.load.deferred_label": "deferred",
    "schedule.load.overdue_label": "overdue",
    "schedule.load.done_label": "done",
    "schedule.load.tasks_label": "vaccine tasks",
    "schedule.load.goats_label": "goats",
    "schedule.load.scheduled_short": "sched.",
    "schedule.load.deferred_short": "def.",
    "schedule.drawer.title": "Drive sheds",
    "schedule.drawer.open_sheds": "Open shed list",
    "schedule.drawer.more": "more",
    "schedule.drawer.less": "Show less",
    "schedule.drawer.close": "Close shed list",
    "schedule.drawer.search": "Search sheds...",
    "schedule.drawer.search_action": "Search",
    "schedule.drawer.previous_page": "Previous",
    "schedule.drawer.next_page": "Next",
    "schedule.drawer.page_label": "Page",
    "schedule.drawer.rows_label": "rows",
    "schedule.drawer.empty": "No sheds match this search.",
    "schedule.move.open": "Move",
    "schedule.move.title": "Move vaccine date",
    "schedule.move.close": "Close move date",
    "schedule.move.recorded_title": "Move recorded",
    "schedule.move.recorded_body": "The backend accepted the vaccine date override. The planner will recalculate assignments from the new vaccine date.",
    "schedule.move.error_title": "Move failed",
    "schedule.move.error_body": "The backend rejected the date move. Check the vaccine/date and try again.",
    "schedule.move.missing_title": "Pick a date",
    "schedule.move.missing_body": "Choose the vaccine and new drive date before moving.",
    "schedule.move.date_placeholder": "Select date",
    "schedule.move.previous_month": "Previous month",
    "schedule.move.next_month": "Next month",
    "schedule.move.invalid_future_date": "Pick a date after {date}.",
    "schedule.state.deferred_title": "Drive has deferred work.",
    "schedule.state.overdue_title": "Drive has overdue work.",
    "schedule.state.due_title": "Drive has due work.",
    "schedule.state.completed_title": "Drive is fully completed.",
    "schedule.state.scheduled_title": "Drive is scheduled.",
    "schedule.row.open_title": "Open shed schedule detail",
    "schedule.row_type.adult": "Adult course",
    "schedule.row_type.kid": "Kid course",
    "schedule.row_type.fallback": "Cohort",
    "schedule.cell.no_record": "—",
    "schedule.cell.no_record_title": "No vaccine record for this shed/type in the selected month.",
    "schedule.cell.outside_year_title": "The next due or last dose date is outside the selected month.",
    "schedule.cell.overdue_title": "Overdue, missed, or rejected vaccination work.",
    "schedule.cell.due_title": "Due soon or waiting for proof / verification.",
    "schedule.cell.scheduled_title": "Drive scheduled or in progress.",
    "schedule.cell.up_to_date_title": "Accepted or completed vaccination record.",
    "label.operators_unassigned": "Operators unassigned",
    "label.no_drive": "No drive",
    "action.open_full_schedule": "Full Schedule",
    "action.open_shed_board": "Shed board",
    "action.next_year": "Next year",
  },
  "shed-execution": {
    "action.open_passport": "Open Animal Passport",
    "status.scheduled_drive": "Drive scheduled",
    "status.no_work_due": "No work due",
    "section.animals.note": "Display ID, health/lifecycle, last vaccination date, next vaccination date, and current vaccination work.",
    "animals.column.display_id": "Display ID",
    "animals.column.tag_1": "Tag 1",
    "animals.column.tag_2": "Tag 2",
    "animals.column.breed": "Breed",
    "animals.column.sex": "Sex",
    "animals.column.age": "Age",
    "animals.column.lifecycle": "Lifecycle",
    "animals.column.health": "Health",
    "animals.column.last_vax_date": "Last vaccination date",
    "animals.column.next_vax_date": "Next vaccination date",
    "animals.column.vax_work": "Vaccination work",
    "animals.status.due": "Due now",
    "animals.status.scheduled": "Scheduled",
    "animals.status.up_to_date": "Up to date",
    "animals.status.no_record": "No record",
    "animals.status.sick": "Sick",
    "animals.status.under_treatment": "Under treatment",
    "animals.status.quarantine": "Quarantine",
    "animals.status.icu": "ICU",
    "animals.status.dead": "Dead",
    "animals.status.sold": "Sold",
    "animals.status.culled": "Culled",
  },
  "protocol-adherence": {
    "gap.capacity_shortfall": "capacity shortfall",
    "vaccine.blue_tongue": "Blue Tongue",
    "vaccine.goat_pox": "Goat Pox",
    "vaccine.sheep_pox": "Sheep Pox",
    "vaccine.et_tt": "ET+TT",
    "vaccine.fmd": "FMD",
    "vaccine.ppr": "PPR",
    "vaccine.hs": "HS",
    "vaccine.generic": "Vaccination",
    "schedule.kid_course": "kid course",
    "schedule.adult_course": "adult course",
    "schedule.course": "course",
    "schedule.weeks": "weeks",
    "schedule.months": "months",
    "schedule.years": "years",
    "actual.deferred_with_reason": "deferred with reason",
    "actual.not_completed_yet": "Not completed yet",
    "adherence.help.aria": "How adherence is calculated",
    "adherence.help.completed_label": "completed",
    "adherence.help.current_prefix": "Example: if 100 vaccinations are expected and 67 are completed correctly, adherence is 67%. Deferred sick/quarantine work is tracked separately.",
    "adherence.help.expected_label": "expected",
    "adherence.help.formula": "Formula: completed expected vaccinations divided by total expected vaccinations.",
    "adherence.help.loading": "The exact percent appears after the adherence summary loads.",
    "adherence.help.title": "What adherence means",
    "adherence.help.window_joiner": "to",
    "adherence.help.window_prefix": "Adherence shows how much scheduled vaccination work was completed correctly in the selected operating window.",
    "label.due_lower": "due",
    "label.info_icon": "i",
    "label.owner_short": "OWNER",
    "label.video_proof": "video proof",
    "label.image_proof": "image proof",
    "label.open_proof": "open proof",
    "label.proof_media_unavailable": "proof media unavailable",
  },
  "goat-passport": {
    "drawer.passport.aria": "Animal Passport",
    "drawer.passport.close_label": "Close Animal Passport drawer",
    "action.full_change_history": "Full change history",
    "action.close": "Close",
    "vaccination.clinical_due": "clinical due",
  },
};

const PROCESS_WORK_STATE_OPTIONS: AdminUiOption[] = [
  { key: "due", label: "Due", title: "", tone: "warn", enabled: true, disabled_reason: "" },
  { key: "overdue", label: "Overdue", title: "", tone: "dng", enabled: true, disabled_reason: "" },
  { key: "missed", label: "Missed", title: "", tone: "warn", enabled: true, disabled_reason: "" },
  { key: "blocked", label: "Blocked", title: "", tone: "dng", enabled: true, disabled_reason: "" },
  { key: "proof_pending", label: "Proof pending", title: "", tone: "warn", enabled: true, disabled_reason: "" },
  { key: "verification_pending", label: "Verification pending", title: "", tone: "pur", enabled: true, disabled_reason: "" },
  { key: "rejected", label: "Rejected", title: "", tone: "dng", enabled: true, disabled_reason: "" },
  { key: "deferred", label: "Deferred", title: "", tone: "mut", enabled: true, disabled_reason: "" },
  { key: "scheduled", label: "Scheduled", title: "", tone: "info", enabled: true, disabled_reason: "" },
  { key: "in_progress", label: "In progress", title: "", tone: "info", enabled: true, disabled_reason: "" },
  { key: "completed", label: "Completed", title: "", tone: "ok", enabled: true, disabled_reason: "" },
];

const PROCESS_SEVERITY_OPTIONS: AdminUiOption[] = [
  { key: "ok", label: "OK", title: "", tone: "ok", enabled: true, disabled_reason: "" },
  { key: "watch", label: "Watch", title: "", tone: "info", enabled: true, disabled_reason: "" },
  { key: "at_risk", label: "At risk", title: "", tone: "warn", enabled: true, disabled_reason: "" },
  { key: "broken", label: "Broken", title: "", tone: "dng", enabled: true, disabled_reason: "" },
];

const PROCESS_SOP_STATE_OPTIONS: AdminUiOption[] = [
  { key: "not_started", label: "SOP: not started", title: "", tone: "mut", enabled: true, disabled_reason: "" },
  { key: "in_progress", label: "SOP: in progress", title: "", tone: "info", enabled: true, disabled_reason: "" },
  { key: "submitted", label: "SOP: submitted", title: "", tone: "warn", enabled: true, disabled_reason: "" },
  { key: "accepted", label: "SOP: accepted", title: "", tone: "ok", enabled: true, disabled_reason: "" },
  { key: "rework", label: "SOP: rework", title: "", tone: "dng", enabled: true, disabled_reason: "" },
];

const PROCESS_PROOF_STATE_OPTIONS: AdminUiOption[] = [
  { key: "not_required", label: "Proof: n/a", title: "", tone: "mut", enabled: true, disabled_reason: "" },
  { key: "missing", label: "Proof: missing", title: "", tone: "warn", enabled: true, disabled_reason: "" },
  { key: "uploaded", label: "Proof: uploaded", title: "", tone: "info", enabled: true, disabled_reason: "" },
  { key: "accepted", label: "Proof: accepted", title: "", tone: "ok", enabled: true, disabled_reason: "" },
  { key: "rejected", label: "Proof: rejected", title: "", tone: "dng", enabled: true, disabled_reason: "" },
];

const PROCESS_VERIFICATION_STATE_OPTIONS: AdminUiOption[] = [
  { key: "not_ready", label: "Verify: not ready", title: "", tone: "mut", enabled: true, disabled_reason: "" },
  { key: "pending", label: "Verify: pending", title: "", tone: "pur", enabled: true, disabled_reason: "" },
  { key: "accepted", label: "Verify: accepted", title: "", tone: "ok", enabled: true, disabled_reason: "" },
  { key: "rejected", label: "Verify: rejected", title: "", tone: "dng", enabled: true, disabled_reason: "" },
];

const PROCESS_OPTION_GROUP_FALLBACKS: Record<string, AdminUiOption[]> = {
  work_state_filter_chips: PROCESS_WORK_STATE_OPTIONS,
  severity_chips: PROCESS_SEVERITY_OPTIONS,
  sop_state_chips: PROCESS_SOP_STATE_OPTIONS,
  proof_state_chips: PROCESS_PROOF_STATE_OPTIONS,
  verification_state_chips: PROCESS_VERIFICATION_STATE_OPTIONS,
};

const OPTION_GROUP_FALLBACKS: Record<string, Record<string, AdminUiOption[]>> = {
  "control-tower": PROCESS_OPTION_GROUP_FALLBACKS,
  "protocol-adherence": PROCESS_OPTION_GROUP_FALLBACKS,
  workflows: PROCESS_OPTION_GROUP_FALLBACKS,
  "workflow-drilldown": PROCESS_OPTION_GROUP_FALLBACKS,
  vaccination: {
    command_board_cohort_ladder: [
      { key: "K0", label: "K0", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "K1", label: "K1", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "K2", label: "K2", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "K3", label: "K3", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "Kid", label: "Kid", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "Fattening", label: "Fattening", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "F2", label: "F2", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "Adults", label: "Adults", title: "", tone: "", enabled: true, disabled_reason: "" },
    ],
    // Mirrors the backend membership map: Adults is a declared rung, never a catch-all, and an
    // unmapped stage renders as its own row rather than joining Adults.
    command_board_cohort_stage_map: [
      { key: "NON-PREGNANT", label: "Adults", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "BUCK", label: "Adults", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "MOTHER", label: "Adults", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "ADULT", label: "Adults", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "ICU-NON-PREGNANT", label: "Adults", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "K0", label: "K0", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "K1", label: "K1", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "K2", label: "K2", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "K3", label: "K3", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "ICU-KID", label: "Kid", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "ICU-KIDS", label: "Kid", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "QUARANTINE KIDS", label: "Kid", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "F2-FEMALE", label: "F2", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "F2-MALE", label: "F2", title: "", tone: "", enabled: true, disabled_reason: "" },
      { key: "FATTENING", label: "Fattening", title: "", tone: "", enabled: true, disabled_reason: "" },
    ],
    schedule_status_legend: [
      { key: "overdue", label: "Overdue", title: "missed, overdue, or rejected", tone: "dng", enabled: true, disabled_reason: "" },
      { key: "due_soon", label: "Due soon", title: "due, proof, or verification pending", tone: "warn", enabled: true, disabled_reason: "" },
      { key: "scheduled", label: "Scheduled", title: "drive scheduled or in progress", tone: "info", enabled: true, disabled_reason: "" },
      { key: "up_to_date", label: "Up to date", title: "accepted or completed", tone: "ok", enabled: true, disabled_reason: "" },
      { key: "no_record", label: "No record", title: "no selected-year date", tone: "mut", enabled: true, disabled_reason: "" },
    ],
  },
};

const TABLE_FALLBACKS: Record<string, Record<string, AdminUiTableContract>> = {
  vaccination: {
    "full-vaccine-schedule": {
      id: "full-vaccine-schedule",
      title: "Full vaccine schedule",
      data_source: "/vaccination/schedule",
      columns: [
        { key: "date", label: "Date", sortable: false, visible: true },
        { key: "park", label: "Park", sortable: false, visible: true },
        { key: "sheds", label: "Sheds", sortable: false, visible: true },
        { key: "animals", label: "Animals", sortable: false, visible: true },
        { key: "vaccines", label: "Vaccines", sortable: false, visible: true },
        { key: "status", label: "Status", sortable: false, visible: true },
      ],
      filters: [],
      sort_keys: [],
      page_size_options: [],
      row_click: {
        enabled: false,
        param: "schedule_row",
        target_drawer: "",
        summary_fields: [],
        detail_fields: [],
      },
      summary_fields: [],
      detail_fields: [],
    },
  },
};

export function copy(page: AdminUiPageContract, key: string, whenAbsent?: string): string {
  const value = page.copy[key];
  if (typeof value !== "string") {
    const fallback = COPY_FALLBACKS[page.route_id]?.[key];
    if (fallback) return fallback;
    // `whenAbsent` is for OPEN key spaces only -- keys derived at runtime from data, such as
    // `feedback.<server error code>`, where the set is defined by the backend's error vocabulary
    // and not every member is guaranteed to have copy. Passing it is an explicit statement that a
    // miss is expected and renders nothing. Fixed keys still throw, which is what keeps a missing
    // contract key a loud build/runtime failure instead of a blank label.
    if (whenAbsent !== undefined) return whenAbsent;
    throw new Error(`Admin-web page contract ${page.route_id} missing copy key ${key}`);
  }
  return value;
}

export function optionalCopy(page: AdminUiPageContract | undefined, key: string): string | undefined {
  return page?.copy[key];
}

export function actionFeedbackCopy(page: AdminUiPageContract, status: string | undefined, key: string | undefined): string {
  if (key) return copy(page, key);
  return copy(page, status === "success" ? "action.success_message" : "action.failed_message");
}

export function table(page: AdminUiPageContract, tableId: string): AdminUiTableContract {
  const value = page.tables.find((item) => item.id === tableId);
  if (!value) {
    const fallback = TABLE_FALLBACKS[page.route_id]?.[tableId];
    if (fallback) return fallback;
    throw new Error(`Admin-web page contract ${page.route_id} missing table ${tableId}`);
  }
  return value;
}

export function tableLabels(page: AdminUiPageContract, tableId: string): string[] {
  return table(page, tableId).columns.filter((column) => column.visible).map((column) => column.label);
}

export function tablePageSizes(page: AdminUiPageContract, tableId: string): number[] {
  return table(page, tableId).page_size_options;
}

/**
 * Whether a backend-declared control is enabled for this principal. Shared because the verifier
 * surfaces gate on it in more than one place (the drawer's verdict form and the queue's telemetry
 * emission), and a second hand-rolled copy is how those two drift apart.
 */
export function controlEnabled(page: AdminUiPageContract, controlId: string, fallback: boolean): boolean {
  return page.controls.find((item) => item.id === controlId)?.enabled ?? fallback;
}

export function control(page: AdminUiPageContract, controlId: string): AdminUiControl {
  const value = page.controls.find((item) => item.id === controlId);
  if (!value) {
    throw new Error(`Admin-web page contract ${page.route_id} missing control ${controlId}`);
  }
  return value;
}

export function optionGroup(page: AdminUiPageContract, groupId: string): AdminUiOption[] {
  const value = page.option_groups.find((item) => item.id === groupId);
  if (!value) {
    const fallback = OPTION_GROUP_FALLBACKS[page.route_id]?.[groupId];
    if (fallback) return fallback;
    throw new Error(`Admin-web page contract ${page.route_id} missing option group ${groupId}`);
  }
  return value.options;
}

export function optionalOptionGroup(page: AdminUiPageContract, groupId: string): AdminUiOption[] {
  return page.option_groups.find((item) => item.id === groupId)?.options ?? OPTION_GROUP_FALLBACKS[page.route_id]?.[groupId] ?? [];
}

export function optionalOption(page: AdminUiPageContract, groupId: string, key: string): AdminUiOption | undefined {
  return optionalOptionGroup(page, groupId).find((item) => item.key === key);
}

export function optionLabel(page: AdminUiPageContract, groupId: string, key: string): string {
  const value = optionGroup(page, groupId).find((item) => item.key === key);
  if (!value) {
    throw new Error(`Admin-web page contract ${page.route_id} missing option ${groupId}.${key}`);
  }
  return value.label;
}

export function optionTitle(page: AdminUiPageContract, groupId: string, key: string): string {
  const value = optionGroup(page, groupId).find((item) => item.key === key);
  if (!value) {
    throw new Error(`Admin-web page contract ${page.route_id} missing option ${groupId}.${key}`);
  }
  return value.title || "";
}

export function optionTone(page: AdminUiPageContract, groupId: string, key: string): string {
  const value = optionGroup(page, groupId).find((item) => item.key === key);
  if (!value) {
    throw new Error(`Admin-web page contract ${page.route_id} missing option ${groupId}.${key}`);
  }
  return value.tone || "mut";
}
