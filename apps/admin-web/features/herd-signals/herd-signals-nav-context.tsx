"use client";

import { createContext, useCallback, useContext, useMemo, useTransition, type ReactNode } from "react";
import { useRouter } from "next/navigation";

// One shared pending-transition flag for every filter/KPI/pagination control on the Live Monitor
// (and the other table tabs), so a click on a KPI card, a select in the filter bar, or a "Next
// page" link all drive the SAME busy affordance instead of each wiring its own useTransition and
// leaving the others looking frozen. This exists because of a real defect: filters were already
// wrapped in startTransition (so React correctly keeps the OLD table on screen instead of
// remounting to the Suspense fallback), but nothing ever READ `isPending`, so there was no visual
// difference between "request in flight" and "page reloaded" — see mesha-theme.css's own
// .wfbusy/.wfspin comment, which describes this exact defect class on a sibling screen.
type HerdSignalsNav = {
  isPending: boolean;
  navigate: (href: string) => void;
};

const HerdSignalsNavContext = createContext<HerdSignalsNav | null>(null);

export function HerdSignalsNavProvider({ children }: { children: ReactNode }) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();

  const navigate = useCallback(
    (href: string) => {
      startTransition(() => {
        // scroll:false — a filter/KPI/page change must not jump the reader back to the top of the
        // table, and must not fight a scroll position while the drawer overlay is open.
        router.push(href, { scroll: false });
      });
    },
    [router],
  );

  const value = useMemo(() => ({ isPending, navigate }), [isPending, navigate]);
  return <HerdSignalsNavContext.Provider value={value}>{children}</HerdSignalsNavContext.Provider>;
}

export function useHerdSignalsNav(): HerdSignalsNav {
  const context = useContext(HerdSignalsNavContext);

  // The provider is rendered by herd-signals-board, which is a SERVER component, so the
  // tab content it renders is passed as `children` and lands as a SIBLING of the provider
  // in the client tree rather than a descendant. Client consumers therefore see no context
  // and previously THREW, crashing the whole table to its error boundary -- which looked
  // like "row click does nothing" and "the page is broken".
  //
  // Fall back to a self-contained transition instead. A consumer outside the provider gets
  // its own pending flag rather than the shared one -- slightly less coordinated, but it
  // navigates correctly and, above all, it cannot take the page down.
  const router = useRouter();
  const [fallbackPending, startFallback] = useTransition();
  const fallbackNavigate = useCallback(
    (href: string) => {
      startFallback(() => {
        router.push(href, { scroll: false });
      });
    },
    [router],
  );
  const fallback = useMemo(
    () => ({ isPending: fallbackPending, navigate: fallbackNavigate }),
    [fallbackPending, fallbackNavigate],
  );

  return context ?? fallback;
}
