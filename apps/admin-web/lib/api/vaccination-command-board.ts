// CEO closure view — KPIs, cohort matrix, shed dose matrix, weekly given, verification queue.
// Types and helpers for the /vaccination/command endpoint.

import type { AppApiComponents } from "@goatos/api-client";

type CommandBoardKpis = AppApiComponents["schemas"]["VaccinationCommandBoardKPI"] & {
  missedNotGiven?: number;
  closedWithoutDose?: number;
};
type CommandBoardDriveOption = AppApiComponents["schemas"]["VaccinationCommandBoardDriveOption"] & {
  driveName?: string;
  parkId?: string | null;
  parkName?: string | null;
  plannedDate?: string | null;
  targetCount?: number;
  doseCount?: number;
  shedNames?: string[];
  shedIds?: string[];
  shedLocations?: Array<{
    shedId: string;
    shedName: string;
    partition_label?: string | null;
    operational_location_display?: string | null;
  }>;
  operatorDays?: Array<{ date: string; targetCount: number; doseCount: number }>;
};

export type VaccinationCommandBoardResponse =
  Omit<AppApiComponents["schemas"]["VaccinationCommandBoardResponse"], "kpis" | "driveOptions"> & {
    kpis: CommandBoardKpis;
    driveOptions: CommandBoardDriveOption[];
    shedVaccineMatrix?: AppApiComponents["schemas"]["ShedDoseMatrixCell"][];
    shedVaccineColumns?: Array<{ key: string; label: string }>;
    closedWithoutDoseAnimals?: unknown[];
    driveOptionsTruncated?: boolean;
  };
export type VaccinationCommandBoardKPI =
  AppApiComponents["schemas"]["VaccinationCommandBoardKPI"];
export type VaccinationCommandBoardCohortCell =
  AppApiComponents["schemas"]["VaccinationCommandBoardCohortCell"];
export type ShedDoseMatrixCell =
  AppApiComponents["schemas"]["ShedDoseMatrixCell"];
export type WeeklyGivenRow =
  AppApiComponents["schemas"]["WeeklyGivenRow"];
export type VerificationQueueRow =
  AppApiComponents["schemas"]["VerificationQueueRow"];
