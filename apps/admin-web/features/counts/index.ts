// Public entrypoint for the Counts vertical. App pages import from "@/features/counts" (not deep submodule
// paths) per the import-boundary convention.
//
// Current visible slice = Herd Register only (the goat.created entry point for the vaccination cascade).
// Tagging & identity / Weights & ADG are mock-map placeholders shown as disabled sidebar leaves, not built
// surfaces.
export { HerdRegisterPage } from "./herd-register";
