"use client";

import type { ReactNode } from "react";

import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";

import { MinimalDrawer, type MinimalDrawerWidth } from "@/components/app/drawer";
import { Scrollbar } from "@/components/minimal/scrollbar";

/**
 * Record/detail drawer on the template MinimalDrawer (portalled MUI Drawer, theme backdrop, focus
 * trapped and returned). The eyebrow + optional icon tile sit under the template header, the body
 * is the template 20px-padded column and the footer is the template action row.
 *
 * With useLocalOverlaySelection, pass its closeDrawer as onClose: MUI's Escape/backdrop and the
 * hook's own Escape listener land on the same idempotent close, so history steps back once.
 */
export function DetailDrawer({
  open,
  onClose,
  title,
  subtitle,
  eyebrow,
  icon,
  iconColors,
  closeLabel,
  ariaLabel,
  footer,
  size = "md",
  bodySx,
  paperTestId,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  /** One muted line under the template header (a panel hint). */
  subtitle?: ReactNode;
  eyebrow?: ReactNode;
  icon?: ReactNode;
  /** Tile fill/ink from the Mesha palette tokens (e.g. `var(--dangerx)` / `var(--danger)`). */
  iconColors?: { bg: string; fg: string };
  closeLabel: string;
  ariaLabel?: string;
  footer?: ReactNode;
  /** Paper width from sm up (phones are always full width): md record/panel (template kanban-details 480), sm compact (360). */
  size?: keyof typeof DRAWER_SIZES;
  bodySx?: object;
  paperTestId?: string;
  children: ReactNode;
}) {
  return (
    <MinimalDrawer
      open={open}
      onClose={onClose}
      title={title}
      closeLabel={closeLabel}
      width={DRAWER_SIZES[size]}
      aria-label={ariaLabel}
      slotProps={{ paper: { "aria-label": ariaLabel, role: "dialog", ...(paperTestId ? { "data-testid": paperTestId } : {}) } as object }}
      footer={
        footer ? (
          <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1.5, width: 1, justifyContent: "flex-end" }}>{footer}</Box>
        ) : undefined
      }
    >
      {eyebrow || icon ? (
        <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, px: 2.5, pt: 2.5 }}>
          {icon ? (
            <Box
              component="span"
              aria-hidden="true"
              sx={{ width: "var(--sp-5)", height: "var(--sp-5)", borderRadius: "var(--r2)", display: "inline-grid", placeItems: "center", flex: "none", "& svg": { width: "var(--sp-2h)", height: "var(--sp-2h)" } }}
              style={{ background: iconColors?.bg ?? "var(--brand-soft)", color: iconColors?.fg ?? "var(--brand-d)" }}
            >
              {icon}
            </Box>
          ) : null}
          {eyebrow ? (
            <Typography variant="overline" component="div" sx={{ color: "text.secondary", minWidth: 0 }}>
              {eyebrow}
            </Typography>
          ) : null}
        </Box>
      ) : null}
      {subtitle ? (
        <Typography variant="body2" sx={{ color: "text.secondary", px: 2.5, pt: 2 }}>
          {subtitle}
        </Typography>
      ) : null}
      {/* Template drawer forms run their fields full width (kanban-details, user-quick-edit): a select
          sized to its value ("—") read as a stray chip at the drawer's left edge. */}
      <Box sx={{ p: 2.5, display: "flex", flexDirection: "column", gap: 2.5, minWidth: 0, "& .MuiFormControl-root": { width: 1 }, ...bodySx }}>{children}</Box>
    </MinimalDrawer>
  );
}

// Template drawer paper widths (MinimalDrawer caps them at 100vw below sm). Nothing wider exists in
// the template: wide tables scroll inside their own Scrollbar (see DrawerTableScroll).
const DRAWER_SIZES = { sm: 360, md: 480 } as const satisfies Record<string, MinimalDrawerWidth>;

/** Two-column label/value grid (template details-info rows). */
export function DrawerMetaGrid({ children, columns = 2 }: { children: ReactNode; columns?: 1 | 2 }) {
  return (
    <Box
      sx={{
        display: "grid",
        gridTemplateColumns: columns === 1 ? "minmax(0,1fr)" : "repeat(2, minmax(0,1fr))",
        gap: 2,
      }}
    >
      {children}
    </Box>
  );
}

export function DrawerMetaItem({ label, children, span }: { label: ReactNode; children: ReactNode; span?: boolean }) {
  return (
    <Box sx={{ minWidth: 0, ...(span ? { gridColumn: "1 / -1" } : {}) }}>
      <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>
        {label}
      </Typography>
      <Typography variant="body2" component="div" sx={{ mt: 0.5, fontWeight: 500, overflowWrap: "anywhere" }}>
        {children}
      </Typography>
    </Box>
  );
}

/** Muted guidance block (template neutral surface). */
export function DrawerNote({ children }: { children: ReactNode }) {
  return (
    <Typography
      variant="body2"
      component="div"
      sx={{ color: "text.secondary", bgcolor: "background.neutral", borderRadius: "var(--r-md)", px: 1.5, py: 1.25 }}
    >
      {children}
    </Typography>
  );
}

/** Heading + content block inside a drawer body (template section title). */
export function DrawerBlock({ title, action, children }: { title: ReactNode; action?: ReactNode; children: ReactNode }) {
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5, minWidth: 0 }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
        <Typography variant="subtitle2" sx={{ flex: 1, minWidth: 0 }}>
          {title}
        </Typography>
        {action}
      </Box>
      {children}
    </Box>
  );
}

/**
 * A table (or any wide block) inside a drawer: it scrolls sideways in its own template Scrollbar
 * (sections/order/order-details-items pattern: `<Scrollbar><Table sx={{ minWidth }} />`) instead
 * of squeezing or clipping the columns at the drawer edge.
 */
export function DrawerTableScroll({ children }: { children: ReactNode }) {
  return <Scrollbar sx={{ minWidth: 0, maxWidth: 1 }}>{children}</Scrollbar>;
}
