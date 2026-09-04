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
  "var(--warn)",
  "var(--brand-d)",
  "var(--brand-l)",
] as const;

export function seriesColorVar(index: number) {
  const base = SERIES_VARS[index % SERIES_VARS.length];
  const cycle = Math.floor(index / SERIES_VARS.length);
  if (cycle === 0) return base;
  const mix = cycle % 2 === 1 ? "var(--ink)" : "var(--panel)";
  const share = cycle % 2 === 1 ? 74 : 82;
  return `color-mix(in srgb, ${base} ${share}%, ${mix})`;
}

const VIEW_W = 560;
const VIEW_H = 168;
const PAD_X = 34;
const PAD_TOP = 10;
const BASELINE = VIEW_H - 18;

const nf = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 1 });

// Left padding sized to the widest tick label so a five-digit kg figure never
// runs off the viewBox edge (the "2,327.4" top-left clip). ~5.6px per character
// at fontSize 9, plus the 5px gap the tick text keeps from the plot edge.
const padForTicks = (max: number) => Math.max(PAD_X, Math.ceil(nf(max).length * 5.6) + 10);

// Interior x-axis ticks: up to four evenly spaced slots between the two
// endpoint labels, thinned by step so a 7-day and a 92-day window both render
// legibly. A candidate landing within half a step of the last slot is dropped
// so it never crowds the endpoint label.
const interiorTickIdx = (n: number) => {
  const step = Math.ceil(Math.max(1, n - 1) / 5);
  const out: number[] = [];
  for (let i = step; i < n - 1; i += step) {
    if (n - 1 - i >= Math.max(1, step / 2)) out.push(i);
  }
  return out;
};

// Axis dates render dd-mm-yy (maintainer request 2026-08-21); a label that is
// not a plain YYYY-MM-DD date renders unchanged.
const fmtDay = (label: string) => {
  const m = /^\d{2}(\d{2})-(\d{2})-(\d{2})$/.exec(label);
  return m ? `${m[3]}-${m[2]}-${m[1]}` : label;
};

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
  const padX = padForTicks(max);
  const slot = (VIEW_W - padX - PAD_X) / days.length;
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
          x1={padX}
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
          x={padX - 5}
          y={BASELINE - (BASELINE - PAD_TOP) * f + 3}
          fontSize="9"
          textAnchor="end"
          fill="var(--faint)"
        >
          {nf(max * f)}
        </text>
      ))}
      <line x1={padX} x2={VIEW_W - 6} y1={BASELINE} y2={BASELINE} stroke="var(--line)" strokeWidth="1" />
      {days.map((d, i) => {
        const x = padX + i * slot + (slot - barW) / 2;
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
                  fill={seriesColorVar(s)}
                />
              );
            })}
            <rect
              x={padX + i * slot}
              y={PAD_TOP}
              width={slot}
              height={BASELINE - PAD_TOP}
              fill="transparent"
              data-tip={tipText}
            />
          </g>
        );
      })}
      <text x={padX} y={VIEW_H - 4} fontSize="8" fill="var(--faint)">
        {fmtDay(days[0].label)}
      </text>
      {interiorTickIdx(days.length).map((i) => (
        <text
          key={days[i].key}
          x={padX + i * slot + slot / 2}
          y={VIEW_H - 4}
          fontSize="7"
          textAnchor="middle"
          fill="var(--faint)"
        >
          {fmtDay(days[i].label)}
        </text>
      ))}
      <text x={VIEW_W - 6} y={VIEW_H - 4} fontSize="8" textAnchor="end" fill="var(--faint)">
        {fmtDay(days[days.length - 1].label)}
      </text>
    </svg>
  );
}

export type LineSeries = {
  label: string;
  colorVar: string;
  /** One point per day slot; null = no figure that day (line breaks, honestly). */
  points: (number | null)[];
};

/**
 * Multi-series line chart on one shared scale, endpoint dot per series.
 *
 * `secondary` draws ONE more series in a different unit on its OWN scale — a dashed line with
 * its ticks on the right edge — so money and quantity can share a card without either being
 * squashed flat by the other's magnitude. Its points join the same hover tip in their own noun.
 */
