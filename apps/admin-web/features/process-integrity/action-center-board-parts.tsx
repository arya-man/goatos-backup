"use client";

// Client leaves of the Action Center board: the pieces that need a themed (function) sx, which a
// Server Component may not pass across the RSC line (guard: server-function-prop). Presentational
// only; the server page owns every string, href and count.
import type { ReactNode } from "react";
import type { Theme } from "@mui/material/styles";
import type { PaletteColorKey } from "@/theme/core";

import Link from "@/components/no-prefetch-link";
import { KanbanItemRoot } from "@/components/app/kanban";
import { CourseWidgetSummary } from "@/components/minimal/sections/overview/course/course-widget-summary";

// The template's selected-card ring (sections/checkout/checkout-payment-methods.tsx,
// sections/payment/payment-methods.tsx): a neutral 2px text.primary ring. Never a status-coloured
// outline (TR1-#26: the selected "All" tile read as a bright blue focus box).
// guard: action-center-kpi-selected
export const SELECTED_TILE_SX = (theme: Theme) => ({ boxShadow: `0 0 0 2px ${theme.vars.palette.text.primary}` });

export function ActionCenterQuickTile({
  href,
  on,
  title,
  total,
  color,
  icon,
}: {
  href: string;
  on: boolean;
  title: string;
  total: number;
  color: PaletteColorKey;
  icon: string;
}) {
  return (
    <Link href={href} scroll={false} aria-pressed={on} style={{ display: "block", height: "100%", color: "inherit", textDecoration: "none" }}>
      <CourseWidgetSummary title={title} total={total} color={color} icon={icon} sx={[{ height: 1 }, on ? SELECTED_TILE_SX : false]} />
    </Link>
  );
}

// Raised card on the neutral column in both schemes (the same treatment as the /work-board kanban
// cards: paper + divider border + card shadow; the template ItemRoot is grey[900] in dark, which
// reads as sunken against the column).
const CARD_ROOT_SX = (t: Theme) => ({
  bgcolor: "background.paper",
  border: `1px solid ${t.vars.palette.divider}`,
  boxShadow: t.vars.customShadows.card,
  ...t.applyStyles("dark", { bgcolor: "background.paper" }),
});

/** Template kanban item shell (`li`) for one Action Center card; the search filter hides it whole. */
export function ActionCenterCardShell({ children }: { children: ReactNode }) {
  return (
    <KanbanItemRoot data-filter-row data-ac-card sx={CARD_ROOT_SX}>
      {children}
    </KanbanItemRoot>
  );
}
