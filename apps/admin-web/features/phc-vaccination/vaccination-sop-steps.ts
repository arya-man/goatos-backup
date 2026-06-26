// Canonical vaccination drive SOP step flow (mock SOPS.vacc) — the operator-facing process a drive runs
// end to end. Authoring/versioning of the real per-tenant SOP lives in the SOP Library (/sops); this is
// the standard drive flow shown when no authored SOP overrides it, shared by the PHC Vaccination SOP
// quick-view and the Action Center obligation drawer so both render the SAME checklist anatomy.
export interface SopStep {
  title: string;
  detail: string;
  videoProof?: boolean;
}

export const VACCINATION_DRIVE_SOP_STEPS: SopStep[] = [
  { title: "Drive scheduled", detail: "Cohort + vaccine; FEFO stock reserved." },
  { title: "Per-shed administration", detail: "Dose per animal; video proof per shed event.", videoProof: true },
  { title: "Consume posted (ledger)", detail: "Verified completion posts a consume movement for doses (FEFO)." },
  { title: "Coverage + booster", detail: "Coverage % computed; next booster scheduled." },
];
