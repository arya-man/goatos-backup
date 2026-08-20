// Server-rendered SVG series charts, following the mock's chart anatomy and the
// repo chart rules: pure SERVER components, no charting library (recharts stays
// at zero importers), CSS-custom-property fills only (no hex literals), a hover
// hit strip per slot, and NO copy of their own — every visible string arrives
// resolved from the backend page contract by the caller.
//
// These live in components/ rather than inside one feature because two pages now
// draw the same marks (Feed Analytics over feed days, Herd Analytics over
// calendar months). A second hand-rolled copy is how two screens start drawing
// the same fact differently; the feature files re-export from here so there is
// exactly one implementation.
//
// Series colours reuse the exact ordering of components/svg-bars.tsx so an
// entity keeps the same colour on every chart of a page (colour follows the
// entity, never its rank on one chart).

export const SERIES_VARS = [
  "var(--brand)",
  "var(--info)",
  "var(--amber)",
  "var(--purple)",
  "var(--teal)",
  "var(--danger)",
  "var(--ok)",
] as const;

const VIEW_W = 560;
const VIEW_H = 168;
const PAD_X = 34;
const PAD_TOP = 10;
const BASELINE = VIEW_H - 18;

const nf = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 1 });

export type StackedDay = {
  key: string;
  /** Tooltip label for the day, resolved by the caller. */
  label: string;
  /** Segment values in series order; the caller aligns them with its legend. */
  segments: number[];
};

/**
 * Slot-by-slot stacked columns on one shared scale; a
 * 1px panel gap between segments per the mark spec so adjacent fills never
 * bleed together.
 */
