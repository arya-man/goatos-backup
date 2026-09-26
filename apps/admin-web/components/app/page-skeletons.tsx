import type { CSSProperties, ReactNode } from "react";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import MuiSkeleton from "@mui/material/Skeleton";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import { cx } from "@/lib/tone";

/**
 * Shimmer placeholders.
 *
 * `Skeleton` is the atom; the shapes below are the standard page-level placeholders so a
 * `loading.tsx` matches the real layout instead of flashing a single grey bar. Give the shape
 * the SAME column widths / row counts / avatar sizes the real content will have — a loader that
 * re-flows on hydration is worse than no loader.
 *
 * Server-safe; no CSS import. The `kit-sk-*` layout classes live in `app/minimal-theme.css`
 * (grid, gap, padding, radius, shadow), so this file never imports its own stylesheet.
 */
export function Skeleton({ width, height = 16, radius = 8, className, style }: { width?: number | string; height?: number | string; radius?: number | string; className?: string; style?: CSSProperties }) {
  // The template's MUI Skeleton (wave shimmer, theme-tinted); `kit-skeleton` stays as the hook the
  // visual lanes wait on.
  return <MuiSkeleton aria-hidden="true" variant="rounded" animation="wave" className={cx("kit-skeleton", className)} width={width} height={height} style={{ borderRadius: radius, flexShrink: 0, minWidth: 0, maxWidth: "100%", ...style }} />;
}

/** Stacked text lines; the last one is short so it reads as a paragraph, not a block. */
export function SkeletonText({ lines = 3, width = "100%", className }: { lines?: number; width?: number | string; className?: string }) {
  return (
    <div aria-hidden="true" className={cx("kit-sk-group", className)} style={{ width }}>
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton key={i} height={12} width={i === lines - 1 ? "60%" : "100%"} />
      ))}
    </div>
  );
}

/** A card-shaped placeholder: title, optional lines, and whatever else you pass as children. */
export function SkeletonCard({ lines = 3, height, className, children }: { lines?: number; height?: number | string; className?: string; children?: ReactNode }) {
  return (
    <Card aria-hidden="true" className={cx("kit-sk-card", className)} sx={{ p: 3, display: "flex", flexDirection: "column", gap: 2, ...(height ? { height } : {}) }}>
      <Skeleton width="42%" height={18} />
      {children ?? <SkeletonText lines={lines} />}
    </Card>
  );
}

/**
 * A responsive row of KPI tiles — the deck most pages open with.
 * `spark` adds the mini bar-spark block so the tile does not grow on hydration.
 */
export function SkeletonKpiRow({ count = 4, spark = false, className }: { count?: number; spark?: boolean; className?: string }) {
  return (
    <div aria-hidden="true" className={cx("kit-sk-kpis", className)}>
      {Array.from({ length: count }, (_, i) => (
        <Card key={i} className="kit-sk-card kit-sk-kpi" sx={{ p: 3, display: "flex", flexDirection: "column", gap: 1.5 }}>
          <Skeleton width="55%" height={14} />
          <Skeleton width="40%" height={32} radius={10} />
          <Skeleton width="30%" height={12} />
          {spark ? <Skeleton width="100%" height={32} radius={6} /> : null}
        </Card>
      ))}
    </div>
  );
}

/**
 * Chart block: an axis gutter plus varied bar heights, optionally a legend strip, inside a card.
 * Pass `card={false}` to drop it into a card you already render.
 */
export function SkeletonChart({ bars = 12, height = 200, card = true, title = true, withLegend = false, className }: { bars?: number; height?: number; card?: boolean; title?: boolean; withLegend?: boolean; className?: string }) {
  const heights = Array.from({ length: bars }, (_, i) => 34 + ((i * 37) % 62));
  const body = (
    <>
      {withLegend ? (
        <div className="kit-sk-legend">
          {Array.from({ length: 3 }, (_, i) => (
            <span key={i} className="kit-sk-legend-item">
              <Skeleton width={10} height={10} radius="50%" />
              <Skeleton width={52 + i * 10} height={10} />
            </span>
          ))}
        </div>
      ) : null}
      <div className="kit-sk-plot">
        <div className="kit-sk-axis">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} width={26} height={9} />
          ))}
        </div>
        <div className="kit-sk-chart" style={{ ["--h" as string]: `${height}px` }}>
          {heights.map((h, i) => (
            <Skeleton key={i} height={`${h}%`} />
          ))}
        </div>
      </div>
      <div className="kit-sk-xaxis">
        {Array.from({ length: Math.min(bars, 6) }, (_, i) => (
          <Skeleton key={i} width={30} height={9} />
        ))}
      </div>
    </>
  );
  if (!card) return <div aria-hidden="true" className={cx("kit-sk-group", className)}>{body}</div>;
  return (
    <Card aria-hidden="true" className={cx("kit-sk-card", className)} sx={{ p: 3, display: "flex", flexDirection: "column", gap: 2 }}>
      {title ? <Skeleton width="34%" height={18} /> : null}
      {body}
    </Card>
  );
}

