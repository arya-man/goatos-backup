"use client";

import { useState } from "react";
import { Scale } from "lucide-react";

import { WeightBars, type WeightBar } from "./weight-bars";

type Metric = "adg" | "weight";

type MetricLabels = {
  adg: string;
  weight: string;
};

type MetricSeries = {
  data: readonly WeightBar[];
  emptyLabel: string;
  unit: string;
  chartLabel: string;
};

function MetricToggle({
  current,
  labels,
  onChange,
}: {
  current: Metric;
  labels: MetricLabels;
  onChange: (next: Metric) => void;
}) {
  return (
    <span className="chips" aria-label="Chart metric">
      {(["adg", "weight"] as const).map((option) => (
        <button
          className={`chip${option === current ? " on" : ""}`}
          key={option}
          onClick={() => onChange(option)}
          type="button"
        >
          {labels[option]}
        </button>
      ))}
    </span>
  );
}

export function MetricChart({
  initialMetric,
  labels,
  title,
  caption,
  series,
  size = "short",
  wide = false,
  className = "card wchart",
}: {
  initialMetric: Metric;
  labels: MetricLabels;
  title: MetricLabels;
  caption?: string;
  series: Record<Metric, MetricSeries>;
  size?: "tall" | "short";
  wide?: boolean;
  className?: string;
}) {
  const [metric, setMetric] = useState<Metric>(initialMetric);
  const active = series[metric];
  return (
    <section className={className} aria-label={active.chartLabel}>
      <h2 className="h">
        {title[metric]}
        <MetricToggle current={metric} labels={labels} onChange={setMetric} />
      </h2>
      {caption ? <p className="muted small">{caption}</p> : null}
      <WeightBars
        data={active.data}
        emptyLabel={active.emptyLabel}
        unit={active.unit}
        chartLabel={active.chartLabel}
        size={size}
        wide={wide}
      />
    </section>
  );
}

type ShedChartColumn = {
  heading: string;
  rows: readonly WeightBar[];
};

type ShedSeries = MetricSeries & {
  columns: readonly ShedChartColumn[];
  domain: { lo: number; hi: number };
  size: "tall" | "short";
  caption: string;
};

export function ShedMetricChart({
  initialMetric,
  labels,
  title,
  series,
}: {
  initialMetric: Metric;
  labels: MetricLabels;
  title: MetricLabels;
  series: Record<Metric, ShedSeries>;
}) {
  const [metric, setMetric] = useState<Metric>(initialMetric);
  const active = series[metric];
  return (
    <section className="card wchart" aria-label={active.chartLabel}>
      <h2 className="h">
        <Scale className="ic" size={15} aria-hidden /> {title[metric]}
        <MetricToggle current={metric} labels={labels} onChange={setMetric} />
      </h2>
      <p className="muted small">{active.caption}</p>
      {active.columns.length === 0 ? (
        <WeightBars
          data={[]}
          emptyLabel={active.emptyLabel}
          unit={active.unit}
          chartLabel={active.chartLabel}
          size={active.size}
        />
      ) : (
        <div className="wcols">
          {active.columns.map((col, index) => (
            <div key={`${col.heading}|${index}`}>
              <h3 className="wcol-h">{col.heading}</h3>
              <WeightBars
                data={col.rows}
                domain={active.domain}
                emptyLabel={active.emptyLabel}
                unit={active.unit}
                chartLabel={active.chartLabel}
                size={active.size}
              />
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
