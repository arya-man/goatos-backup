"use client";

import { useRef, useState } from "react";

import { DenseToggle } from "./dense-toggle";

/**
 * A density switch that needs no wiring: it tightens the nearest table card it sits in (the
 * closest MUI Card, else the nearest table scroller) by toggling `data-dense`, which AppBaseline
 * (theme/app-baseline.tsx) reads to give its body cells MUI's small-table padding.
 * For footers rendered by server components, where a target id or callback is not available.
 */
export function DenseToggleAuto({ label = "Dense" }: { label?: string }) {
  const [dense, setDense] = useState(false);
  const anchor = useRef<HTMLSpanElement | null>(null);
  return (
    <span ref={anchor}>
      <DenseToggle
        checked={dense}
        label={label}
        onChange={(next) => {
          setDense(next);
          const host = anchor.current?.closest<HTMLElement>(".MuiCard-root") ?? anchor.current?.closest<HTMLElement>("[role=group]");
          host?.toggleAttribute("data-dense", next);
        }}
      />
    </span>
  );
}
