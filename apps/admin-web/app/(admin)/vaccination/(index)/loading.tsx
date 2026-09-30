import { VaccinationRouteSkeleton } from "@/features/preventive-care-vaccination/vaccination-route-skeleton";

/** /vaccination: the board shape, or the full-schedule shape for `?view=schedule` (the page's own predicate). */
export default function Loading() {
  return <VaccinationRouteSkeleton />;
}
