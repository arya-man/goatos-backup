import type {
  HealthAnalyticsDeath,
  HealthAnalyticsDisease,
  HealthAnalyticsEngineRule,
  HealthAnalyticsMedicine,
} from "@/lib/api/server";
import type { DeathRow, DiseaseRow, EngineRuleRow, MedicineRow } from "./health-analytics-tables";

/**
 * Wire rows -> table rows for Health Analytics.
 *
 * Pure and React-free ON PURPOSE. The one rule on this page that must never be got wrong is
 * that an UNATTRIBUTED death carries no disease — no cause of death is recorded anywhere, so
 * rendering one for an animal that had no case open would be the page inventing a clinical
 * fact. Keeping the mapping here makes that rule directly testable instead of only reachable
 * through a rendered component.
 *
 * Every piece of copy is passed IN, already resolved from the page contract: this file names
 * no visible string of its own.
 */

export type DeathLabels = {
  never: string;
  unattributed: string;
  noTag: string;
};

export type AgeBandLabels = {
  adult: string;
  kid: string;
  both: string;
  unknown: string;
};

export function ageBandLabel(labels: AgeBandLabels, raw: string): string {
  switch (raw) {
    case "adult":
      return labels.adult;
    case "kid":
      return labels.kid;
    case "both":
      return labels.both;
    default:
      return labels.unknown;
  }
}

export function toDiseaseRows(
  rows: readonly HealthAnalyticsDisease[],
  ageBands: AgeBandLabels,
): DiseaseRow[] {
  return rows.map((row) => ({
    key: row.key,
    label: row.label,
    ageBandLabel: ageBandLabel(ageBands, row.age_bands),
    newCases: row.new_cases,
    openCases: row.open_cases,
    recovered: row.recovered,
    died: row.died,
    caseFatalityPct: row.case_fatality_pct,
  }));
}

export function toDeathRows(
  rows: readonly HealthAnalyticsDeath[],
  labels: DeathLabels,
  ageBands: AgeBandLabels,
  /**
   * The shared `lib/format.fmtDate`, passed IN rather than imported so this module stays a
   * pure mapper with no app-alias dependency — which is what keeps it directly testable.
   */
  formatDate: (iso: string) => string,
): DeathRow[] {
  return rows.map((row) => {
    const attributed = row.attribution === "attributed";
    return {
      goatId: row.goat_id,
      // The farm reads an animal by its RFID. An animal with none is a data gap and says so
      // rather than rendering an empty cell that reads as a loading failure.
      animalLabel: row.tag || labels.noTag,
      displayId: row.display_id,
      pen: row.operational_location_display,
      // DD-MM-YYYY through the shared helper. The wire value stays ISO; shipping it
      // straight into the cell would put the API's own format in front of the farm.
      date: formatDate(row.business_date),
      sortDate: row.business_date,
      ageBandLabel: ageBandLabel(ageBands, row.age_band),
      // "No case on record" and "the case had closed" are different facts about the animal,
      // and both are unattributed. Collapsing them would hide the detection gap.
      attributionLabel: row.never_diagnosed ? labels.never : labels.unattributed,
      attributed,
      // THE RULE. A disease name survives only on an ATTRIBUTED row. Even if the backend
      // were to send one on an unattributed row, this drops it: the page must not be the
      // place a cause of death is invented.
      diseaseLabel: attributed ? row.disease_label : "",
      daysUnderTreatment: attributed ? (row.days_under_treatment ?? null) : null,
    };
  });
}

export function toMedicineRows(rows: readonly HealthAnalyticsMedicine[]): MedicineRow[] {
  return rows.map((row) => ({
    key: `${row.name}|${row.route}`,
    name: row.name,
    route: row.route,
    doses: row.doses,
    animals: row.animals,
  }));
}

export function toEngineRuleRows(rows: readonly HealthAnalyticsEngineRule[]): EngineRuleRow[] {
  return rows.map((row) => ({
    key: row.key,
    proposed: row.proposed,
    opened: row.opened,
    notTakenUpPct: row.not_taken_up_pct,
  }));
}
