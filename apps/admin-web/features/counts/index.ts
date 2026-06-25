// Public entrypoint for the Counts vertical. App pages import from "@/features/counts" (not deep submodule
// paths) per the import-boundary convention.
//
// Current visible slice = Herd Register only (the goat.created entry point for the vaccination cascade).
// Tagging, identity repair, weights, and ADG are not sidebar leaves for this slice. If a required
// vaccination-trigger path needs an identifier field/status, implement it inside /counts/herd.
export { HerdRegisterPage } from "./herd-register";
