"use client";

// Segment-level boundary for the Feed Purchases ledger.
//
// The (admin) boundary would already catch a failure here, but it replaces the whole admin
// segment. Scoping the boundary to this route keeps the shell and navigation rendered when only
// the purchase read fails, so a reader can move to another screen instead of losing the console —
// and the failure still reaches Faro through AdminRouteError's pushError. Same shape as the Sales
// board's boundary.
import { AdminRouteError } from "@/components/observability/error-boundary";
import type { NextRouteError } from "@/lib/admin-route-error";

export default function Error({ error, reset }: { error: NextRouteError; reset: () => void }) {
  return <AdminRouteError error={error} reset={reset} />;
}
