"use client";

import CardActionArea from "@mui/material/CardActionArea";

import { KpiWidget, type KpiWidgetProps } from "@/components/app/kpi-widget";

/**
 * Client KPI adapter for a drill-in (drawer / filter): the verbatim template widget inside an MUI
 * CardActionArea (button role, Enter/Space, focus ring). Without `onClick` it is the plain widget,
 * so a zero-count tile stays inert.
 */
export function KpiWidgetAction({ onClick, sx, ...props }: KpiWidgetProps & { onClick?: () => void }) {
  if (!onClick) return <KpiWidget {...props} sx={sx} />;
  return (
    <CardActionArea onClick={onClick} sx={{ height: 1, borderRadius: 1.5 }}>
      <KpiWidget {...props} sx={[{ height: 1 }, ...(Array.isArray(sx) ? sx : [sx])]} />
    </CardActionArea>
  );
}
