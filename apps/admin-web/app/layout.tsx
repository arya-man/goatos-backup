import type { Metadata } from "next";
import { FaroProvider } from "@/components/observability/faro-provider";
import { ObservabilityErrorBoundary } from "@/components/observability/error-boundary";
import "./globals.css";
import "./mesha-theme.css";

export const metadata: Metadata = {
  title: "Mesha Admin",
  description: "Mesha internal herd dashboard",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className="dark" suppressHydrationWarning>
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
        <ObservabilityErrorBoundary>{children}</ObservabilityErrorBoundary>
      </body>
    </html>
  );
}
