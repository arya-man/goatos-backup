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
import { useEffect, useRef, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

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
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const navKey = `${pathname}?${searchParams.toString()}`;
  // Optimistic selection. Cleared implicitly on the next render with a new `current`, so a
  // navigation that fails or is superseded falls back to the server's answer rather than leaving
  // a segment highlighted for something that never happened.
  const [optimistic, setOptimistic] = useState<{ value: string; navKey: string } | null>(null);
  const isPending = optimistic !== null && optimistic.navKey === navKey;
  const selected = isPending ? optimistic.value : current;
  // The scroll position at the moment of the click, restored once the navigation settles.
  //
  // Next router scroll suppression did not hold on these long pages: the navigation is genuinely
  // client-side (a marker set on `window` survives it) and the position still resets to 0 from
  // ~2,600px. The position is restored here, which is what keeps the reader beside the chart they
  // just toggled.
  //
  // Restored only when a pending transition ENDS, so it never fights an ordinary scroll: `pending`
  // is the trigger, and the ref is cleared as soon as it is used.
  const restoreTo = useRef<number | null>(null);
  const wasPending = useRef(false);
  const fallbackTimer = useRef<number | null>(null);
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

  useEffect(() => {
    if (!isPending) return undefined;
    const timer = window.setTimeout(() => {
      setOptimistic(null);
    }, 8000);
    return () => window.clearTimeout(timer);
  }, [isPending]);

  return (
    <span
      className={isPending ? "metricseg metricseg-pending" : "metricseg"}
      role="group"
      aria-label={ariaLabel}
      aria-busy={isPending}
    >
      {options.map((option) => (
        <a
          key={option.value}
          className={option.value === selected ? "on" : ""}
          href={option.href}
          aria-current={option.value === selected ? "true" : undefined}
          onClick={(event) => {
            // Modified clicks (new tab, new window, download) are left to the browser — these are
            // real links with real hrefs, and hijacking them would break open-in-new-tab.
            if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) {
              return;
            }
            event.preventDefault();
            setOptimistic({ value: option.value, navKey });
            restoreTo.current = window.scrollY;
            if (fallbackTimer.current !== null) {
              window.clearTimeout(fallbackTimer.current);
              fallbackTimer.current = null;
            }
            window.dispatchEvent(
              new CustomEvent("metricseg:navigate", {
                detail: { value: option.value, href: option.href },
              }),
            );
            router.push(option.href, { scroll: false });
          }}
        >
          {option.label}
        </a>
      ))}
    </span>
  );
}
