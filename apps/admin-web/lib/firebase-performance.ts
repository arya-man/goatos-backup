"use client";

import { getApp, getApps, initializeApp, type FirebaseOptions } from "firebase/app";
import type { FirebasePerformance, PerformanceTrace } from "firebase/performance";
import { getFirebaseClientRuntimeConfig } from "@/lib/auth/firebase-client";

type TraceAttributes = Record<string, string | number | boolean | null | undefined>;

type ActiveTrace = {
  stop(payload?: TraceAttributes): void;
};

let perfPromise: Promise<FirebasePerformance | null> | null = null;

export function preloadFirebasePerformance(): void {
  if (typeof window === "undefined") return;
  void getFirebasePerformance();
}

export function startFirebasePerformanceTrace(name: string, attributes: TraceAttributes = {}): ActiveTrace | null {
  if (typeof window === "undefined") return null;
  const startedAt = performance.now();
  let traceRef: PerformanceTrace | null = null;
  let stopped = false;
  let stopPayload: TraceAttributes | null = null;

  void getFirebasePerformance()
    .then(async (perf) => {
      if (!perf) return;
      const { trace } = await import("firebase/performance");
      traceRef = trace(perf, sanitizeTraceName(name));
      applyAttributes(traceRef, attributes);
      traceRef.start();
      if (stopped) {
        finishTrace(traceRef, startedAt, stopPayload ?? {});
      }
    })
    .catch(() => {
      traceRef = null;
    });

  return {
    stop(payload: TraceAttributes = {}) {
      if (stopped) return;
      stopped = true;
      stopPayload = payload;
      if (traceRef) finishTrace(traceRef, startedAt, payload);
    },
  };
}

async function getFirebasePerformance(): Promise<FirebasePerformance | null> {
  if (!perfPromise) {
    perfPromise = initializeFirebasePerformance().catch(() => {
      perfPromise = null;
      return null;
    });
  }
  return perfPromise;
}

async function initializeFirebasePerformance(): Promise<FirebasePerformance | null> {
  if (typeof window === "undefined") return null;
  const enabled = process.env.NEXT_PUBLIC_FIREBASE_PERFORMANCE_ENABLED;
  if (enabled !== "1" && enabled !== "true") return null;
  const [{ config }, { getPerformance }] = await Promise.all([
    getFirebaseClientRuntimeConfig(),
    import("firebase/performance"),
  ]);
  try {
    return getPerformance(getOrCreateFirebaseApp(config));
  } catch {
    return null;
  }
}

function getOrCreateFirebaseApp(config: FirebaseOptions) {
  return getApps().some((candidate) => candidate.name === "goatos-admin-web")
    ? getApp("goatos-admin-web")
    : initializeApp(config, "goatos-admin-web");
}

function applyAttributes(traceRef: PerformanceTrace, attributes: TraceAttributes): void {
  for (const [key, value] of Object.entries(attributes)) {
    if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
      traceRef.putAttribute(sanitizeAttributeName(key), String(value).slice(0, 100));
    }
  }
}

function finishTrace(traceRef: PerformanceTrace, startedAt: number, payload: TraceAttributes): void {
  applyAttributes(traceRef, payload);
  traceRef.putMetric("duration_ms", Math.max(0, Math.round(performance.now() - startedAt)));
  traceRef.stop();
}

function sanitizeTraceName(name: string): string {
  const clean = name.replace(/[^A-Za-z0-9_]/g, "_").replace(/^_+/, "").slice(0, 100);
  return clean || "admin_web_trace";
}

function sanitizeAttributeName(name: string): string {
  const clean = name.replace(/[^A-Za-z0-9_]/g, "_").replace(/^_+/, "").slice(0, 40);
  return clean || "attr";
}
