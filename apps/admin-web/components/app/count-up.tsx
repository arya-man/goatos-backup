"use client";

import Box from "@mui/material/Box";
import { animate, useInView, useReducedMotion } from "motion/react";
import { useEffect, useRef, useState } from "react";

export type CountUpProps = {
  value: number;
  /** Formats the in-flight and final number. Default: toLocaleString with `digits` decimals. */
  format?: (n: number) => string;
  digits?: number;
  /** ms, default 900 */
  duration?: number;
  className?: string;
};

// Paths whose KPIs have already counted up in this document. A tab switch or refetch re-mounts the
// cards on the same path; re-running the count there shows wrong intermediate numbers (People
// "Not clocked in" read 22, then 27, before 34), so later mounts render the final value directly.
const countedPaths = new Set<string>();

/** Counts from 0 to value once per page, on first view. Reduced motion renders the final value immediately. */
export function CountUp({ value, format, digits = 0, duration = 900, className }: CountUpProps) {
  const ref = useRef<HTMLSpanElement>(null);
  const inView = useInView(ref, { once: true, margin: "-40px" });
  const reduce = useReducedMotion();
  const played = useRef(false);
  const fmt = format ?? ((n: number) => n.toLocaleString(undefined, { minimumFractionDigits: digits, maximumFractionDigits: digits }));
  const [shown, setShown] = useState<number>(value);
  // An absent/invalid figure renders as a dash, never "NaN".
  const valid = Number.isFinite(value);

  useEffect(() => {
    if (reduce || !Number.isFinite(value)) {
      const id = window.setTimeout(() => setShown(value), 0);
      return () => window.clearTimeout(id);
    }
    if (!inView) return;
    const path = window.location.pathname;
    if (played.current || countedPaths.has(path)) {
      played.current = true;
      const id = window.setTimeout(() => setShown(value), 0);
      return () => window.clearTimeout(id);
    }
    played.current = true;
    // Mark the path once the first count has had time to land, so sibling cards on the first load
    // still animate together.
    const mark = window.setTimeout(() => countedPaths.add(path), duration);
    // In-flight frames are rounded to `digits` so a caller format (e.g. toLocaleString("en-IN"))
    // never prints "9.633" for an integer count, and an interrupted count lands on the final value.
    const scale = 10 ** digits;
    const controls = animate(0, value, {
      duration: duration / 1000,
      ease: "easeOut",
      onUpdate: (n) => setShown(Math.round(n * scale) / scale),
      onComplete: () => setShown(value),
    });
    return () => {
      controls.stop();
      setShown(value);
      window.clearTimeout(mark);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inView, value, reduce, duration]);

  return (
    <Box component="span" ref={ref} className={className} sx={{ fontVariantNumeric: "tabular-nums" }}>
      {valid && Number.isFinite(shown) ? fmt(shown) : "\u2014"}
    </Box>
  );
}
