import type { ReactElement } from "react";
import {
  chartAccessibleLabel,
  chartLayout,
  isRenderableChart,
  type CeoAiChart as CeoAiChartData,
} from "./ceo-ai-chart-geometry";

// Inline-SVG renderer for the leadership assistant's optional answer chart.
// It is a thin view over ceo-ai-chart-geometry (the sanctioned mesha viz
// pattern ported from components/svg-bars.tsx). No charting library: recharts
// stays unused. Every colour is a theme token, so the chart is correct in light
// and dark. The SVG scales to the bubble width via viewBox + width="100%".
//
// Renders nothing when the chart is absent or not renderable (< 2 points), so
// the assistant bubble falls back to its text answer.
export function CeoAiChart({ chart }: { chart: CeoAiChartData | undefined }): ReactElement | null {
  if (!isRenderableChart(chart)) return null;
  const layout = chartLayout(chart);
  if (!layout) return null;

  const label = chartAccessibleLabel(chart);

  return (
    <figure className="mzai-chart" role="img" aria-label={label}>
      <figcaption className="mzai-chart-title">{chart.title}</figcaption>
      {layout.legend.length ? (
        <ul className="mzai-chart-legend" aria-hidden="true">
          {layout.legend.map((item) => (
            <li key={item.name}>
              <span className="mzai-chart-swatch" style={{ background: item.color }} />
              {item.name}
            </li>
          ))}
        </ul>
      ) : null}
      {layout.kind === "bar" ? (
        <ul className="mzai-chart-bars" aria-hidden="true">
          {layout.bars.map((bar) => (
            <li key={bar.key} className="mzai-chart-row" title={`${bar.label}: ${bar.valueLabel}`}>
              <span className="mzai-chart-label">{bar.label}</span>
              {(bar.parts ?? [{ name: "", value: bar.value, valueLabel: bar.valueLabel, pct: bar.pct, color: bar.color }]).map((part) => (
                <span key={part.name || "value"} className="mzai-chart-track" title={part.name ? `${bar.label} · ${part.name}: ${part.valueLabel}` : undefined}>
                  <span className="mzai-chart-area">
                    <span className="mzai-chart-bar" style={{ width: `${part.pct}%`, background: part.color }} />
                  </span>
                  <span className="mzai-chart-value">{part.valueLabel}</span>
                </span>
              ))}
            </li>
          ))}
        </ul>
      ) : (
        <div className="mzai-chart-line" aria-hidden="true">
          <div className="mzai-chart-plot">
          <div className="mzai-chart-yaxis">
            {layout.yTicks.map((tick) => (
              <span key={tick.value} className="mzai-chart-ytick" style={{ top: `${tick.pct}%` }}>
                {tick.label}
              </span>
            ))}
          </div>
          <svg
            className="mzai-chart-svg"
            viewBox={`0 0 ${layout.viewWidth} ${layout.viewHeight}`}
            width="100%"
            preserveAspectRatio="none"
          >
            <line
              x1="0"
              y1={layout.baselineY}
              x2={layout.viewWidth}
              y2={layout.baselineY}
              stroke="var(--line)"
              strokeWidth="1"
              vectorEffect="non-scaling-stroke"
            />
            {layout.zeroY !== null ? (
              <line
                x1="0"
                y1={layout.zeroY}
                x2={layout.viewWidth}
                y2={layout.zeroY}
                stroke="var(--muted)"
                strokeWidth="1"
                strokeDasharray="3 3"
                vectorEffect="non-scaling-stroke"
              />
            ) : null}
            {layout.lines.map((line) => (
              <path
                key={line.name}
                d={line.path}
                fill="none"
                stroke={line.color}
                strokeWidth="2"
                strokeLinejoin="round"
                strokeLinecap="round"
                vectorEffect="non-scaling-stroke"
              />
            ))}
          </svg>
          {layout.lines.flatMap((line) =>
            line.points.filter((point) => point.cy !== null).map((point) => (
              <span
                key={point.key}
                className="mzai-chart-dot"
                style={{
                  left: `${(point.cx / layout.viewWidth) * 100}%`,
                  top: `${((point.cy ?? 0) / layout.viewHeight) * 100}%`,
                  background: line.color,
                }}
                title={`${layout.lines.length > 1 ? `${line.name} · ` : ""}${point.label}: ${point.value}`}
              />
            )),
          )}
          </div>
          <div
            className="mzai-chart-ticks"
            // Tick labels never overlap: an end label spans at most 2/3 of the gap.
            style={{ ["--tick-max" as string]: `${(100 / Math.max(layout.ticks.length - 1, 1)) * 0.64}%` }}
          >
            {layout.ticks.map((i, k) => {
              const point = layout.points[i];
              const edge = k === 0 ? " start" : k === layout.ticks.length - 1 ? " end" : "";
              return (
                <span
                  key={point.key}
                  className={`mzai-chart-tick${edge}`}
                  style={{ left: `${(point.cx / layout.viewWidth) * 100}%` }}
                >
                  {point.label}
                </span>
              );
            })}
          </div>
        </div>
      )}
    </figure>
  );
}