export function SeriesLines({
  series,
  dayLabels,
  valueNoun,
  chartLabel,
  emptyLabel,
  secondary,
}: {
  series: LineSeries[];
  dayLabels: string[];
  valueNoun: string;
  chartLabel: string;
  emptyLabel: string;
  secondary?: { series: LineSeries; valueNoun: string };
}) {
  const values = series.flatMap((s) => s.points.filter((p): p is number => p !== null));
  const secondaryValues = secondary?.series.points.filter((p): p is number => p !== null) ?? [];
  if ((values.length === 0 && secondaryValues.length === 0) || dayLabels.length === 0) {
    return (
      <div className="muted small" style={{ padding: "12px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }
  const max = Math.max(1, ...values);
  const padX = padForTicks(max);
  const secondaryMax = Math.max(1, ...secondaryValues);
  // The right edge holds the secondary ticks when there are any; otherwise the plot keeps its
  // usual 6px margin so charts without a secondary series draw exactly as before.
  const rightX = secondary ? VIEW_W - padForTicks(secondaryMax) : VIEW_W - 6;
  const stepX = (rightX - padX) / Math.max(1, dayLabels.length - 1);
  const yOf = (v: number) => BASELINE - ((BASELINE - PAD_TOP) * v) / max;
  const yOfSecondary = (v: number) => BASELINE - ((BASELINE - PAD_TOP) * v) / secondaryMax;
  const pathFor = (points: (number | null)[], y: (v: number) => number): string[] => {
    const segments: string[] = [];
    let current: string[] = [];
    points.forEach((p, i) => {
      if (p === null) {
        if (current.length > 0) segments.push(current.join(" "));
        current = [];
        return;
      }
      current.push(`${current.length === 0 ? "M" : "L"}${(padX + i * stepX).toFixed(1)} ${y(p).toFixed(1)}`);
    });
    if (current.length > 0) segments.push(current.join(" "));
    return segments;
  };
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
          x1={padX}
          x2={rightX}
          y1={BASELINE - (BASELINE - PAD_TOP) * f}
          y2={BASELINE - (BASELINE - PAD_TOP) * f}
          stroke="var(--line)"
          strokeWidth="1"
        />
      ))}
      {[0.5, 1].map((f) => (
        <text
          key={f}
          x={padX - 5}
          y={BASELINE - (BASELINE - PAD_TOP) * f + 3}
          fontSize="9"
          textAnchor="end"
          fill="var(--faint)"
        >
          {nf(max * f)}
        </text>
      ))}
      {secondary
        ? [0.5, 1].map((f) => (
            <text
              key={`s${f}`}
              x={rightX + 5}
              y={BASELINE - (BASELINE - PAD_TOP) * f + 3}
              fontSize="9"
              textAnchor="start"
              fill="var(--faint)"
            >
              {nf(secondaryMax * f)}
            </text>
          ))
        : null}
      <line x1={padX} x2={rightX} y1={BASELINE} y2={BASELINE} stroke="var(--line)" strokeWidth="1" />
      {secondary
        ? pathFor(secondary.series.points, yOfSecondary).map((d, i) => (
            <path
              key={`s${i}`}
              d={d}
              fill="none"
              stroke={secondary.series.colorVar}
              strokeWidth="1.5"
              strokeDasharray="4 3"
              strokeLinejoin="round"
              opacity="0.75"
            />
          ))
        : null}
      {series.map((s) => {
        const segments = pathFor(s.points, yOf);
        const lastIdx: number = s.points.reduce<number>((acc, p, i) => (p === null ? acc : i), -1);
        const lastVal = lastIdx >= 0 ? s.points[lastIdx] : null;
        return (
          <g key={s.label}>
            {segments.map((d, i) => (
              <path key={i} d={d} fill="none" stroke={s.colorVar} strokeWidth="2" strokeLinejoin="round" />
            ))}
            {lastVal !== null && lastIdx >= 0 ? (
              <circle
                cx={padX + lastIdx * stepX}
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
        const secondaryPoint = secondary?.series.points[i];
        if (secondary && secondaryPoint !== null && secondaryPoint !== undefined) {
          parts.push(`${secondary.series.label}  ${nf(secondaryPoint)} ${secondary.valueNoun}`);
        }
        if (parts.length === 0) return null;
        return (
          <rect
            key={day}
            x={padX + i * stepX - stepX / 2}
            y={PAD_TOP}
            width={stepX}
            height={BASELINE - PAD_TOP}
            fill="transparent"
            data-tip={[day, ...parts].join("\n")}
          />
        );
      })}
      <text x={padX} y={VIEW_H - 4} fontSize="8" fill="var(--faint)">
        {fmtDay(dayLabels[0])}
      </text>
      {interiorTickIdx(dayLabels.length).map((i) => (
        <text
          key={dayLabels[i]}
          x={padX + i * stepX}
          y={VIEW_H - 4}
          fontSize="7"
          textAnchor="middle"
          fill="var(--faint)"
        >
          {fmtDay(dayLabels[i])}
        </text>
      ))}
      <text x={rightX} y={VIEW_H - 4} fontSize="8" textAnchor="end" fill="var(--faint)">
        {fmtDay(dayLabels[dayLabels.length - 1])}
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

/** One pie slice: a label, its share value, and the colour token it carries on the other charts. */
export type PieSlice = { label: string; value: number; colorVar: string };

/**
 * A single-ring pie of shares. Each slice carries a `data-tip` for the
 * ChartHover wrapper (label, value, share). Slices are drawn in the order
 * given so the caller decides the ranking; a zero or negative value draws
 * nothing. Inline SVG per the mock's chart anatomy, no library.
 */
export function SeriesPie({
  slices,
  valueNoun,
  chartLabel,
  emptyLabel,
  formatValue,
}: {
  slices: PieSlice[];
  valueNoun: string;
  chartLabel: string;
  emptyLabel: string;
  formatValue?: (value: number) => string;
}) {
  const live = slices.filter((s) => Number.isFinite(s.value) && s.value > 0);
  const total = live.reduce((acc, s) => acc + s.value, 0);
  if (live.length === 0 || total <= 0) {
    return (
      <div className="muted small" style={{ padding: "12px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }
  const fmt = formatValue ?? ((v: number) => v.toLocaleString("en-IN", { maximumFractionDigits: 1 }));
  const size = 300;
  const cx = size / 2;
  const cy = size / 2;
  const r = size / 2 - 6;
  const point = (angle: number) => [cx + r * Math.cos(angle), cy + r * Math.sin(angle)] as const;
  let start = -Math.PI / 2;
  const paths = live.map((s) => {
    const sweep = (s.value / total) * Math.PI * 2;
    const end = start + sweep;
    const [x0, y0] = point(start);
    const [x1, y1] = point(end);
    const large = sweep > Math.PI ? 1 : 0;
    // A single slice is a full ring: an arc from a point back to itself draws nothing.
    const d =
      live.length === 1
        ? `M ${cx - r} ${cy} A ${r} ${r} 0 1 1 ${cx + r} ${cy} A ${r} ${r} 0 1 1 ${cx - r} ${cy} Z`
        : `M ${cx} ${cy} L ${x0} ${y0} A ${r} ${r} 0 ${large} 1 ${x1} ${y1} Z`;
    const share = ((s.value / total) * 100).toFixed(1);
    start = end;
    return { key: s.label, d, colorVar: s.colorVar, tip: `${s.label}\n${fmt(s.value)} ${valueNoun}\n${share}%` };
  });
  return (
    <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: "12px 40px", width: "100%" }}>
      <svg
        viewBox={`0 0 ${size} ${size}`}
        width={size}
        height={size}
        role="img"
        aria-label={chartLabel}
        style={{ flex: "0 0 auto", maxWidth: "100%" }}
      >
        {paths.map((p) => (
          <path key={p.key} d={p.d} fill={p.colorVar} stroke="var(--panel)" strokeWidth={1.5} data-tip={p.tip} />
        ))}
      </svg>
      <div
        style={{
          flex: "1 1 320px",
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(390px, 1fr))",
          gap: "10px 32px",
          alignContent: "center",
        }}
      >
        {live.map((s) => (
          <div key={s.label} style={{ display: "flex", alignItems: "center", gap: 10 }} data-tip={`${s.label}\n${fmt(s.value)} ${valueNoun}`}>
            <span aria-hidden style={{ width: 11, height: 11, borderRadius: 3, background: s.colorVar, display: "inline-block", flex: "0 0 auto" }} />
            <span style={{ flex: 1, fontSize: 15, whiteSpace: "nowrap" }}>{s.label}</span>
            <span className="muted" style={{ fontSize: 14, whiteSpace: "nowrap" }}>{`${fmt(s.value)} ${valueNoun} · ${((s.value / total) * 100).toFixed(1)}%`}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
