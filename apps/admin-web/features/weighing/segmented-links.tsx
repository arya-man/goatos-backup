"use client";

// A URL-driven segmented control that does NOT throw the reader back to the top of the page.
//
// The Weights page is long — the gain chart sits ~2,600px down and the feed table further still —
// and every one of its toggles changes a search param, which means a real navigation and a real
// server re-render. Rendered as plain <a> links, each of those answered the question and then
// scrolled to the top, so the reader had to find the chart again to see what changed.
//
// `<Link scroll={false}>` did NOT fix it here: the navigation is genuinely client-side (a marker
// set on `window` survives it), and the scroll still reset. The repo's own working pattern for
// this is `router.push(href, { scroll: false })` from a client component — six other filters use
// it — so this uses that.
//
// `useTransition` is not decoration either: it is the URL-Driven Filter Responsiveness rule. The
// active option is rendered OPTIMISTICALLY from local state so the pressed segment highlights on
// the click rather than after the server round trip, which is what stopped the old select controls
// from visibly bouncing back to their previous value while a refresh was pending.
//
// It renders NO copy of its own: labels arrive already resolved from the page contract.
import { useEffect, useRef, useState, useTransition } from "react";
import { useRouter } from "next/navigation";

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
  const [isPending, startTransition] = useTransition();
  // Optimistic selection. Cleared implicitly on the next render with a new `current`, so a
  // navigation that fails or is superseded falls back to the server's answer rather than leaving
  // a segment highlighted for something that never happened.
  const [optimistic, setOptimistic] = useState<string | null>(null);
  const selected = isPending && optimistic !== null ? optimistic : current;

  // The scroll position at the moment of the click, restored once the navigation settles.
  //
  // `router.push(href, { scroll: false })` is the documented way to ask for this and it is passed
  // below, but on this page it does not hold: the navigation is genuinely client-side (a marker set
  // on `window` survives it) and the position still resets to 0 from ~2,600px. `<Link scroll={false}>`
  // behaves the same. So the flag is kept — it is correct and may start working — and the position is
  // ALSO restored here, which is what actually keeps the reader beside the chart they just toggled.
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
    <span className="metricseg" role="group" aria-label={ariaLabel} aria-busy={isPending}>
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
            setOptimistic(option.value);
            restoreTo.current = window.scrollY;
            startTransition(() => {
              router.push(option.href, { scroll: false });
            });
          }}
        >
          {option.label}
        </a>
      ))}
    </span>
  );
}
