"use client";

import { useState, useEffect } from "react";

/** Returns true when the viewport matches the given media query string. */
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(
    () => typeof window !== "undefined" && window.matchMedia(query).matches
  );

  useEffect(() => {
    const mq = window.matchMedia(query);
    const handler = (e: MediaQueryListEvent) => setMatches(e.matches);
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, [query]);

  return matches;
}

/** Convenience: true when viewport is ≥ 640px (Tailwind `sm`). */
export function useIsSmUp(): boolean {
  return useMediaQuery("(min-width: 640px)");
}