export function StackedColumns({
  days,
  seriesLabels,
  valueNoun,
  chartLabel,
  emptyLabel,
}: {
  days: StackedDay[];
  seriesLabels: string[];
  valueNoun: string;
  chartLabel: string;
  emptyLabel: string;
}) {
  if (days.length === 0) {
    return (
      <div className="muted small" style={{ padding: "12px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }
  const totals = days.map((d) => d.segments.reduce((a, b) => a + b, 0));
  const max = Math.max(1, ...totals);
  const slot = (VIEW_W - 2 * PAD_X) / days.length;
  const barW = Math.max(2, Math.min(14, slot - 3));
  return (
    <svg
      viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
      role="img"
      aria-label={chartLabel}
      style={{ width: "100%", height: "auto", display: "block" }}
    >
      {[0.25, 0.5, 0.75, 1].map((f) => (
        <line
          key={f}
          x1={PAD_X}
          x2={VIEW_W - 6}
          y1={BASELINE - (BASELINE - PAD_TOP) * f}
          y2={BASELINE - (BASELINE - PAD_TOP) * f}
          stroke="var(--line)"
          strokeWidth="1"
        />
      ))}
      {[0.5, 1].map((f) => (
        <text
          key={f}
          x={PAD_X - 5}
          y={BASELINE - (BASELINE - PAD_TOP) * f + 3}
          fontSize="9"
          textAnchor="end"
          fill="var(--faint)"
        >
          {nf(max * f)}
        </text>
      ))}
      <line x1={PAD_X} x2={VIEW_W - 6} y1={BASELINE} y2={BASELINE} stroke="var(--line)" strokeWidth="1" />
      {days.map((d, i) => {
        const x = PAD_X + i * slot + (slot - barW) / 2;
        let y = BASELINE;
        // Composed server-side from contract copy; surfaced instantly by the
        // ChartHover client wrapper via the hit strip below (native SVG <title>
        // needs a ~1s dwell, which operators read as "no tooltip").
        // Newline-separated: the ChartHover tooltip renders line 1 as the
        // header and every following line as its own list row.
        const tipText = [
          d.label,
          ...d.segments.map((v, s) => `${seriesLabels[s] ?? ""}  ${nf(v)} ${valueNoun}`),
        ].join("\n");
        return (
          <g key={d.key}>
            {d.segments.map((v, s) => {
              const h = ((BASELINE - PAD_TOP) * v) / max;
              y -= h;
              return h <= 0 ? null : (
                <rect
                  key={s}
                  x={x}
                  y={y + 0.5}
                  width={barW}
                  height={Math.max(0.5, h - 1)}
                  rx={s === d.segments.length - 1 ? 2 : 0}
                  fill={SERIES_VARS[s % SERIES_VARS.length]}
                />
              );
            })}
            <rect
              x={PAD_X + i * slot}
              y={PAD_TOP}
              width={slot}
              height={BASELINE - PAD_TOP}
              fill="transparent"
              data-tip={tipText}
            />
          </g>
        );
      })}
    </svg>
  );
}

export type LineSeries = {
  label: string;
  colorVar: string;
  /** One point per day slot; null = no figure that day (line breaks, honestly). */
  points: (number | null)[];
};

/** Multi-series line chart on one shared scale, endpoint dot per series. */
export function SeriesLines({
  series,
  dayLabels,
  valueNoun,
  chartLabel,
  emptyLabel,
}: {
  series: LineSeries[];
  dayLabels: string[];
  valueNoun: string;
  chartLabel: string;
  emptyLabel: string;
}) {
  const values = series.flatMap((s) => s.points.filter((p): p is number => p !== null));
  if (values.length === 0 || dayLabels.length === 0) {
    return (
      <div className="muted small" style={{ padding: "12px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }
  const max = Math.max(1, ...values);
  const stepX = (VIEW_W - 2 * PAD_X) / Math.max(1, dayLabels.length - 1);
  const yOf = (v: number) => BASELINE - ((BASELINE - PAD_TOP) * v) / max;
  return (
    <svg
      viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
      role="img"
      aria-label={chartLabel}
      style={{ width: "100%", height: "auto", display: "block" }}
    >
      {[0.25, 0.5, 0.75, 1].map((f) => (
        <line
          key={f}
          x1={PAD_X}
          x2={VIEW_W - 6}
          y1={BASELINE - (BASELINE - PAD_TOP) * f}
          y2={BASELINE - (BASELINE - PAD_TOP) * f}
          stroke="var(--line)"
          strokeWidth="1"
        />
      ))}
      {[0.5, 1].map((f) => (
        <text
          key={f}
          x={PAD_X - 5}
          y={BASELINE - (BASELINE - PAD_TOP) * f + 3}
          fontSize="9"
          textAnchor="end"
          fill="var(--faint)"
        >
          {nf(max * f)}
        </text>
      ))}
      <line x1={PAD_X} x2={VIEW_W - 6} y1={BASELINE} y2={BASELINE} stroke="var(--line)" strokeWidth="1" />
      {series.map((s) => {
        const segments: string[] = [];
        let current: string[] = [];
        s.points.forEach((p, i) => {
          if (p === null) {
            if (current.length > 0) segments.push(current.join(" "));
            current = [];
            return;
          }
          current.push(`${current.length === 0 ? "M" : "L"}${(PAD_X + i * stepX).toFixed(1)} ${yOf(p).toFixed(1)}`);
        });
        if (current.length > 0) segments.push(current.join(" "));
        const lastIdx: number = s.points.reduce<number>((acc, p, i) => (p === null ? acc : i), -1);
        const lastVal = lastIdx >= 0 ? s.points[lastIdx] : null;
        return (
          <g key={s.label}>
            {segments.map((d, i) => (
              <path key={i} d={d} fill="none" stroke={s.colorVar} strokeWidth="2" strokeLinejoin="round" />
            ))}
            {lastVal !== null && lastIdx >= 0 ? (
              <circle
                cx={PAD_X + lastIdx * stepX}
                cy={yOf(lastVal)}
                r="3.5"
                fill={s.colorVar}
                stroke="var(--panel)"
                strokeWidth="2"
              />
            ) : null}
          </g>
        );
      })}
      {dayLabels.map((day, i) => {
        const parts = series
          .map((s) => (s.points[i] === null ? null : `${s.label}  ${nf(s.points[i] as number)} ${valueNoun}`))
          .filter((p): p is string => p !== null);
        if (parts.length === 0) return null;
        return (
          <rect
            key={day}
            x={PAD_X + i * stepX - stepX / 2}
            y={PAD_TOP}
            width={stepX}
            height={BASELINE - PAD_TOP}
            fill="transparent"
            data-tip={[day, ...parts].join("\n")}
          />
        );
      })}
      <text x={PAD_X} y={VIEW_H - 4} fontSize="9" fill="var(--faint)">
        {dayLabels[0]}
      </text>
      <text x={VIEW_W - 6} y={VIEW_H - 4} fontSize="9" textAnchor="end" fill="var(--faint)">
        {dayLabels[dayLabels.length - 1]}
      </text>
    </svg>
  );
}

/** Shared legend row; the caller resolves labels and keeps series order stable. */
export function SeriesLegend({ entries }: { entries: { label: string; colorVar: string }[] }) {
  return (
    <div className="lg" style={{ display: "flex", flexWrap: "wrap", gap: "6px 14px" }}>
      {entries.map((entry) => (
        <span key={entry.label} style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
          <span
            aria-hidden
            style={{ width: 9, height: 9, borderRadius: 3, background: entry.colorVar, display: "inline-block" }}
          />
          {entry.label}
        </span>
      ))}
    </div>
  );
}
