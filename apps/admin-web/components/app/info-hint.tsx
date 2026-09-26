"use client";

import Box from "@mui/material/Box";

import { Iconify } from "@/layouts/template/iconify";

/**
 * A caveat that stays out of the page: one info glyph whose full sentence is the native tooltip
 * and the accessible name. Replaces the explanatory paragraphs under charts, footers and drawer
 * fields (the frame keeps captions to one short line; the reason lives here). A client leaf so its theme-callback sx never crosses the server->client boundary.
 *
 * Renders MUI Box + template Iconify only; sizing/hover come from MUI theme tokens.
 */
export function InfoHint({ text, className, size = 24 }: { text: string; className?: string; size?: number }) {
  const glyph = Math.round(size * 0.667);
  return (
    <Box
      component="span"
      className={className}
      role="img"
      title={text}
      aria-label={text}
      tabIndex={0}
      sx={{
        display: "inline-grid",
        placeItems: "center",
        // Phone: a 44px tap box (webview rule) that keeps the glyph's layout footprint through a
        // negative margin, so rows and headers do not grow. Desktop: the glyph size.
        width: { xs: Math.max(size, 44), sm: size },
        height: { xs: Math.max(size, 44), sm: size },
        m: { xs: size < 44 ? `${(size - 44) / 2}px` : 0, sm: 0 },
        borderRadius: "50%",
        color: "text.secondary",
        cursor: "help",
        verticalAlign: "middle",
        transition: (theme) => theme.transitions.create(["background-color", "color"]),
        "&:hover, &:focus-visible": {
          bgcolor: (theme) => theme.palette.action.hover,
        },
      }}
    >
      <Iconify icon="solar:info-circle-bold" width={glyph} aria-hidden="true" />
    </Box>
  );
}
