/**
 * The vaccination plan feature's public entrypoint.
 *
 * Everything the app routes need comes through here. Deep imports into the feature's
 * internals are refused by the boundary guard, and rightly: a page reaching past this file
 * pins the feature's internal file layout in place from outside it.
 */
export { VaccinationPlanConsole } from "./plan-console";
export { VaccinationPlanEditor } from "./plan-editor";
export { describeChange, readVaccines } from "./plan-model";
export type { VaccineGroup } from "./plan-model";
export { fromRuleDsl } from "./editor-model";
export type { EditorPlan, EditorVaccine } from "./editor-model";
