// CEO closure view — KPIs, cohort matrix, shed dose matrix, weekly given, verification queue.
// Types and helpers for the /vaccination/command endpoint.

import type { AppApiComponents } from "@goatos/api-client";

export type VaccinationCommandBoardResponse =
  AppApiComponents["schemas"]["VaccinationCommandBoardResponse"];
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
