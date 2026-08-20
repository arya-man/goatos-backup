// Public entrypoint for the Counts vertical. App pages import from "@/features/counts" (not deep submodule
// paths) per the import-boundary convention.
//
// Current visible slice = Herd Analytics (the leadership read: composition now, movement by month),
// Counts Breakdown (the farm x stage x breed x gender x shed census) and Milk Preparation
// (the current K1/K2/K3 direction). Herd Register stays exported and its route stays reachable —
// only its sidebar leaf is withheld (maintainer decision 2026-08-20) — because it remains the
// goat.created entry point for the vaccination cascade. Tagging, identity repair,
// weights, and ADG are still NOT sidebar leaves for this slice. If a required vaccination-trigger
// path needs an identifier field/status, implement it inside /counts/herd.
export { HerdRegisterPage } from "./herd-register";
export { HerdPassportLocalDrawer, type HerdPassportDrawerItem } from "./herd-passport-local-drawer";
export { HerdPassportVaccinationBlock } from "./herd-passport-vaccination";
export { CountsBreakdownPage } from "./counts-breakdown";
export { HerdAnalyticsPage } from "./herd-analytics";
export { MilkPreparationPage } from "./milk-preparation";
