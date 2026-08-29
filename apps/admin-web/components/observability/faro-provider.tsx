"use client";

// Grafana Faro Web RUM bootstrap for admin-web (OBSERVABILITY_DESIGN.md §2.4).
//
// Guarded to be a safe no-op when NEXT_PUBLIC_FARO_COLLECTOR_URL is unset (e.g. local dev
// without a Grafana Alloy collector running) and during SSR (Faro is browser-only).
//
// trace linkage: TracingInstrumentation instruments window.fetch/XHR and injects a W3C
// `traceparent` header on same-origin requests (and on any origin listed in
// propagateTraceHeaderCorsUrls) automatically — no manual fetch wrapping is needed here.
// admin-web's browser code only ever calls same-origin Next.js routes (server components and
// route handlers proxy to the real backend via packages/api-client), so this auto-instrumented
// header is what carries the trace from the browser onto the Next.js server. The Next.js side
// then forwards that incoming `traceparent` onto the backend call — see
// apps/admin-web/lib/api/server.ts (`apiClientOptions` / `ServerConfig.traceparent`) and
// packages/api-client/src/index.ts (the @goatos/api-client getTraceHeaders hook) — so the RUM trace and
// the backend's otelhttp span chain onto one trace end to end.
import { useEffect, useRef, useState } from "react";
import { faro, getWebInstrumentations, initializeFaro } from "@grafana/faro-web-sdk";
import { TracingInstrumentation } from "@grafana/faro-web-tracing";
import { usePathname } from "next/navigation";

const FARO_APP_NAME = "mesha-admin-web";

function traceHeaderCorsUrls(): RegExp[] {
  // Public, non-secret config: the origin browser fetches should attach traceparent to besides
  // same-origin (same-origin is always covered by Faro's fetch instrumentation by default).
  // Only set if a deployment ever fetches the backend origin directly from the browser; today
  // admin-web's browser code only calls same-origin Next.js routes/route-handlers.
  const apiBaseOrigin = process.env.NEXT_PUBLIC_GOATOS_API_BASE_ORIGIN;
  if (!apiBaseOrigin) {
    return [];
  }
  // Anchor at both ends of the origin: `^${escaped}` alone would also match a lookalike host
  // (`https://api.example.com` prefix-matches `https://api.example.com.evil.com/...`), which
  // would leak the traceparent header to an attacker-controlled origin. Require the match to end
  // exactly at the origin boundary — either the URL ends there or is followed by `:` (port) or
  // `/` (path).
  const escaped = apiBaseOrigin.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return [new RegExp(`^${escaped}(?:[:/]|$)`)];
}

function initFaro(): void {
  if (typeof window === "undefined" || faro.api) {
    // SSR (no window) or already initialized (React re-render / Strict Mode double-invoke).
    return;
  }
  const collectorUrl = process.env.NEXT_PUBLIC_FARO_COLLECTOR_URL;
  if (!collectorUrl) {
    // No Grafana Alloy faro.receiver endpoint configured for this environment — no-op so local
    // dev without a collector keeps working exactly as before.
    return;
  }
  try {
    initializeFaro({
      url: collectorUrl,
      app: {
        name: FARO_APP_NAME,
        version: process.env.NEXT_PUBLIC_APP_VERSION || "unknown",
        environment: process.env.NEXT_PUBLIC_GOATOS_ENV || "local",
      },
      instrumentations: [
        // Errors (window.onerror + unhandledrejection), Web Vitals, console capture, session.
        // captureConsole defaults to true in getWebInstrumentations, so no options are needed here.
        ...getWebInstrumentations(),
        new TracingInstrumentation({
          instrumentationOptions: {
            propagateTraceHeaderCorsUrls: traceHeaderCorsUrls(),
          },
        }),
      ],
    });
  } catch {
    // Faro must never break page rendering.
  }
}

/**
 * FaroProvider initializes Grafana Faro RUM (errors, web vitals, console errors, unhandled
 * rejections, and route changes) once on the client. Renders nothing. Mount high in the tree
 * (app/layout.tsx) so it captures every route from the first paint.
 */
export function FaroProvider(): null {
  const pathname = usePathname() ?? "/";
  const [routeKey, setRouteKey] = useState(pathname);
  // Mirrors the Faro-recommended Next.js App Router pattern: guarded by `typeof window` and
  // `faro.api` above, so this is a no-op during SSR and idempotent across client re-renders.
  // Runs in an effect (commit phase), not the render body, so it stays safe under React
  // concurrent rendering (renders can be started, discarded, or replayed without side effects).
  useEffect(() => {
    initFaro();
  }, []);

  const previousPathname = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (typeof window === "undefined") return;
    let routeReadTimer: number | undefined;
    const readRoute = (): void => {
      routeReadTimer = undefined;
      setRouteKey(`${window.location.pathname}${window.location.search}`);
    };
    const scheduleRouteRead = (): void => {
      if (routeReadTimer !== undefined) {
        window.clearTimeout(routeReadTimer);
      }
      routeReadTimer = window.setTimeout(readRoute, 0);
    };
    const originalPushState = window.history.pushState;
    const originalReplaceState = window.history.replaceState;
    window.history.pushState = function pushState(...args) {
      const result = originalPushState.apply(this, args);
      scheduleRouteRead();
      return result;
    };
    window.history.replaceState = function replaceState(...args) {
      const result = originalReplaceState.apply(this, args);
      scheduleRouteRead();
      return result;
    };
    scheduleRouteRead();
    window.addEventListener("popstate", scheduleRouteRead);
    return () => {
      if (routeReadTimer !== undefined) {
        window.clearTimeout(routeReadTimer);
      }
      window.history.pushState = originalPushState;
      window.history.replaceState = originalReplaceState;
      window.removeEventListener("popstate", scheduleRouteRead);
    };
  }, []);

  useEffect(() => {
    if (!faro.api) {
      return;
    }
    const viewName = pathname;
    const route = routeKey;
    if (previousPathname.current === route) {
      return;
    }
    faro.api?.setView({ name: viewName });
    faro.api?.pushEvent("admin_route_view", { route });
    previousPathname.current = route;
  }, [pathname, routeKey]);

  return null;
}
