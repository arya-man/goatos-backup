import type { Metadata, Viewport } from "next";
import localFont from "next/font/local";
import { FaroProvider } from "@/components/observability/faro-provider";
import { ObservabilityErrorBoundary } from "@/components/observability/error-boundary";
import "./globals.css";
import "./minimal-tokens.css";
import "./mesha-theme.css";
import "./minimal-theme.css";
import "./frame.css";
import "@/theme/fonts.css";
import "@/layouts/template/scrollbar/styles.css";
import "@/components/minimal/chart/styles.css";
import { AppThemeProvider } from "@/theme/app-theme-provider";
import { detectSettings } from "@/layouts/template/settings/server";
import { THEME_BOOT_SCRIPT } from "@/lib/theme";
import { DEV_PERF_GUARD_SCRIPT } from "@/lib/dev-perf-guard";

// Self-hosted (7088230b1): next/font/google fetches from Google at build time, which fails in
// the sandboxed Cloud Build. Same Public Sans variable 400-800 latin face Storybook uses.
const publicSans = localFont({
  src: "./fonts/PublicSans-variable-latin.woff2",
  weight: "400 800",
  display: "swap",
  variable: "--font-public-sans",
});

export const metadata: Metadata = {
  title: "Mesha Admin",
  description: "Mesha internal herd dashboard",
};

// viewport-fit=cover lets the layout opt into env(safe-area-inset-*) so fixed
// surfaces (top bar, drawers, sheets, the AI FAB) clear the notch and the
// home indicator inside a phone browser or an Android/iOS WebView.
export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  // Template cookieSettings path: the settings cookie lets the server render the mini rail (and the
  // shell skeleton, via html[data-nav-rail]) directly, instead of 300px then 88px after hydration.
  const settings = await detectSettings();
  return (
    <html
      lang="en"
      className={`dark ${publicSans.variable}`}
      data-theme="dark"
      data-nav-rail={settings?.navLayout === "mini" ? "mini" : "vertical"}
      suppressHydrationWarning
    >
      <head>
        {/* Runs before paint so a stored light preference never flashes dark. */}
        <script dangerouslySetInnerHTML={{ __html: THEME_BOOT_SCRIPT }} />
        {process.env.NODE_ENV !== "production" ? <script dangerouslySetInnerHTML={{ __html: DEV_PERF_GUARD_SCRIPT }} /> : null}
      </head>
      <body className="font-sans antialiased" suppressHydrationWarning>
        <FaroProvider />
        {/*
          Root-level boundary so every route — including /login, /auth/action, and /api/auth/*
          which render under this layout only (no (admin) route-group boundary) — reports render
          errors to Faro instead of showing a blank page. The (admin) route group nests its own
          ObservabilityErrorBoundary inside AdminShell for a scoped fallback; nested boundaries are
          fine — the innermost one catches first and this root one is the final safety net.
          ObservabilityErrorBoundary is a client component ("use client"); RootLayout stays a server
          component and passes `children` through as already-rendered server output, which is a
          standard, SSR-safe App Router composition.
        */}
        <ObservabilityErrorBoundary>
          <AppThemeProvider cookieSettings={settings}>{children}</AppThemeProvider>
        </ObservabilityErrorBoundary>
      </body>
    </html>
  );
}
