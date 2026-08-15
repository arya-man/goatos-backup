"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useState, useTransition } from "react";

import { DateRangePicker, type DateRangePickerLabels } from "@/components/date-range-picker";
import { VIDEO_LOG_DATE_KEY, VIDEO_LOG_PANEL_ID, VIDEO_LOG_PANEL_SELECTION_KEY } from "./video-log-params";

/**
 * The Video Log's own day picker.
 *
 * A SINGLE day, always — the video log answers "what arrived from this shed today", and a range
 * would make the arrival times ambiguous about which day they belong to. The shared DateRangePicker
 * expresses a single day as from === to, so this passes the same value on both ends and hands back
 * only the start of whatever the calendar returns.
 *
 * It is deliberately SEPARATE from the queue's capture-date filter (`vd_from`/`vd_to`): that one is
 * oversight-gated chrome that reshapes the queue, and the verifier does not have it. This one is a
 * plain day selector on a read she does have, and it never touches the queue below.
 *
 * TODAY IS THE DEFAULT AND IS EXPRESSED BY ABSENCE. Writing today's date into the URL would make a
 * bookmark mean "15 Aug" forever instead of "today", which is the whole point of a landing default
 * — the same convention ActionsDateFilter uses for the queue's own dates.
 */
export function VideoLogDateFilter({
  labels,
  basePath,
  day,
  today,
}: {
  labels: DateRangePickerLabels;
  basePath: string;
  /** The day currently rendered, "YYYY-MM-DD", resolved by the backend. */
  day: string;
  /** Today's business day (Asia/Kolkata), resolved on the server. */
  today: string;
}) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [pending, startTransition] = useTransition();

  // The App Router refresh is async, so `day` still describes the OLD selection while it runs.
  // Rendering the requested value immediately is what stops the label and the highlighted cell from
  // snapping back to the previous date for the length of the round trip. The optimistic value
  // remembers WHICH server selection it overrides and is derived away once the props move off it,
  // rather than cleared by an effect that would repaint after the correct value already landed.
  const [optimistic, setOptimistic] = useState<{ day: string; overrides: string } | null>(null);
  const selected = optimistic?.overrides === day ? optimistic.day : day;

  function apply(nextFrom: string): void {
    setOptimistic({ day: nextFrom, overrides: day });

    const next = new URLSearchParams(searchParams?.toString() ?? "");
    if (nextFrom === today) next.delete(VIDEO_LOG_DATE_KEY);
    else next.set(VIDEO_LOG_DATE_KEY, nextFrom);
    // KEEP THE PANEL OPEN across the day change.
    //
    // The trigger opens this panel with a URL HASH (#vi_video_log=open), which is client-local
    // state. Rebuilding the query string here drops that hash, so every date pick closed the drawer
    // the user was working in. Promoting the panel selection into the QUERY makes it survive the
    // navigation and keeps the deep link honest — the server reads the same key for initialOpen.
    next.set(VIDEO_LOG_PANEL_SELECTION_KEY, VIDEO_LOG_PANEL_ID);
    // The selected SHED is deliberately kept: "same pen, previous day" is the natural next question
    // once a shed's day is open. A shed with no arrivals that day renders its own empty line, which
    // is the honest answer rather than a silent bounce back to the shed list.
    const qs = next.toString();
    startTransition(() => {
      router.replace(qs ? `${basePath}?${qs}` : basePath, { scroll: false });
    });
  }

  return (
    <DateRangePicker
      labels={labels}
      from={selected}
      to={selected}
      today={today}
      busy={pending}
      // Hides the single/range tabs outright rather than accepting a span and quietly keeping only
      // its start — a control that does not do what it offers is worse than one that is absent.
      singleDayOnly
      onChange={(nextFrom) => apply(nextFrom)}
    />
  );
}
