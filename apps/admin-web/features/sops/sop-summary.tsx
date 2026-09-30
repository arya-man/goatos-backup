// The read-only SOP summaries in the SOP drawer (weighing, inspection, pc-care, follow-up, capture,
// feed, shifting, toxin) share one template anatomy: a subtitle2 heading with its muted subtitle,
// then divider rows (title + caption meta + note), the order-history list rhythm of the template.
// Every summary composes these parts; none carries a class name or a stylesheet of its own.
import type { ReactNode } from "react";

import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";

/** One summary (the drawer may stack several). */
export function SummaryRoot({ children }: { children: ReactNode }) {
  return <Box sx={{ minWidth: 0 }}>{children}</Box>;
}

/** The summary's heading line: the title, then the muted subtitle beside it. */
export function SummaryHeading({ children }: { children: ReactNode }) {
  return (
    <Typography variant="subtitle2" component="div" sx={{ mt: 1.75, mb: 1 }}>
      {children}
    </Typography>
  );
}

/** The muted subtitle next to a heading ("— what the crew fills"). */
export function SummarySub({ children }: { children: ReactNode }) {
  return (
    <Typography component="span" variant="caption" sx={{ color: "text.secondary", fontWeight: "fontWeightRegular" }}>
      {children}
    </Typography>
  );
}

/** A group label inside a summary ("Questions", a track name). */
export function SummaryGroup({ children, dense = false }: { children: ReactNode; dense?: boolean }) {
  return (
    <Typography variant="caption" component="div" sx={{ color: "text.secondary", fontWeight: "fontWeightBold", mt: dense ? 0.75 : 1, mb: dense ? 0.75 : 0.5 }}>
      {children}
    </Typography>
  );
}

/** The list of rows. */
export function SummaryList({ children }: { children: ReactNode }) {
  return <Box sx={{ position: "relative", pl: 0.75, mb: 0.75 }}>{children}</Box>;
}

/** One row: a divider line under it, an optional leading glyph, its text column filling the width. */
export function SummaryRow({ children, lead }: { children: ReactNode; lead?: ReactNode }) {
  return (
    <Box sx={{ display: "flex", alignItems: "flex-start", gap: 1.5, py: 1.125, px: 0.25, borderBottom: 1, borderColor: "divider" }}>
      {lead}
      <Box sx={{ flex: 1, minWidth: 0 }}>{children}</Box>
    </Box>
  );
}

/** A row's title. */
export function SummaryTitle({ children }: { children: ReactNode }) {
  return (
    <Typography component="b" variant="body2" sx={{ fontWeight: "fontWeightSemiBold" }}>
      {children}
    </Typography>
  );
}

/** A row's meta line (what is captured, compulsory or not). */
export function SummaryMeta({ children }: { children: ReactNode }) {
  return (
    <Typography variant="caption" component="div" sx={{ color: "text.secondary", mt: 0.25 }}>
      {children}
    </Typography>
  );
}

/** A muted note under a row (the step's instruction). */
export function SummaryNote({ children }: { children: ReactNode }) {
  return (
    <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>
      {children}
    </Typography>
  );
}

/** The whole summary is empty. */
export function SummaryEmpty({ children }: { children: ReactNode }) {
  return (
    <Typography variant="body2" component="div" sx={{ color: "text.secondary", bgcolor: "background.neutral", border: 1, borderColor: "divider", borderRadius: "var(--r-md)", px: 1.5, py: 1.125 }}>
      {children}
    </Typography>
  );
}