/**
 * Table placeholder with the REAL column rhythm.
 * `widths` are CSS grid tracks, one per column (e.g. `["40px","2fr","1fr","90px"]`); when omitted
 * the first column is wide and the rest share the remainder. `cols` is only used without `widths`.
 */
export function SkeletonTable({ rows = 8, cols = 4, widths, header = true, className }: { rows?: number; cols?: number; widths?: string[]; header?: boolean; className?: string }) {
  const tracks = widths ?? ["2fr", ...Array.from({ length: Math.max(cols - 1, 0) }, () => "1fr")];
  const bare = tracks.map((t) => t.replace(/^minmax\(\s*[^,]+,\s*/, "").replace(/\)$/, "").trim());
  const weight = (t: string) => (t.endsWith("fr") ? Number.parseFloat(t) || 1 : (Number.parseFloat(t) || 100) / 100);
  const total = bare.reduce((sum, t) => sum + weight(t), 0) || 1;
  const colWidth = (t: string) => `${(weight(t) / total) * 100}%`;
  const n = tracks.length;
  return (
    <Box aria-hidden="true" className={cx("kit-sk-rows", className)} sx={{ overflow: "hidden", minWidth: 0 }}>
      <Table sx={{ tableLayout: "fixed", width: 1, minWidth: "0 !important", "& .MuiTableCell-root": { px: { xs: 1, sm: 2 } } }}>
        <colgroup>
          {bare.map((t, c) => (
            <col key={c} style={{ width: colWidth(t) }} />
          ))}
        </colgroup>
        {header ? (
          <TableHead>
            <TableRow>
              {bare.map((_, c) => (
                <TableCell key={c}>
                  <Skeleton height={10} width={c === 0 ? "38%" : "54%"} />
                </TableCell>
              ))}
            </TableRow>
          </TableHead>
        ) : null}
        <TableBody>
          {Array.from({ length: rows }, (_, r) => (
            <TableRow key={r}>
              {Array.from({ length: n }, (_, c) => (
                <TableCell key={c}>
                  <Skeleton height={12} width={c === 0 ? "70%" : `${48 + ((r * 13 + c * 29) % 34)}%`} />
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Box>
  );
}

/** List placeholder: avatar circle + two text lines + a trailing value. */
export function SkeletonList({ rows = 5, avatar = true, card = true, className }: { rows?: number; avatar?: boolean; card?: boolean; className?: string }) {
  const body = Array.from({ length: rows }, (_, i) => (
    <div key={i} className="kit-sk-listrow">
      {avatar ? <Skeleton width={44} height={44} radius="50%" /> : null}
      <span className="kit-sk-group" style={{ flex: 1, minWidth: 0 }}>
        <Skeleton height={13} width={`${48 + ((i * 17) % 28)}%`} />
        <Skeleton height={10} width={`${30 + ((i * 23) % 26)}%`} />
      </span>
      <Skeleton width={56} height={12} />
    </div>
  ));
  if (!card) return <div aria-hidden="true" className={cx("kit-sk-list", className)}>{body}</div>;
  return <div aria-hidden="true" className={cx("kit-sk-card kit-sk-list", className)}>{body}</div>;
}

/** Drawer/sheet placeholder: header strip, meta grid, then paragraph lines. */
export function SkeletonDrawer({ metaRows = 3, lines = 4, className }: { metaRows?: number; lines?: number; className?: string }) {
  return (
    <div aria-hidden="true" className={cx("kit-sk-drawer", className)}>
      <div className="kit-sk-drawer-hd">
        <Skeleton width={44} height={44} radius={12} />
        <span className="kit-sk-group" style={{ flex: 1, minWidth: 0 }}>
          <Skeleton height={11} width="28%" />
          <Skeleton height={18} width="56%" />
        </span>
      </div>
      <div className="kit-sk-meta">
        {Array.from({ length: metaRows * 2 }, (_, i) => (
          <span key={i} className="kit-sk-group">
            <Skeleton height={10} width="56%" />
            <Skeleton height={14} width="78%" />
          </span>
        ))}
      </div>
      <SkeletonText lines={lines} />
    </div>
  );
}

/** Class names for hand-built loading layouts (route `loading.tsx` files). */
export const skeletonClasses = {
  card: "kit-sk-card",
  group: "kit-sk-group",
  rows: "kit-sk-rows",
  row: "kit-sk-row",
  kpis: "kit-sk-kpis",
  thead: "kit-sk-thead",
} as const;
