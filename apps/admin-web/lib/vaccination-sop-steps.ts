// Canonical vaccination drive SOP step flow shown by both the vaccination SOP
// quick-view and Action Center. Tenant-authored/versioned SOP truth remains in
// the SOP Library; this is the shared standard-flow presentation fallback.
export interface SopStep {
  title: string;
  detail: string;
  videoProof?: boolean;
}

export const VACCINATION_DRIVE_SOP_STEPS: SopStep[] = [
  { title: "Drive scheduled", detail: "Cohort + vaccine; FEFO stock reserved." },
  { title: "Per-goat administration", detail: "Dose per animal; in-app camera proof for each goat.", videoProof: true },
  { title: "Consume posted (ledger)", detail: "Verified completion posts a consume movement for doses (FEFO)." },
  { title: "Coverage + booster", detail: "Coverage % computed; next booster scheduled." },
];
