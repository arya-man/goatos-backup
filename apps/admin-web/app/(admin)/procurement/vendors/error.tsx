"use client";

// Segment-level boundary for the vendor register. A missing/stale page contract should not take
// the whole admin shell down while the error is being reported.
import { AdminRouteError } from "@/components/observability/error-boundary";
import type { NextRouteError } from "@/lib/admin-route-error";

export default function Error({ error, reset }: { error: NextRouteError; reset: () => void }) {
  return <AdminRouteError error={error} reset={reset} />;
}
