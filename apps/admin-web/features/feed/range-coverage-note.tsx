"use client";

// Transient coverage note for the Feed Analytics range switch (maintainer
// request 2026-08-21): when the selected window is wider than the data the
// sheets actually cover, surface a small note for two seconds instead of
// letting a 60-day chart quietly render 20 days of bars. The MESSAGE arrives
// fully composed from the backend page contract — this component owns only the
// show-then-fade timing (client-local presentation state, per the contract
// rule), so it carries no copy of its own.
//
// telemetry:exempt pure presentational transient note — no user action to
// track, and the route's Faro view + error boundary already cover the page.

import { useEffect, useState } from "react";

export function RangeCoverageNote({ message }: { message: string }) {
  const [visible, setVisible] = useState(true);
  useEffect(() => {
    const timer = setTimeout(() => setVisible(false), 2000);
    return () => clearTimeout(timer);
  }, [message]);
  if (!visible) return null;
  return (
    <div
      role="status"
      aria-live="polite"
      className="card"
      style={{
        position: "fixed",
        top: 64,
        left: "50%",
        transform: "translateX(-50%)",
        zIndex: 60,
        padding: "8px 14px",
        fontSize: 13,
        color: "var(--amber)",
        borderColor: "var(--amber)",
      }}
    >
      {message}
    </div>
  );
}
