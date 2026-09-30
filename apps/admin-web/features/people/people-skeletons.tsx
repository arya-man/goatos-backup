import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { FormCardSkeleton } from "@/components/app/skeletons";

/**
 * The /people Vaccination desk's loading shape, ONE component for the tab-click fallback
 * (people-page UrlSuspense) and the screen's own client loading state, so the three phases of a
 * click (panel skeleton, screen loading, content) keep one shape (J3 P1-2 class; r2
 * `interact|*|fallback-shape`). A multi-park principal with no park chosen lands on the "Choose a
 * park" card (template Card + CardHeader + one select), everyone else on the roster.
 */
export function VaccinationDeskSkeleton({ chooser }: { chooser: boolean }) {
  return chooser ? <FormCardSkeleton subheader wrap controls={[260]} /> : <PanelSkeleton kpis={4} table={10} />;
}

/** Whether the desk opens on the park chooser: no park in the URL and more than one park in scope. */
export function vaccinationDeskOpensOnChooser(initialParkId: string | undefined, parkCount: number): boolean {
  return !initialParkId && parkCount > 1;
}
