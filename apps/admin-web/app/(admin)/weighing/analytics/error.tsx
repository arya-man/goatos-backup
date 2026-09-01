"use client";

// Segment-level boundary for the Weights analytics read-out, mirroring the sibling Weights route.
//
// The (admin) boundary would already catch a failure here, but it replaces the whole admin
// segment. Scoping the boundary to this route keeps the shell and navigation rendered when only
// the analytics reads fail, so a reader can move to another screen instead of losing the console —
// and the failure still reaches Faro through AdminRouteError's pushError.
import { AdminRouteError } from "@/components/observability/error-boundary";
import type { NextRouteError } from "@/lib/admin-route-error";

export default function Error({ error, reset }: { error: NextRouteError; reset: () => void }) {
  return <AdminRouteError error={error} reset={reset} />;
}
