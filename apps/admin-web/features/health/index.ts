// Public entrypoint for the Health vertical. App routes import from "@/features/health" (not deep
// submodule paths) per the import-boundary convention.
//
// One module surface today:
//   Health Config — the authored treatment rulebook: per disease, per age band, the day-by-day
//                   course of medicines, actions and critical handoffs.
//
// Health treatment EXECUTION is app-only (the operator works the day's sessions on the phone), so
// there is no web surface for it. This screen is the INPUT that execution reads.
//
// Before changing anything on this screen, read the header of health-config.tsx: a published
// version is immutable because goats are being treated from it, and every edit goes through a
// draft. Collapsing that into an in-place edit would change the dosage an animal mid-course
// receives.
export { HealthConfigPage } from "./health-config";
