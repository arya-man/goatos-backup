"use client";

// A global error replaces the ROOT layout, so this file must load the stylesheets and the theme
// provider itself — nothing from app/layout.tsx renders around it.
import "./minimal-tokens.css";
import "@/theme/fonts.css";

import { Iconify } from "@/components/minimal/iconify";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Container from "@mui/material/Container";
import Typography from "@mui/material/Typography";

import { Logo } from "@/layouts/template/logo";
import { AppThemeProvider } from "@/theme/app-theme-provider";
import { THEME_BOOT_SCRIPT } from "@/lib/theme";

// Template pattern: Minimal_TypeScript_v7.7.0 next-ts src/sections/error/500-view.tsx inside the
// SimpleLayout compact content (centred column, logo on top). The illustration is left out. The logo
// is the Mesha मे tile the shell and auth pages show (Logo default = top_bar.logo_text).
export default function GlobalError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    // No forced theme: the same boot script as app/layout.tsx applies the user's stored light/dark
    // choice before paint, so a light-theme user does not get a dark error page.
    <html lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: THEME_BOOT_SCRIPT }} />
      </head>
      <body suppressHydrationWarning>
        <AppThemeProvider>
          <Box
            component="main"
            sx={(theme) => ({
              minHeight: "100dvh",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              p: theme.spacing(3, 2, 10, 2),
              bgcolor: "background.default",
              color: "text.primary",
            })}
          >
            <Container maxWidth="xs" sx={{ textAlign: "center" }}>
              <Box sx={{ mb: 5, display: "flex", justifyContent: "center" }}>
                <Logo />
              </Box>
              <Typography variant="h3" sx={{ mb: 2 }}>
                Something went wrong
              </Typography>
              <Typography sx={{ color: "text.secondary" }}>
                The dashboard could not finish rendering this screen. Retrying reloads it from the server; if it keeps
                failing, quote the reference below.
              </Typography>
              {error.digest ? (
                <Box
                  component="code"
                  sx={{
                    mt: 3,
                    px: 1.25,
                    py: 0.75,
                    display: "inline-block",
                    borderRadius: 0.75,
                    typography: "caption",
                    fontFamily: "monospace",
                    bgcolor: "action.hover",
                  }}
                >
                  {error.digest}
                </Box>
              ) : null}
              <Box sx={{ mt: 5 }}>
                <Button size="large" variant="contained" color="primary" onClick={reset} startIcon={<Iconify icon="solar:restart-bold" />}>
                  Try again
                </Button>
              </Box>
            </Container>
          </Box>
        </AppThemeProvider>
      </body>
    </html>
  );
}
