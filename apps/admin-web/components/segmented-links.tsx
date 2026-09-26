"use client";

// A URL-driven segmented control that does NOT throw the reader back to the top of the page.
//
// The Weights page is long — the gain chart sits ~2,600px down and the feed table further still —
// and every one of its toggles changes a search param, which means a real navigation and a real
// server re-render. As ordinary anchor links, each of those answered the question and then
// scrolled to the top, so the reader had to find the chart again to see what changed.
//
// Plain anchors keep the target route honest: authenticated admin tabs must not Next-prefetch,
// because prefetch can trigger expensive server/API reads before the user actually clicks.
// The explicit pending state keeps the pressed segment highlighted immediately instead of
// waiting for the server round trip.
//
// It renders NO copy of its own: labels arrive already resolved from the page contract.
import { useEffect, useRef } from "react";
import { SegmentTabs } from "@/components/minimal/list/segment-tabs";
import { shownTabValue } from "@/components/app/url-tab-nav";
import { useUrlTabNav } from "@/components/app/use-url-tab-nav";

export type SegmentedOption = {
  /** Stable identity for this option, compared against `current`. */
  value: string;
  /** Already resolved from the page contract by the caller. */
  label: string;
  href: string;
};

export function SegmentedLinks({
  options,
  current,
  ariaLabel,
}: {
  options: readonly SegmentedOption[];
  current: string;
  /** Already resolved from the page contract by the caller; omitted when the group is unlabelled. */
  ariaLabel?: string;
}) {
  // The pressed segment is drawn selected at once and the page stays on screen while the
  // navigation runs in a transition (see useUrlTabNav).
  const { pendingValue, isPending, navigate } = useUrlTabNav();
  const selected = shownTabValue(current, pendingValue);
  // The scroll position at the moment of the click, restored once the navigation settles.
  //
  // Next router scroll suppression did not hold on these long pages: the position still reset to
  // 0 from ~2,600px. The position is restored here, which is what keeps the reader beside the chart
  // they just toggled.
  //
  // Restored only when a pending transition ENDS, so it never fights an ordinary scroll: `pending`
  // is the trigger, and the ref is cleared as soon as it is used.
  const restoreTo = useRef<number | null>(null);
  const wasPending = useRef(false);
  useEffect(() => {
    if (isPending) {
      wasPending.current = true;
      return;
    }
    if (!wasPending.current) return;
    wasPending.current = false;
    const target = restoreTo.current;
    restoreTo.current = null;
    if (target === null) return;
    // The re-rendered page can be SHORTER than the one clicked on (a filter that hides rows), so
    // the browser would clamp a restore past the new bottom. Clamping here keeps the jump silent
    // instead of landing at an arbitrary point.
    const max = Math.max(0, document.documentElement.scrollHeight - window.innerHeight);
    window.scrollTo({ top: Math.min(target, max), behavior: "instant" as ScrollBehavior });
  }, [isPending]);

  return (
    <SegmentTabs
      className={isPending ? "metricseg metricseg-pending" : "metricseg"}
      ariaLabel={ariaLabel}
      busy={isPending}
      value={selected}
      tabs={options.map((option) => ({
        value: option.value,
        label: option.label,
        href: option.href,
        onClick: (event: React.MouseEvent<HTMLElement>) => {
          restoreTo.current = window.scrollY;
          // Fires metricseg:navigate (LinkNavPending dims the body) and pushes in a transition.
          navigate(event, option.value, option.href);
        },
      }))}
    />
  );
}
