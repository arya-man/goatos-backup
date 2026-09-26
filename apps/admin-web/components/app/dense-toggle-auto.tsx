"use client";

import { useRef, useState } from "react";

import { DenseToggle } from "./dense-toggle";

/**
 * A density switch that needs no wiring: it tightens the nearest table card it sits in (the
 * closest `.card` / `.kit-tablecard`, else the nearest `.tablewrap`) by toggling `kit-dense`.
 * For footers rendered by server components, where a target id or callback is not available.
 */
export function DenseToggleAuto({ label = "Dense" }: { label?: string }) {
  const [dense, setDense] = useState(false);
  const anchor = useRef<HTMLSpanElement | null>(null);
  return (
    <span ref={anchor} className="kit-dense-auto">
      <DenseToggle
        checked={dense}
        label={label}
        onChange={(next) => {
          setDense(next);
          const host = anchor.current?.closest<HTMLElement>(".card, .kit-tablecard, .MuiCard-root") ?? anchor.current?.closest<HTMLElement>(".tablewrap, .twrap");
          host?.classList.toggle("kit-dense", next);
        }}
      />
    </span>
  );
}
