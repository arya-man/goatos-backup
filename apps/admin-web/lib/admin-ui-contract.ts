import type { AdminWebPageContract } from "@/lib/api/server";

export type AdminUiPageContract = AdminWebPageContract;
export type AdminUiTableContract = AdminWebPageContract["tables"][number];
export type AdminUiOption = AdminWebPageContract["option_groups"][number]["options"][number];
export type AdminUiControl = AdminWebPageContract["controls"][number];

const COPY_FALLBACKS: Record<string, Record<string, string>> = {
  calendar: {
    "calendar.picker.previous_month": "Previous month",
    "calendar.picker.next_month": "Next month",
    "calendar.picker.drive_hint": "drive day",
    "calendar.picker.other_hint": "other due work",
  },
  config: {
    "modal.rule_editor.matrix_grid_title": "Vaccination matrix rows",
    "modal.rule_editor.field.selected_matrix_row_details": "Selected row details",
    "modal.rule_editor.field.shared_eligibility_policy": "Shared eligibility policy",
    "modal.rule_editor.field.vaccine_pathogen_class": "Pathogen class",
    "modal.rule_editor.field.vaccine_course_type": "Source course type",
    "modal.rule_editor.field.compatibility_policy": "Cross-vaccine spacing policy",
    "modal.rule_editor.field.live_to_killed_gap": "live -> killed gap days",
    "modal.rule_editor.field.killed_to_killed_gap": "killed -> killed gap days",
    "modal.rule_editor.field.live_to_live_gap": "live -> live gap days",
    "modal.rule_editor.field.kid_booster_min_gap": "kid booster min gap days",
    "modal.rule_editor.field.bacterial_viral_same_day": "bacterial + viral same day",
    "modal.rule_editor.field.live_killed_viral_same_day": "live viral + killed viral same day",
    "modal.rule_editor.field.procurement_policy": "Procurement / source policy",
    "modal.rule_editor.field.warmup_no_vaccination_days": "warm-up hold days",
    "modal.rule_editor.field.kids_normal_schedule_until_weeks": "kids normal schedule until weeks",
    "modal.rule_editor.field.adult_source_vaccination_allowed": "adult source vaccination allowed",
    "modal.rule_editor.field.first_wave": "first wave vaccines",
    "modal.rule_editor.field.second_wave_after_days": "second wave after days",
    "modal.rule_editor.field.goat_second_wave": "goat second wave vaccines",
    "modal.rule_editor.field.sheep_second_wave": "sheep second wave vaccines",
    "modal.rule_editor.field.pregnancy_policy": "Pregnancy / delivery policy",
    "modal.rule_editor.field.allow_until_pregnancy_month": "allow until pregnancy month",
    "modal.rule_editor.field.skip_from_pregnancy_month": "skip from pregnancy month",
    "modal.rule_editor.field.skip_through_pregnancy_month": "skip through pregnancy month",
    "modal.rule_editor.field.post_delivery_catch_up_days": "post-delivery catch-up days",
    "modal.rule_editor.action.add_matrix_row": "Add matrix row",
    "modal.rule_editor.action.load_source_vaccine_matrix": "Load Source Vaccine Matrix",
    "modal.rule_editor.action.apply_source_schedule": "Apply source schedule",
    "modal.rule_editor.action.remove_matrix_row": "Remove matrix row",
    "modal.rule_editor.title.keep_one_matrix_row": "At least one matrix row is required",
    "modal.rule_editor.table.row": "Row",
    "modal.rule_editor.table.vaccine_code": "Vaccine",
    "modal.rule_editor.table.vaccine_name": "Name",
    "modal.rule_editor.table.course_type": "Course",
    "modal.rule_editor.table.source_schedule": "Source schedule",
    "modal.rule_editor.table.vial_doses": "Vial",
    "modal.rule_editor.table.revaccination": "Revaccination (d)",
    "modal.rule_editor.table.stage": "Stage",
    "modal.rule_editor.table.sex": "Sex",
    "modal.rule_editor.table.breed": "Breed",
    "modal.rule_editor.guided.selected_combo_label": "Selected combo",
    "modal.rule_editor.guided.dose_rows_count": "dose rows",
    "modal.rule_editor.guided.vaccine_lot_policy_label": "Vaccine lot policy",
    "modal.rule_editor.impact_title_vaccination": "Impact preview - selected dose row",
    "modal.rule_editor.impact_scope_label": "Preview scope",
    "modal.rule_editor.impact_stock_set": "stock item set",
    "modal.rule_editor.impact_stock_missing": "no stock item",
    "modal.rule_editor.impact_plan_title": "Planned session split",
    "modal.rule_editor.impact_plan_date": "Date",
    "modal.rule_editor.impact_plan_vaccinations": "Vaccinations",
    "modal.rule_editor.impact_plan_daily_limit": "Daily limit",
    "modal.rule_editor.impact_plan_capacity": "Capacity",
    "modal.rule_editor.impact_method_title": "How the numbers are calculated",
    "modal.rule_editor.impact_method_body": "Read-only estimate for the selected combo. Eligible goats = live in-care animals matching species, stage, sex, breed, health, and park scope. Obligations/cycle = eligible goats x dose rows. Batches = distinct eligible sheds, approximating one SOP drive task per shed. Doses required = eligible goats x dose rows. Stock appears only when the row has a vaccine inventory item.",
    "modal.rule_editor.impact_scale_note": "For 1-5M goats, this panel uses aggregate SQL counts and does not load goats into the browser. It is a quick operator estimate; full scale proof still comes from the high-scale kernel E2E/staging report.",
  },
};

const OPTION_GROUP_FALLBACKS: Record<string, Record<string, AdminUiOption[]>> = {
  config: {
    vaccine_pathogen_classes: [
      optionFallback("unknown_review_needed", "unknown - review needed", "publishable only when source lacks class; fail closed for same-day planning", "warn"),
      optionFallback("bacterial", "bacterial", "", ""),
      optionFallback("viral", "viral", "", ""),
      optionFallback("mixed", "mixed", "combination vaccine or reviewed combo row", "info"),
    ],
    vaccine_course_types: [
      optionFallback("single", "single", "source table Type = Single", ""),
      optionFallback("booster", "booster", "source table Type = Booster", ""),
    ],
  },
};

function optionFallback(key: string, label: string, title: string, tone: string): AdminUiOption {
  return { key, label, title, tone, enabled: true, disabled_reason: "" };
}

export function copy(page: AdminUiPageContract, key: string): string {
  const value = page.copy[key];
  if (typeof value !== "string") {
    const fallback = COPY_FALLBACKS[page.route_id]?.[key];
    if (fallback) return fallback;
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
  return page.option_groups.find((item) => item.id === groupId)?.options ?? [];
}

export function optionalOption(page: AdminUiPageContract, groupId: string, key: string): AdminUiOption | undefined {
  return optionGroup(page, groupId).find((item) => item.key === key);
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
