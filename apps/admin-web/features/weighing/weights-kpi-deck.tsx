"use client";

import { Activity, Scale, TrendingUp, Weight } from "lucide-react";
import { EmptyState } from "@/components/app/empty-state";
import { GoatGlyph } from "@/components/goat-glyph";
import type { ReactNode } from "react";
import { TrendChart } from "@/components/app/trend-chart";
import Card from "@mui/material/Card";
import CardHeader, { cardHeaderClasses } from "@mui/material/CardHeader";
import type { KitTone } from "@/lib/tone";
import { KpiCard, KpiGrid, type KpiPart } from "@/components/minimal/widgets";

/**
 * Presentation-only KPI deck for the Weights pages. The server component resolves every figure
 * and every label (backend copy) and hands plain serialisable data down; this file only decides
 * how it looks. A null value is a real "no data" state and renders the supplied text, never 0.
 */
const ICONS = {
  kids: GoatGlyph,
  total: Weight,
  average: Scale,
  over30: Weight,
  over35: Weight,
  gain: TrendingUp,
  activity: Activity,
} as const;

export type WeightsKpiIcon = keyof typeof ICONS;

export type WeightsKpi = {
  key: string;
  label: ReactNode;
  /** Numeric value (count-up) or a pre-rendered string; null = no data. */
  value: number | string | null;
  noDataText: string;
  digits?: number;
  unit?: string;
  hint?: ReactNode;
  tone?: KitTone;
  icon?: WeightsKpiIcon;
  sparkline?: number[];
  /** Signed change vs. previous point, in `trendSuffix` units. */
  trend?: number | null;
  trendSuffix?: string;
  /** Two readings with their own labels (template split card), e.g. Individual / Lump-sum. */
  parts?: KpiPart[];
};

const fmtIn = (digits: number) => (n: number) =>
  n.toLocaleString("en-IN", { minimumFractionDigits: digits, maximumFractionDigits: digits });

export function WeightsKpiDeck({ items, min = 200, ariaLabel }: { items: WeightsKpi[]; min?: number; ariaLabel: string }) {
  return (
    <section aria-label={ariaLabel} className="wt-kit-kpis">
      <KpiGrid min={min}>
        {items.map((item) => {
          const Icon = item.icon ? ICONS[item.icon] : null;
          const digits = item.digits ?? 0;
          const hasSpark = (item.sparkline?.length ?? 0) > 1;
          return (
            <KpiCard
              key={item.key}
              label={item.label}
              value={item.value == null ? <span className="muted">{item.noDataText}</span> : item.value}
              format={fmtIn(digits)}
              digits={digits}
              unit={item.value == null ? undefined : item.unit}
              tone={item.tone ?? "primary"}
              icon={Icon && !hasSpark ? <Icon size={22} strokeWidth={2} /> : undefined}
              sparkline={hasSpark ? item.sparkline : undefined}
              trend={
                item.trend == null
                  ? undefined
                  : { value: item.trend, suffix: item.trendSuffix ?? "", digits: 0 }
              }
              hint={item.hint}
              parts={item.value == null ? undefined : item.parts}
            />
          );
        })}
      </KpiGrid>
    </section>
  );
}

export type GainTrendPoint = { week: string; label: string; gain: number; animals: number };

/** Weekly daily-gain area chart with the kit's soft tooltip card. */
export function GainTrendCard({
  title,
  subtitle,
  seriesLabel,
  points,
  emptyLabel,
  action,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  seriesLabel: string;
  points: GainTrendPoint[];
  emptyLabel: ReactNode;
  action?: ReactNode;
}) {
  return (
    <Card className="wt-kit-trend" sx={{ p: { xs: 2, sm: 3 } }}>
      <CardHeader
        title={title}
        subheader={subtitle}
        action={action}
        sx={{
          p: 0,
          mb: 2,
          flexWrap: "wrap",
          rowGap: 1.5,
          [`& .${cardHeaderClasses.action}`]: { m: 0, flex: { xs: "1 1 100%", sm: "0 0 auto" }, minWidth: 0, maxWidth: "100%" },
        }}
      />
      {points.length < 2 ? (
        <EmptyState title={emptyLabel} />
      ) : (
        <TrendChart
          data={points}
          xKey="label"
          kind="area"
          height={280}
          series={[{ key: "gain", label: seriesLabel }]}
          valueFormat={(n) => `${Math.round(Number(n)).toLocaleString("en-IN")} g`}
          yWidth={56}
        />
      )}
    </Card>
  );
}
