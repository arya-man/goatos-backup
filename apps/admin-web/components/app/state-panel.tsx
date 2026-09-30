"use client";

import type { ReactNode } from "react";

import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";

import { varAlpha } from "minimal-shared/utils";

import { Iconify } from "@/components/minimal/iconify";

/**
 * Full-screen (or in-place) error / unavailable state: the template 500-view anatomy (centred h4
 * title, body2 text, actions) on a template Card, with a warning icon tile. Replaces the retired
 * `.kit-state*` classes (frame.css / minimal-theme.css). Client leaf (themed tint); servers pass nodes.
 */
export function StatePanel({
  title,
  body,
  actions,
  reference,
  page = true,
  titleComponent = "h1",
}: {
  title: ReactNode;
  body?: ReactNode;
  actions?: ReactNode;
  reference?: ReactNode;
  /** true: fills the viewport (shell unavailable); false: sits in the page column (route error). */
  page?: boolean;
  titleComponent?: "h1" | "h2";
}) {
  return (
    <Box
      component="main"
      sx={{
        display: "grid",
        placeItems: "center",
        px: 2,
        py: page ? 3 : 6,
        minHeight: page ? "100dvh" : 0,
        bgcolor: page ? "background.default" : "transparent",
      }}
    >
      <Card
        role="alert"
        sx={{ width: 1, maxWidth: page ? 480 : 560, px: { xs: 3, sm: 4 }, py: 5, textAlign: "center" }}
      >
        <Stack spacing={1.5} sx={{ alignItems: "center" }}>
          <Box
            aria-hidden
            sx={(theme) => ({
              p: 1.5,
              display: "inline-flex",
              borderRadius: "var(--r-lg)",
              color: "warning.main",
              bgcolor: varAlpha(theme.vars.palette.warning.mainChannel, 0.16),
            })}
          >
            <Iconify icon="solar:danger-triangle-bold" width={24} />
          </Box>
          <Typography component={titleComponent} variant="h5" sx={{ pt: 1 }}>
            {title}
          </Typography>
          {body ? (
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {body}
            </Typography>
          ) : null}
          {actions ? (
            <Stack direction="row" spacing={1} sx={{ pt: 1, flexWrap: "wrap", justifyContent: "center" }}>
              {actions}
            </Stack>
          ) : null}
          {reference ? (
            <Typography variant="caption" sx={{ color: "text.disabled", fontVariantNumeric: "tabular-nums" }}>
              {reference}
            </Typography>
          ) : null}
        </Stack>
      </Card>
    </Box>
  );
}
