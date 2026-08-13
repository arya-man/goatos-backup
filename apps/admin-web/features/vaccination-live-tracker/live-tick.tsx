"use client";

import { useEffect, useRef, useState } from "react";

// The mock's "▲ live" tick is `opacity:0` by default and flashes `.on` only when a tile's value
// actually bumps (mock lines 81-82, 418-419, 432). Rendering it permanently — which is what shipped
// — asserts liveness that is not being observed, including after the reader clicks LIVE → PAUSED,
// when the board is explicitly stale and the poller has stopped.
//
// This is deliberately derived from the TILE VALUE, not from the poller's live flag: a poll that
// returns the same numbers is not a bump, and the tick's whole job is to say "this number just
// moved". A paused board never receives a new value, so the tick never fires.
const FLASH_MS = 2600;

export function LiveTick({ value, label }: { value: number; label: string }) {
  const previous = useRef<number | null>(null);
  const [flashing, setFlashing] = useState(false);

  useEffect(() => {
    const was = previous.current;
    previous.current = value;
    if (was === null || was === value) return;
    setFlashing(true);
    const timer = window.setTimeout(() => setFlashing(false), FLASH_MS);
    return () => window.clearTimeout(timer);
  }, [value]);

  return (
    <span className={`lt-tick${flashing ? " on" : ""}`} aria-hidden={!flashing}>
      {label}
    </span>
  );
}
