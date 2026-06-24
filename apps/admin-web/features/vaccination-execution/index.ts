// Public entrypoint for the vaccination-execution read model (park/shed execution context owned by PHC
// Vaccination). App pages import from "@/features/vaccination-execution" (not deep submodule paths).
//
// Parks is NOT a separate visible vaccination product. The execution surface renders INSIDE PHC /
// Vaccination via VaccinationExecutionBoard (embedded by features/phc-vaccination/execution-section) at
// /vaccination#execution. Park scope comes from the shell top bar. The only sub-route is the shed
// execution detail at /vaccination/execution/sheds/{shedId}.
export { VaccinationExecutionBoard } from "./execution-board";
export { ShedExecutionDetailPage } from "./shed-drilldown";
