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
        <svg
          className="mzai-chart-svg"
          viewBox={`0 0 ${layout.viewWidth} ${layout.viewHeight}`}
          width="100%"
          aria-hidden="true"
        >
          {layout.bars.map((bar) => (
            <g key={bar.key}>
              <text x={bar.labelX} y={bar.textY} fontSize="9" fill="var(--muted)">
                {bar.label}
              </text>
              <rect
                x={bar.x}
                y={bar.y}
                width={bar.width}
                height={bar.height}
                rx="4"
                fill={bar.color}
              >
                <title>{`${bar.label}: ${bar.value}`}</title>
              </rect>
              <text x={bar.valueX} y={bar.textY} fontSize="9" fill="var(--ink)" fontWeight="700">
                {bar.value}
              </text>
            </g>
          ))}
        </svg>
      ) : (
        <svg
          className="mzai-chart-svg"
          viewBox={`0 0 ${layout.viewWidth} ${layout.viewHeight}`}
          width="100%"
          aria-hidden="true"
        >
          <line
            x1="0"
            y1={layout.baselineY}
            x2={layout.viewWidth}
            y2={layout.baselineY}
            stroke="var(--line)"
            strokeWidth="1"
          />
          <path d={layout.path} fill="none" stroke={layout.color} strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />
          {layout.points.map((point) => (
            <g key={point.key}>
              <circle cx={point.cx} cy={point.cy} r="2.5" fill={layout.color}>
                <title>{`${point.label}: ${point.value}`}</title>
              </circle>
              <text
                x={point.cx}
                y={layout.baselineY + 12}
                fontSize="8"
                fill="var(--muted)"
                textAnchor="middle"
              >
                {point.label}
              </text>
            </g>
          ))}
        </svg>
      )}
    </figure>
  );
}
