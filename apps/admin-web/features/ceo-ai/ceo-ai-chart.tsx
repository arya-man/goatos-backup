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
      {layout.kind === "bar" ? (
        <ul className="mzai-chart-bars" aria-hidden="true">
          {layout.bars.map((bar) => (
            <li key={bar.key} className="mzai-chart-row" title={`${bar.label}: ${bar.valueLabel}`}>
              <span className="mzai-chart-label">{bar.label}</span>
              <span className="mzai-chart-track">
                <span className="mzai-chart-area">
                  <span className="mzai-chart-bar" style={{ width: `${bar.pct}%`, background: bar.color }} />
                </span>
                <span className="mzai-chart-value">{bar.valueLabel}</span>
              </span>
            </li>
          ))}
        </ul>
      ) : (
        <div className="mzai-chart-line" aria-hidden="true">
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
            <path
              d={layout.path}
              fill="none"
              stroke={layout.color}
              strokeWidth="2"
              strokeLinejoin="round"
              strokeLinecap="round"
              vectorEffect="non-scaling-stroke"
            />
          </svg>
          {layout.points.map((point) => (
            <span
              key={point.key}
              className="mzai-chart-dot"
              style={{
                left: `${(point.cx / layout.viewWidth) * 100}%`,
                top: `${(point.cy / layout.viewHeight) * 100}%`,
                background: layout.color,
              }}
              title={`${point.label}: ${point.value}`}
            />
          ))}
          <div className="mzai-chart-ticks">
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
