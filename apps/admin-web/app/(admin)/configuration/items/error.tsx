"use client";

// Segment-level boundary for /configuration/items, scoped to this route so the shell and navigation stay
// rendered when only the configuration read fails; the failure still reaches Faro through
// AdminRouteError's pushError.
import { AdminRouteError } from "@/components/observability/error-boundary";
import type { NextRouteError } from "@/lib/admin-route-error";

export default function Error({ error, reset }: { error: NextRouteError; reset: () => void }) {
  return <AdminRouteError error={error} reset={reset} />;
}
