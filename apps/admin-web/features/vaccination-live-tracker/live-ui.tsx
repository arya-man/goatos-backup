import type { ReactNode } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import LinearProgress from "@mui/material/LinearProgress";
import TableCell from "@mui/material/TableCell";
import TableRow from "@mui/material/TableRow";
import Typography from "@mui/material/Typography";
import Link from "@/components/no-prefetch-link";
import type { LivePaletteColor } from "./format";

// Small template-anatomy pieces shared by the live tracker cards. Server-safe: object sx only, no
// element props that MUI clones (guard: server-element-prop, server-function-prop).

/** Legend / feed dot in a palette colour. */
export function LiveDot({ color, size = 8 }: { color: LivePaletteColor; size?: number }) {
  return (
    <Box
      component="span"
      aria-hidden="true"
      sx={{ width: size, height: size, flexShrink: 0, borderRadius: "50%", bgcolor: color === "default" ? "text.disabled" : `${color}.main` }}
    />
  );
}

/** Template progress cell: thin LinearProgress with the figure beside it. */
export function LiveProgress({ value, color, label }: { value: number; color: "primary" | "warning" | "error"; label: string }) {
  return (
    <Box sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
      <LinearProgress variant="determinate" value={value} color={color} sx={{ flex: 1, height: "calc(1 * var(--spacing))" }} aria-label={label} />
      <Typography variant="caption" sx={{ minWidth: 36, textAlign: "right", color: "text.secondary" }}>
        {label}
      </Typography>
    </Box>
  );
}

/** Table head row (TableHeadCustom look): numeric columns right-aligned. */
export function LiveHeadRow({ labels, numeric = [] }: { labels: string[]; numeric?: number[] }) {
  return (
    <TableRow>
      {labels.map((label, index) => (
        <TableCell key={label} component="th" align={numeric.includes(index) ? "right" : "left"} sx={{ whiteSpace: "nowrap" }}>
          {label}
        </TableCell>
      ))}
    </TableRow>
  );
}

/** Card-body empty state: subtitle + body, optional reset button. */
export function LiveEmpty({ title, body, resetHref, resetLabel }: { title: string; body?: string; resetHref?: string | null; resetLabel?: string }) {
  return (
    <Box sx={{ px: 3, pb: 3, display: "flex", alignItems: "center", gap: 2, flexWrap: "wrap" }}>
      <Box sx={{ flex: "1 1 200px", minWidth: 0 }}>
        <Typography variant="subtitle2">{title}</Typography>
        {body ? (
          <Typography variant="body2" sx={{ mt: 0.5, color: "text.secondary" }}>
            {body}
          </Typography>
        ) : null}
      </Box>
      {resetHref && resetLabel ? (
        <Button component={Link} href={resetHref} replace scroll={false} size="small" variant="outlined" color="inherit">
          {resetLabel}
        </Button>
      ) : null}
    </Box>
  );
}

/** Visible caption under a table / list (truncation, residuals): never a tooltip. */
export function LiveNote({ children }: { children: ReactNode }) {
  return (
    <Typography variant="caption" component="div" role="status" data-truncnote="" sx={{ px: 3, py: 1.5, color: "text.secondary", borderTop: 1, borderColor: "divider", borderTopStyle: "dashed" }}>
      {children}
    </Typography>
  );
}
