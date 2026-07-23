// Public entrypoint for the Counts vertical. App pages import from "@/features/counts" (not deep submodule
// paths) per the import-boundary convention.
//
// Current visible slice = Herd Register (the goat.created entry point for the vaccination cascade)
// plus Counts Breakdown (the farm x stage x breed x gender x shed census). Tagging, identity repair,
// weights, and ADG are still NOT sidebar leaves for this slice. If a required vaccination-trigger
// path needs an identifier field/status, implement it inside /counts/herd.
export { HerdRegisterPage } from "./herd-register";
export { HerdPassportLocalDrawer, type HerdPassportDrawerItem } from "./herd-passport-local-drawer";
export { HerdPassportVaccinationBlock } from "./herd-passport-vaccination";
export { CountsBreakdownPage } from "./counts-breakdown";
