"use client";

import { useSyncExternalStore } from "react";

const mobileQuery = "(max-width: 639px)";

function subscribe(onStoreChange: () => void): () => void {
  const mq = window.matchMedia(mobileQuery);
  mq.addEventListener("change", onStoreChange);
  return () => mq.removeEventListener("change", onStoreChange);
}

function getSnapshot(): boolean {
  return window.matchMedia(mobileQuery).matches;
}

function getServerSnapshot(): boolean {
  return false;
}

/** Returns true when viewport width is below 640px (Tailwind's `sm` breakpoint). */
export function useIsMobile(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
