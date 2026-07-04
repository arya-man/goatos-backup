// Public entrypoint for the vaccination-execution read model (park/shed execution context owned by Preventive Care (PC)
// Vaccination). App pages import from "@/features/vaccination-execution" (not deep submodule paths).
//
// Parks is NOT a separate visible vaccination product. The execution surface renders INSIDE the
// Preventive Care (PC) Vaccination module via VaccinationExecutionBoard (embedded by features/preventive-care-vaccination/execution-section) at
// /vaccination#execution. Park scope comes from the shell top bar. The only sub-route is the shed
// execution detail at /vaccination/execution/sheds/{shedId}.
export { VaccinationExecutionBoard } from "./execution-board";
export { ShedExecutionDetailPage } from "./shed-drilldown";
