"use client";

// telemetry:exempt presentational chart tooltip wrapper — no user action, no data read

// Instant hover tooltips for server-rendered SVG charts.
//
// The repo's charts carry native SVG <title> tooltips, which browsers show only
// after a ~1s dwell and only while the pointer sits dead still — operators read
// that as "no tooltip at all". This wrapper adds an immediate tooltip WITHOUT
// making the charts client components: the chart stays a server-rendered SVG,
// marks (or invisible hit strips) carry a `data-tip` attribute, and this thin
// client shell only listens for pointer movement over them by delegation.
//
// It renders NO copy of its own: every `data-tip` string arrives already
// composed by the server component from backend contract copy.

import { useCallback, useRef, useState } from "react";

type Tip = { text: string; x: number; y: number };

export function ChartHover({ children }: { children: React.ReactNode }) {
  const [tip, setTip] = useState<Tip | null>(null);
  const frame = useRef<number | null>(null);

  const onMove = useCallback((event: React.MouseEvent<HTMLDivElement>) => {
    const target = (event.target as Element).closest("[data-tip]");
    const text = target?.getAttribute("data-tip") ?? null;
    const x = event.clientX;
    const y = event.clientY;
    if (frame.current !== null) cancelAnimationFrame(frame.current);
    frame.current = requestAnimationFrame(() => {
      setTip(text ? { text, x, y } : null);
    });
  }, []);

  const onLeave = useCallback(() => {
    if (frame.current !== null) cancelAnimationFrame(frame.current);
    setTip(null);
  }, []);

  return (
    <div onMouseMove={onMove} onMouseLeave={onLeave} style={{ position: "relative" }}>
      {children}
      {tip ? (
        // Newline-separated tip: line 1 is the header (the day), every other
        // line renders as its own list row so a stacked day reads as a list,
        // never one long sentence.
        <div
          role="status"
          style={{
            position: "fixed",
            left: Math.min(tip.x + 14, typeof window === "undefined" ? tip.x : window.innerWidth - 280),
            top: tip.y + 16,
            zIndex: 30,
            pointerEvents: "none",
            background: "var(--panel)",
            color: "var(--ink)",
            border: "1px solid var(--line)",
            borderRadius: "var(--r)",
            padding: "7px 11px",
            fontSize: "11.5px",
            boxShadow: "var(--shadow)",
            maxWidth: 300,
          }}
        >
          {tip.text.split("\n").map((line, index) => (
            <div
              key={index}
              style={
                index === 0
                  ? { fontWeight: 700, marginBottom: 3 }
                  : { fontWeight: 500, color: "var(--muted)", padding: "1px 0" }
              }
            >
              {line}
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}
