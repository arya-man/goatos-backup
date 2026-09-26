import * as React from "react";
import type { Decorator } from "@storybook/nextjs-vite";

/** Page-like padded surface on the app background, so stories read like a screen. */
export function Frame({ children, width, title }: { children: React.ReactNode; width?: number | string; title?: string }) {
  return (
    <div style={{ background: "var(--bg)", color: "var(--fg)", minHeight: "100vh", padding: "clamp(12px, 4vw, 24px)", display: "grid", alignContent: "start", gap: 12 }}>
      {title ? (
        <h2 style={{ fontSize: 13, fontWeight: 700, letterSpacing: ".06em", textTransform: "uppercase", color: "var(--fg-muted)", margin: "0 0 12px" }}>
          {title}
        </h2>
      ) : null}
      <div style={{ maxWidth: width ?? "100%", minWidth: 0 }}>{children}</div>
    </div>
  );
}

/** Stacks labelled examples so one story can show many states at once. */
export function Grid({ children, min = 320 }: { children: React.ReactNode; min?: number }) {
  return <div style={{ display: "grid", gap: 20, gridTemplateColumns: `repeat(auto-fit, minmax(min(${min}px, 100%), 1fr))` }}>{children}</div>;
}

export function Labelled({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: "grid", gap: 8, minWidth: 0 }}>
      <span style={{ fontSize: 11, fontWeight: 700, letterSpacing: ".06em", textTransform: "uppercase", color: "var(--fg-faint, var(--fg-muted))" }}>
        {label}
      </span>
      {children}
    </div>
  );
}

export const withFrame: Decorator = (Story) => (
  <Frame>
    <Story />
  </Frame>
);

/** Story-level shorthand for the 390px lane. */
export const mobile = { viewport: { value: "mobile", isRotated: false } } as const;
