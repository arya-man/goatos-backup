import { Children, type ReactNode } from "react";

import { cx } from "@/lib/tone";

/**
 * KPI card grid. Card count drives a balanced layout (no orphan card, no dead slot) once the
 * container is wide enough; below that the auto-fit track takes over. Gap 24 = the template Grid
 * spacing={3}. Layout rules per data-n live in `app/minimal-theme.css`.
 */
export function KpiGrid({ children, min = 220, className }: { children: ReactNode; min?: number; className?: string }) {
  const n = Children.toArray(children).filter(Boolean).length;
  return (
    <div className="kit-kpi-wrap">
      <div className={cx("kit-kpi-grid", className)} data-n={n} style={{ gridTemplateColumns: `repeat(auto-fit,minmax(min(${min}px,100%),1fr))` }}>
        {children}
      </div>
    </div>
  );
}
